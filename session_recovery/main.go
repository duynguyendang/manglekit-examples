package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// InMemoryStateProvider implements core.StateProvider using a thread-safe map.
// For production, replace with Redis, Badger, or Postgres-backed implementation.
type InMemoryStateProvider struct {
	store map[string]*core.SessionState
	mu    sync.RWMutex
}

func NewInMemoryStateProvider() *InMemoryStateProvider {
	return &InMemoryStateProvider{
		store: make(map[string]*core.SessionState),
	}
}

func (p *InMemoryStateProvider) Get(ctx context.Context, sessionID string) (any, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()

	state, ok := p.store[sessionID]
	if !ok {
		return nil, nil
	}
	data, err := json.Marshal(state)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal state: %w", err)
	}
	return data, nil
}

func (p *InMemoryStateProvider) Set(ctx context.Context, sessionID string, state any) error {
	p.mu.Lock()
	defer p.mu.Unlock()

	var parsed core.SessionState
	switch v := state.(type) {
	case []byte:
		if err := json.Unmarshal(v, &parsed); err != nil {
			return fmt.Errorf("failed to unmarshal state: %w", err)
		}
	case *core.SessionState:
		parsed = *v
	case core.SessionState:
		parsed = v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return fmt.Errorf("failed to marshal state: %w", err)
		}
		if err := json.Unmarshal(data, &parsed); err != nil {
			return fmt.Errorf("failed to unmarshal state: %w", err)
		}
	}

	p.store[sessionID] = &parsed
	return nil
}

func (p *InMemoryStateProvider) Delete(ctx context.Context, sessionID string) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	delete(p.store, sessionID)
	return nil
}

func (p *InMemoryStateProvider) Close(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.store = make(map[string]*core.SessionState)
	return nil
}

// SessionManager wraps a StateProvider with checkpoint/hydrate logic.
// This mirrors what the SDK's internal statemanager does.
type SessionManager struct {
	provider core.StateProvider
}

func NewSessionManager(provider core.StateProvider) *SessionManager {
	return &SessionManager{provider: provider}
}

func (m *SessionManager) Checkpoint(ctx context.Context, state *core.SessionState) error {
	if err := state.Validate(); err != nil {
		return fmt.Errorf("invalid state: %w", err)
	}
	return m.provider.Set(ctx, state.SessionID, state)
}

func (m *SessionManager) Hydrate(ctx context.Context, sessionID string) (*core.SessionState, error) {
	raw, err := m.provider.Get(ctx, sessionID)
	if err != nil {
		return nil, fmt.Errorf("provider get failed: %w", err)
	}
	if raw == nil {
		return nil, nil
	}

	var state core.SessionState
	switch v := raw.(type) {
	case []byte:
		if err := json.Unmarshal(v, &state); err != nil {
			return nil, err
		}
	case *core.SessionState:
		state = *v
	default:
		data, err := json.Marshal(v)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(data, &state); err != nil {
			return nil, err
		}
	}

	if err := state.Validate(); err != nil {
		return nil, err
	}
	return &state, nil
}

// WorkflowStep represents a single step in a multi-step workflow.
type WorkflowStep struct {
	Name     string
	Fact     string
	Metadata map[string]any
}

