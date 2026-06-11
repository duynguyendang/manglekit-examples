package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/adapters/knowledge"
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
// engine so the policy's reference to pii_scan resolves. Returns
// false if the engine does not expose RegisterExternalPredicate.
func registerNoopPIIScan(t *testing.T, client *sdk.Client) {
	t.Helper()
	if reg, ok := client.Engine().(interface {
		RegisterExternalPredicate(string, func(context.Context, []any) ([][]any, error)) error
	}); ok {
		if err := reg.RegisterExternalPredicate("pii_scan", noopPIIScan); err != nil {
			t.Fatalf("Failed to register pii_scan: %v", err)
		}
	} else {
		t.Fatal("engine does not support RegisterExternalPredicate")
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
	loader, ok := client.Engine().(interface {
		LoadFromSource(context.Context, string) error
	})
	if !ok {
		t.Fatal("engine does not support LoadFromSource")
	}
	if err := loader.LoadFromSource(ctx, string(policyData)); err != nil {
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
	if err := client.Engine().LoadFacts(facts); err != nil {
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
	loader, ok := client.Engine().(interface {
		LoadFromSource(context.Context, string) error
	})
	if !ok {
		t.Fatal("engine does not support LoadFromSource")
	}
	if err := loader.LoadFromSource(ctx, string(policyData)); err != nil {
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
	if err := client.Engine().LoadFacts(facts); err != nil {
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
