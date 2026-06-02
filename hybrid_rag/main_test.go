package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/sdk"
)

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
	if err := client.Engine().LoadPolicy(ctx, string(policyData)); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	graphData, err := os.ReadFile(filepath.Join(root, "hybrid_rag/data/access_graph.nq"))
	if err != nil {
		t.Fatalf("Failed to read access_graph.nq: %v", err)
	}
	var facts []string
	lines := strings.Split(string(graphData), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			s := strings.Trim(parts[0], "<>")
			p := strings.Trim(parts[1], "<>")
			o := strings.Trim(parts[2], "\"")
			fact := `triple("` + s + `", "` + p + `", "` + o + `")`
			facts = append(facts, fact)
		}
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

	// Load policy
	policyData, err := os.ReadFile(filepath.Join(root, "hybrid_rag/policy.dl"))
	if err != nil {
		t.Fatalf("Failed to read policy.dl: %v", err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyData)); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}

	// Load access graph facts
	graphData, err := os.ReadFile(filepath.Join(root, "hybrid_rag/data/access_graph.nq"))
	if err != nil {
		t.Fatalf("Failed to read access_graph.nq: %v", err)
	}
	var facts []string
	for _, line := range strings.Split(string(graphData), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			s := strings.Trim(parts[0], "<>")
			p := strings.Trim(parts[1], "<>")
			o := strings.Trim(parts[2], "\"")
			facts = append(facts, `triple("`+s+`", "`+p+`", "`+o+`")`)
		}
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
