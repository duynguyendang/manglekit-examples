// persistent_store answers: Does the agent's work survive a crash or restart? (UC-I3, UC-I7)
// persistent_store demonstrates a durable, BadgerDB-backed session/knowledge
// store with restart-resume: the store is closed and reopened within the same
// run, and the checkpointed workflow state survives.
//
// The example implements core.StateProvider on top of BadgerDB opened with
// explicit durability (WithSyncWrites(true) — every Set fsyncs before
// returning, so a crash cannot lose a checkpointed step). This mirrors
// manglekit's own graph store durability stance
// (internal graph.NewStoreWithOptions(path, readOnly, syncWrites=true)):
// asynchronous writes are the right default for re-derivable cache state,
// while session persistence opts into fsync. Knowledge facts are written with
// a single Badger WriteBatch per session (the batch pattern behind
// Store.AddFacts) instead of one transaction per fact.
//
// No API key required. The store directory defaults to ./persistent_store_data
// under the example dir and is created on first use.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/dgraph-io/badger/v4"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// BadgerStateProvider implements core.StateProvider on BadgerDB with
// syncWrites=true (durable session persistence).
type BadgerStateProvider struct {
	db *badger.DB
}

var _ core.StateProvider = (*BadgerStateProvider)(nil)

// NewBadgerStateProvider opens (or creates) a durable BadgerDB store at dir.
func NewBadgerStateProvider(dir string) (*BadgerStateProvider, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("create store dir: %w", err)
	}
	// WithSyncWrites(true): every write fsyncs before returning — the
	// durability opt-in for session persistence (the default, async writes,
	// is for cache-style, re-derivable state).
	opts := badger.DefaultOptions(dir).WithSyncWrites(true).WithLogger(nil)
	db, err := badger.Open(opts)
	if err != nil {
		return nil, fmt.Errorf("open badger at %s: %w", dir, err)
	}
	return &BadgerStateProvider{db: db}, nil
}

func stateKey(sessionID string) []byte { return []byte("state/" + sessionID) }

// Get implements core.StateProvider: JSON bytes of the session state, or
// (nil, nil) when absent.
func (p *BadgerStateProvider) Get(_ context.Context, sessionID string) (any, error) {
	var data []byte
	err := p.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(stateKey(sessionID))
		if err == badger.ErrKeyNotFound {
			return nil // absent => nil, nil
		}
		if err != nil {
			return err
		}
		return item.Value(func(v []byte) error {
			data = append([]byte(nil), v...)
			return nil
		})
	})
	if err != nil {
		return nil, err
	}
	return data, nil
}

// Set implements core.StateProvider (durable: fsynced on return).
func (p *BadgerStateProvider) Set(_ context.Context, sessionID string, state any) error {
	var raw []byte
	switch v := state.(type) {
	case []byte:
		raw = v
	default:
		b, err := json.Marshal(state)
		if err != nil {
			return fmt.Errorf("marshal state: %w", err)
		}
		raw = b
	}
	return p.db.Update(func(txn *badger.Txn) error {
		return txn.Set(stateKey(sessionID), raw)
	})
}

// PutFacts batch-writes knowledge facts for a session using ONE Badger
// WriteBatch (the same batch pattern as the kernel's Store.AddFacts), instead
// of paying one fsync-ed transaction per fact.
func (p *BadgerStateProvider) PutFacts(_ context.Context, sessionID string, facts []string) error {
	wb := p.db.NewWriteBatch()
	defer wb.Cancel()
	for i, f := range facts {
		key := []byte(fmt.Sprintf("facts/%s/%06d", sessionID, i))
		if err := wb.Set(key, []byte(f)); err != nil {
			return err
		}
	}
	return wb.Flush()
}

// Facts reads back a session's knowledge facts in insertion order.
func (p *BadgerStateProvider) Facts(_ context.Context, sessionID string) ([]string, error) {
	prefix := []byte("facts/" + sessionID + "/")
	var out []string
	err := p.db.View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.Prefix = prefix
		it := txn.NewIterator(opts)
		defer it.Close()
		for it.Rewind(); it.ValidForPrefix(prefix); it.Next() {
			err := it.Item().Value(func(v []byte) error {
				out = append(out, string(v))
				return nil
			})
			if err != nil {
				return err
			}
		}
		return nil
	})
	return out, err
}

// Delete implements core.StateProvider.
func (p *BadgerStateProvider) Delete(_ context.Context, sessionID string) error {
	return p.db.Update(func(txn *badger.Txn) error {
		return txn.Delete(stateKey(sessionID))
	})
}