func main() {
	ctx := context.Background()

	fmt.Println("=== Session Recovery Example ===")
	fmt.Println("Demonstrates durable state persistence with crash simulation")
	fmt.Println()

	// Define a 5-step workflow
	steps := []WorkflowStep{
		{Name: "Initialize Environment", Fact: "step(initialize)", Metadata: map[string]any{"env": "production"}},
		{Name: "Load Configuration", Fact: "step(load_config)", Metadata: map[string]any{"config_version": "2.1"}},
		{Name: "Validate Inputs", Fact: "step(validate)", Metadata: map[string]any{"validated": true}},
		{Name: "Execute Transaction", Fact: "step(execute_txn)", Metadata: map[string]any{"txn_id": "TXN-001"}},
		{Name: "Finalize and Report", Fact: "step(finalize)", Metadata: map[string]any{"report": "complete"}},
	}

	sessionID := "workflow-session-001"
	provider := NewInMemoryStateProvider()

	// Create SDK client with the state provider to demonstrate sdk.WithStateProvider()
	client, err := sdk.NewClient(ctx, sdk.WithStateProvider(provider))
	if err != nil {
		log.Fatalf("Failed to create SDK client: %v", err)
	}
	fmt.Println("✓ SDK client created with InMemoryStateProvider")
	fmt.Println()

	// Use our SessionManager for direct checkpoint/hydrate operations
	sm := NewSessionManager(provider)

	// --- Phase 1: Execute steps 1-3, then simulate crash ---
	fmt.Println("--- Phase 1: Execute steps 1-3 and checkpoint ---")

	state := &core.SessionState{
		SessionID: sessionID,
		ActiveEnvelope: core.Envelope{
			ID:          core.NewEnvelope(nil).ID,
			Payload:     map[string]any{"workflow": "order-processing"},
			ContentType: core.TypeJSON,
			Metadata:    make(map[string]any),
		},
		ExecutionCtx: core.ExecutionContext{},
	}

	for i := 0; i < 3; i++ {
		executeStep(state, steps[i])

		if err := sm.Checkpoint(ctx, state); err != nil {
			log.Fatalf("Failed to checkpoint at step %d: %v", i+1, err)
		}
		fmt.Printf("  ✓ Checkpointed after step %d\n", i+1)
	}

	fmt.Println()
	fmt.Println("💥 CRASH SIMULATED — discarding client and state variable")
	fmt.Println("   Creating fresh client with the same StateProvider...")
	fmt.Println()

	// Shutdown old client
	_ = client.Shutdown(ctx)

	// --- Phase 2: Recover and resume from step 4 ---
	fmt.Println("--- Phase 2: Recover state and resume from step 4 ---")

	// New client reuses the same provider — state persists
	client2, err := sdk.NewClient(ctx, sdk.WithStateProvider(provider))
	if err != nil {
		log.Fatalf("Failed to create new client: %v", err)
	}
	defer client2.Shutdown(ctx)

	sm2 := NewSessionManager(provider)

	recovered, err := sm2.Hydrate(ctx, sessionID)
	if err != nil {
		log.Fatalf("Failed to hydrate session: %v", err)
	}
	if recovered == nil {
		log.Fatal("No recovered state found — checkpoint may have failed")
	}

	fmt.Printf("  ✓ Recovered session: %s\n", recovered.SessionID)
	fmt.Printf("  ✓ Facts preserved: %v\n", recovered.LogicalFacts)
	fmt.Printf("  ✓ History length: %d messages\n", len(recovered.ExecutionCtx.CurrentHistory))
	fmt.Println()

	// Resume from step 4
	for i := 3; i < len(steps); i++ {
		executeStep(recovered, steps[i])

		if err := sm2.Checkpoint(ctx, recovered); err != nil {
			log.Fatalf("Failed to checkpoint at step %d: %v", i+1, err)
		}
		fmt.Printf("  ✓ Checkpointed after step %d\n", i+1)
	}

	// --- Final verification ---
	fmt.Println()
	fmt.Println("--- Final Verification ---")

	final, err := sm2.Hydrate(ctx, sessionID)
	if err != nil {
		log.Fatalf("Failed final hydration: %v", err)
	}

	fmt.Printf("  Session ID:  %s\n", final.SessionID)
	fmt.Printf("  Total facts: %d\n", len(final.LogicalFacts))
	for i, f := range final.LogicalFacts {
		fmt.Printf("    [%d] %s\n", i+1, f)
	}
	fmt.Printf("  History:     %d messages\n", len(final.ExecutionCtx.CurrentHistory))
	fmt.Println()

	// Clean up
	if err := provider.Delete(ctx, sessionID); err != nil {
		log.Fatalf("Failed to delete session: %v", err)
	}
	fmt.Printf("  ✓ Session %s cleaned up\n", sessionID)
	fmt.Println()
	fmt.Println("=== Session recovery example completed successfully ===")
}

// executeStep simulates executing a workflow step by appending facts and history.
func executeStep(state *core.SessionState, step WorkflowStep) {
	state.LogicalFacts = append(state.LogicalFacts, step.Fact)
	state.ExecutionCtx.CurrentHistory = append(state.ExecutionCtx.CurrentHistory, core.Message{
		Role:    "system",
		Content: fmt.Sprintf("Executed: %s", step.Name),
	})

	if state.ActiveEnvelope.Metadata == nil {
		state.ActiveEnvelope.Metadata = make(map[string]any)
	}
	for k, v := range step.Metadata {
		state.ActiveEnvelope.Metadata[k] = v
	}

	fmt.Printf("  → Step: %s (fact: %s)\n", step.Name, step.Fact)
	time.Sleep(10 * time.Millisecond)
}
