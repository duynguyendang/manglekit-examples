package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/knowledge"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// noopPIIScan is a pii_scan external predicate that never matches.
// The transitive-access and supervised-execution tests don't exercise
// the PII rule, but the policy references pii_scan, so we must still
// provide a callback or the policy fails to load.
func noopPIIScan(_ context.Context, _ []any) ([][]any, error) {
	return nil, nil
}

// registerNoopPIIScan registers the no-op pii_scan callback on the
// engine so the policy's reference to pii_scan resolves. Fails the
// test if the engine does not support RegisterExternalPredicate.
func registerNoopPIIScan(t *testing.T, client *sdk.Client) {
	t.Helper()
	if err := client.RegisterExternalPredicate("pii_scan", noopPIIScan); err != nil {
		t.Fatalf("Failed to register pii_scan: %v", err)
	}
}

func repoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filename))
}

func TestTransitiveAccessControl(t *testing.T) {
	ctx := context.Background()
	root := repoRoot()

	policyData, err := os.ReadFile(filepath.Join(root, "hybrid_rag/policy.dl"))
	if err != nil {
		t.Fatalf("Failed to read policy.dl: %v", err)
	}

	client := manglekit.Must(manglekit.NewClient(ctx))
	t.Cleanup(func() { client.Shutdown(ctx) })

	// Register pii_scan BEFORE loading the policy, then load via
	// LoadFromSource so the engine auto-emits the external Decl.
	registerNoopPIIScan(t, client)
	if err := client.LoadFromSource(ctx, string(policyData)); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	graphData, err := os.ReadFile(filepath.Join(root, "hybrid_rag/data/access_graph.nq"))
	if err != nil {
		t.Fatalf("Failed to read access_graph.nq: %v", err)
	}
	facts, err := knowledge.ParseNTriples(strings.NewReader(string(graphData)))
	if err != nil {
		t.Fatalf("Failed to parse access_graph.nq: %v", err)
	}
	if err := client.Engine().LoadFacts(ctx, facts); err != nil {
		t.Fatalf("Failed to load graph facts: %v", err)
	}

	checkAccess(t, client, "user_alice", "doc_project_x", true)
	checkAccess(t, client, "user_alice", "doc_project_x_spec", true)
	checkAccess(t, client, "user_alice", "doc_project_y", true)
	checkAccess(t, client, "user_charlie", "doc_project_x", false)
	checkAccess(t, client, "user_charlie", "doc_project_x_spec", false)
	checkAccess(t, client, "user_charlie", "doc_project_y", true)
	checkAccess(t, client, "user_diana", "doc_project_x", true)
	checkAccess(t, client, "user_alice", "doc_remote_work", false)
}

func checkAccess(t *testing.T, client *manglekit.Client, user, doc string, expectAccess bool) {
	t.Helper()
	ctx := context.Background()
	query := `can_access("` + user + `", "` + doc + `").`
	solutions, err := client.Engine().Query(ctx, nil, query)
	if err != nil {
		t.Fatalf("Query %q failed: %v", query, err)
	}
	hasAccess := len(solutions) > 0
	if hasAccess != expectAccess {
		if expectAccess {
			t.Errorf("Expected %s to have access to %s, but access was denied", user, doc)
		} else {
			t.Errorf("Expected %s to be denied access to %s, but access was granted", user, doc)
		}
	}
}

func TestSupervisedActionExecution(t *testing.T) {
	ctx := context.Background()
	root := repoRoot()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	policyData, err := os.ReadFile(filepath.Join(root, "hybrid_rag/policy.dl"))
	if err != nil {
		t.Fatalf("Failed to read policy.dl: %v", err)
	}
	registerNoopPIIScan(t, client)
	if err := client.LoadFromSource(ctx, string(policyData)); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	graphData, err := os.ReadFile(filepath.Join(root, "hybrid_rag/data/access_graph.nq"))
	if err != nil {
		t.Fatalf("Failed to read access_graph.nq: %v", err)
	}
	facts, err := knowledge.ParseNTriples(strings.NewReader(string(graphData)))
	if err != nil {
		t.Fatalf("Failed to parse access_graph.nq: %v", err)
	}
	if err := client.Engine().LoadFacts(ctx, facts); err != nil {
		t.Fatalf("Failed to load graph facts: %v", err)
	}

	// Register a supervised action
	fn := func(ctx context.Context, req QueryRequest) (string, error) {
		return "processed: " + req.Text, nil
	}
	action := manglekit.Define(client, "test_action", fn)

	// Execute should succeed for a basic request with no policy violation
	res, err := action.Run(ctx, QueryRequest{Type: "query", Text: "hello"})
	if err != nil {
		t.Fatalf("Supervised action execution failed: %v", err)
	}
	if res != "processed: hello" {
		t.Fatalf("Unexpected result: %q", res)
	}
}

// TestPreCheckFailClosed pins that the supervisor PRE-CHECK is fail-closed
// (Tier 0/1): a halt("Req", ...) rule must block execution and the inner
// action must NOT run. The PII POST-check demonstrated in runPIIScenario is
// fail-open on verifier error (CODE_REVIEW P0.1), so the trustworthy gate is
// the PRE-CHECK — this test proves the PRE-CHECK actually blocks.
//
// We load a tiny inline policy (gated on a metadata flag we control) so the
// test is independent of policy.dl and the data files.
func TestPreCheckFailClosed(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	// Inline policy: gate a halt("Req", ...) rule on a metadata flag we
	// control. The supervisor PRE-CHECK evaluates halt("Req", ...) rules
	// before the inner action runs, so toggling the flag deterministically
	// demonstrates the pre-check blocking (and the inner action not running).
	const preCheckPolicy = `
halt("Req", "blocked by pre-check") :-
    action_operation("Req", "simulate_llm"),
    meta("force_block", "true").
`
	if err := client.LoadFromSource(ctx, preCheckPolicy); err != nil {
		t.Fatalf("Failed to load pre-check policy: %v", err)
	}

	ran := false
	act := function.New("simulate_llm", func(ctx context.Context, req QueryRequest) (LLMOutput, error) {
		ran = true
		return LLMOutput{Content: "Processed Query: " + req.Text}, nil
	})
	safeAct := client.Supervise(act)
	client.RegisterAction("simulate_llm", safeAct)

	// 1. PRE-CHECK must BLOCK when force_block metadata is set, and the
	//    inner action must never run.
	_, err = client.ExecuteByName(ctx, "simulate_llm", QueryRequest{Type: "query", Text: "hello"},
		sdk.WithMetadata("force_block", "true"),
	)
	if err == nil {
		t.Fatalf("Expected pre-check to block, but action executed")
	}
	if !core.IsPolicyViolationError(err) {
		t.Fatalf("Expected PolicyViolationError from pre-check, got: %v", err)
	}
	if ran {
		t.Fatalf("Pre-check must block before the inner action runs, but the inner action ran")
	}

	// 2. Without force_block, the PRE-CHECK must NOT block and the inner
	//    action must run normally.
	ran = false
	_, err = client.ExecuteByName(ctx, "simulate_llm", QueryRequest{Type: "query", Text: "hello"},
		sdk.WithMetadata("force_block", "false"),
	)
	if err != nil {
		t.Fatalf("Expected success without force_block, got: %v", err)
	}
	if !ran {
		t.Fatalf("Expected inner action to run without force_block")
	}
}
