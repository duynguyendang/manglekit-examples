package main

import (
	"context"
	"os"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func loadGherkinPolicy(t *testing.T, ctx context.Context, client *sdk.Client) {
	t.Helper()
	featureContent, err := os.ReadFile("data_governance.feature")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.LoadGherkinPolicy(ctx, string(featureContent)); err != nil {
		t.Fatal(err)
	}
}

func TestPiiLabelHalts(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadGherkinPolicy(t, ctx, client)

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
	loadGherkinPolicy(t, ctx, client)

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
	loadGherkinPolicy(t, ctx, client)

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
	loadGherkinPolicy(t, ctx, client)

	env := core.NewEnvelope("test data")
	env.SetMeta("user_verified", "true")

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "sensitive_action"}, env)
	if err != nil {
		t.Fatalf("expected no error for verified user, got %v", err)
	}
}
