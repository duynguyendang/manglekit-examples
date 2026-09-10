package main

import (
	"context"
	"os"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func loadRules(t *testing.T, ctx context.Context, client *sdk.Client) string {
	t.Helper()
	src, err := os.ReadFile("policy.rules")
	if err != nil {
		t.Fatal(err)
	}
	policy, err := compileRules(string(src))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.LoadPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}
	return policy
}

func TestCompileRules(t *testing.T) {
	src := `# comment
deny action "a" label "l" -> "msg"
deny action "b" meta "k" "v" -> "m2"
`
	policy, err := compileRules(src)
	if err != nil {
		t.Fatal(err)
	}
	want := "halt(\"Req\", \"msg\", \"T3\") :- action_operation(\"Req\", \"a\"), label(\"l\").\n" +
		"halt(\"Req\", \"m2\", \"T3\") :- action_operation(\"Req\", \"b\"), meta(\"k\", \"v\").\n"
	if policy != want {
		t.Fatalf("unexpected policy:\n%s\nwant:\n%s", policy, want)
	}
}

func TestCompileRulesRejectsBadLine(t *testing.T) {
	if _, err := compileRules("not a policy line"); err == nil {
		t.Fatal("expected error for unrecognized rule")
	}
}

func TestPiiLabelHalts(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadRules(t, ctx, client)

	env := core.NewEnvelope("test data")
	env.AddLabel("pii")

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "llm_generate"}, env)
	if err == nil {
		t.Fatal("expected alignment error for PII label")
	}
	if !core.IsAlignmentError(err) {
		t.Fatalf("expected AlignmentError, got %T: %v", err, err)
	}
	alignErr := err.(*core.AlignmentError)
	if alignErr.Message != "PII data must not leave the organization" {
		t.Errorf("unexpected message: %s", alignErr.Message)
	}
}

func TestPublicLabelProceeds(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadRules(t, ctx, client)

	env := core.NewEnvelope("test data")
	env.AddLabel("public")

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "llm_generate"}, env)
	if err != nil {
		t.Fatalf("expected no error for public label, got %v", err)
	}
}

func TestUnverifiedUserHalts(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadRules(t, ctx, client)

	env := core.NewEnvelope("test data")
	env.SetMeta("user_verified", "false")

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "sensitive_action"}, env)
	if err == nil {
		t.Fatal("expected alignment error for unverified user")
	}
	if !core.IsAlignmentError(err) {
		t.Fatalf("expected AlignmentError, got %T: %v", err, err)
	}
}

func TestVerifiedUserProceeds(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadRules(t, ctx, client)

	env := core.NewEnvelope("test data")
	env.SetMeta("user_verified", "true")

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "sensitive_action"}, env)
	if err != nil {
		t.Fatalf("expected no error for verified user, got %v", err)
	}
}
