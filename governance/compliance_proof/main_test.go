package main

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func loadGDPRPolicy(t *testing.T, ctx context.Context, client *sdk.Client) {
	t.Helper()
	policyData, err := os.ReadFile("gdpr_policy.mg")
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

func TestComplianceProofBundleAndAuditSink(t *testing.T) {
	ctx := context.Background()
	var captured []core.AuditRecord
	sink := core.AuditSinkFunc(func(_ context.Context, rec core.AuditRecord) error {
		captured = append(captured, rec)
		return nil
	})

	client, err := sdk.NewClient(ctx,
		sdk.WithPolicyPath(filepath.Join(exampleDir(), "gdpr_policy.mg")),
		sdk.WithAuditSink(sink),
	)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)

	client.RegisterSupervised("process_data", function.New("process_data",
		func(ctx context.Context, in map[string]string) (string, error) {
			return "processed", nil
		}))

	env := core.NewEnvelope(map[string]string{"type": "health_record"})
	env.SecurityLabels = []string{"special"}

	_, err = client.ExecuteByName(ctx, "process_data", env)
	if err == nil {
		t.Fatal("expected policy violation error for Art.9 special data")
	}

	if len(captured) == 0 {
		t.Fatal("expected audit sink to receive at least one record")
	}

	rec := captured[0]
	if rec.Phase != core.PhasePreCheck {
		t.Errorf("expected phase pre-check, got %s", rec.Phase)
	}
	if rec.Outcome != core.AuditOutcomeHalt {
		t.Errorf("expected outcome HALT, got %s", rec.Outcome)
	}
	if rec.Tier != "T0" {
		t.Errorf("expected tier T0 for Art.9 violation, got %s", rec.Tier)
	}
	if rec.ProofBundle == nil {
		t.Fatal("expected ProofBundle attached to audit record")
	}
	jsonBytes, err := rec.ProofBundle.ToJSON()
	if err != nil {
		t.Fatalf("failed to serialize ProofBundle to JSON: %v", err)
	}
	if len(jsonBytes) == 0 {
		t.Error("expected non-empty JSON bytes")
	}
}
