package main

import (
	"context"
	"os"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func loadGDPRPolicy(t *testing.T, ctx context.Context, client *sdk.Client) {
	t.Helper()
	policyData, err := os.ReadFile("gdpr_policy.dl")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyData)); err != nil {
		t.Fatal(err)
	}
}

func TestProofIsPopulated(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadGDPRPolicy(t, ctx, client)

	env := core.NewEnvelope("process health data")
	env.SecurityLabels = []string{"special"}
	env.Facts = append(env.Facts, `action_operation("Req", "process_data").`)

	decision, err := client.Engine().AssessPlan(ctx, env)
	if err != nil {
		t.Fatalf("AssessPlan error: %v", err)
	}
	if decision.Outcome != core.DecisionHalt {
		t.Errorf("expected halt, got %s", decision.Outcome)
	}
	if decision.AuditTrail == nil {
		t.Fatal("AuditTrail is nil — AssessPlan regression not fixed")
	}
	if len(decision.AuditTrail.MatchedRules) == 0 {
		t.Error("expected at least one matched rule in AuditTrail")
	}
}

func TestArt9SpecialCategoryBlocked(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadGDPRPolicy(t, ctx, client)

	env := core.NewEnvelope("process health record")
	env.SecurityLabels = []string{"special"}
	env.Facts = append(env.Facts, `action_operation("Req", "process_data").`)

	decision, err := client.Engine().AssessPlan(ctx, env)
	if err != nil {
		t.Fatalf("AssessPlan error: %v", err)
	}
	if decision.Outcome != core.DecisionHalt {
		t.Errorf("expected halt for Art.9, got %s", decision.Outcome)
	}

	found := false
	for _, r := range decision.Reasons {
		if r == "Art.9: special-category data requires explicit consent" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected Art.9 reason, got %v", decision.Reasons)
	}
}

func TestArt6NoLawfulBasisBlocked(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadGDPRPolicy(t, ctx, client)

	env := core.NewEnvelope("process personal data")
	env.SecurityLabels = []string{"personal"}
	env.Facts = append(env.Facts, `action_operation("Req", "process_data").`)

	decision, err := client.Engine().AssessPlan(ctx, env)
	if err != nil {
		t.Fatalf("AssessPlan error: %v", err)
	}
	if decision.Outcome != core.DecisionHalt {
		t.Errorf("expected halt for Art.6, got %s", decision.Outcome)
	}
}

func TestArt6WithConsentPermitted(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadGDPRPolicy(t, ctx, client)

	env := core.NewEnvelope("process personal data")
	env.SecurityLabels = []string{"personal"}
	env.Facts = []string{
		`action_operation("Req", "process_data").`,
		`has_lawful_basis("consent").`,
	}

	decision, err := client.Engine().AssessPlan(ctx, env)
	if err != nil {
		t.Fatalf("AssessPlan error: %v", err)
	}
	if decision.Outcome != core.DecisionProceed {
		t.Errorf("expected proceed with consent, got %s", decision.Outcome)
	}
}