// Close implements core.StateProvider: gracefully closes Badger, flushing
// memtables — the "shutdown" half of the restart-resume demo.
func (p *BadgerStateProvider) Close(_ context.Context) error {
	return p.db.Close()
}

// ---------------------------------------------------------------------------
// Restart-resume workflow
// ---------------------------------------------------------------------------

var workflowSteps = []string{
	"step(initialize).",
	"step(load_config).",
	"step(validate).",
	"step(execute_txn).",
	"step(finalize).",
}

// runPhase1 executes steps 1-3, checkpoints after each, then CLOSES the
// store — simulating a crash / planned shutdown after step 3.
func runPhase1(ctx context.Context, dir, sessionID string) error {
	provider, err := NewBadgerStateProvider(dir)
	if err != nil {
		return err
	}

	state := &core.SessionState{
		SessionID:      sessionID,
		ActiveEnvelope: core.NewEnvelope(map[string]any{"workflow": "order-processing"}),
	}
	for i := 0; i < 3; i++ {
		state.LogicalFacts = append(state.LogicalFacts, workflowSteps[i])
		if err := provider.Set(ctx, sessionID, state); err != nil {
			return fmt.Errorf("checkpoint step %d: %w", i+1, err)
		}
		if err := provider.PutFacts(ctx, sessionID, state.LogicalFacts); err != nil {
			return fmt.Errorf("batch facts step %d: %w", i+1, err)
		}
		fmt.Printf("  checkpointed after step %d: %s\n", i+1, workflowSteps[i])
	}

	fmt.Println("  CRASH/SHUTDOWN simulated — closing the Badger store.")
	return provider.Close(ctx)
}

// runPhase2 REOPENS the same store directory, hydrates the session, verifies
// the 3 checkpointed facts survived, and finishes steps 4-5.
func runPhase2(ctx context.Context, dir, sessionID string) error {
	provider, err := NewBadgerStateProvider(dir)
	if err != nil {
		return err
	}
	defer provider.Close(ctx)

	// Wire the durable provider into an SDK client — this is how a real
	// service resumes: same store path, fresh process.
	client, err := sdk.NewClient(ctx, sdk.WithStateProvider(provider))
	if err != nil {
		return err
	}
	defer client.Shutdown(ctx)

	raw, err := provider.Get(ctx, sessionID)
	if err != nil {
		return err
	}
	if raw == nil {
		return fmt.Errorf("no recovered state for %s — durability broken", sessionID)
	}
	var state core.SessionState
	if err := json.Unmarshal(raw.([]byte), &state); err != nil {
		return err
	}
	fmt.Printf("  hydrated session %s with %d checkpointed facts\n", state.SessionID, len(state.LogicalFacts))

	facts, err := provider.Facts(ctx, sessionID)
	if err != nil {
		return err
	}
	fmt.Printf("  knowledge facts recovered from batch store: %d\n", len(facts))

	for i := 3; i < len(workflowSteps); i++ {
		state.LogicalFacts = append(state.LogicalFacts, workflowSteps[i])
		if err := provider.Set(ctx, sessionID, &state); err != nil {
			return fmt.Errorf("checkpoint step %d: %w", i+1, err)
		}
		fmt.Printf("  resumed and checkpointed step %d: %s\n", i+1, workflowSteps[i])
	}

	if len(state.LogicalFacts) != len(workflowSteps) {
		return fmt.Errorf("expected %d total facts after resume, got %d", len(workflowSteps), len(state.LogicalFacts))
	}
	return provider.Delete(ctx, sessionID)
}

// Run executes the full restart-resume demo against dir. Split out of main so
// tests can drive it against a temp directory.
func Run(dir string) error {
	ctx := context.Background()
	const sessionID = "workflow-session-001"

	fmt.Println("--- Phase 1: run steps 1-3, checkpoint durably, close ---")
	if err := runPhase1(ctx, dir, sessionID); err != nil {
		return err
	}
	fmt.Println("--- Phase 2: reopen the store and resume from step 4 ---")
	if err := runPhase2(ctx, dir, sessionID); err != nil {
		return err
	}
	fmt.Println("PASS: state survived the close/reopen round-trip.")
	return nil
}

func main() {
	dir := filepath.Join(exampleDir(), "persistent_store_data")
	if err := Run(dir); err != nil {
		log.Fatalf("persistent_store failed: %v", err)
	}
	ctx := context.Background()
	if err := demoRecovery(ctx); err != nil {
		log.Fatalf("recovery section failed: %v", err)
	}
}
