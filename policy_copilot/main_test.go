package main

import (
	"context"
	"strings"
	"testing"

	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/x/genes"
	"github.com/stretchr/testify/require"
)

func TestGenerateRule_Default(t *testing.T) {
	ctx := context.Background()
	gen, err := sdk.NewPolicyGenerator(&mockLLMAction{}, sdk.GeneratorOptions{
		RuleHead: "deny(Req)",
	})
	if err != nil {
		t.Fatal(err)
	}

	rule, err := gen.GenerateRule(ctx, Transaction{}, "Block transactions over $1000")
	if err != nil {
		t.Fatalf("GenerateRule error: %v", err)
	}
	if rule != `deny(Req) :- amount(Req, X), X > 1000.` {
		t.Errorf("unexpected rule: %s", rule)
	}
}

func TestGenerateRule_Region(t *testing.T) {
	ctx := context.Background()
	gen, err := sdk.NewPolicyGenerator(&mockLLMAction{}, sdk.GeneratorOptions{
		RuleHead: "deny(Req)",
	})
	if err != nil {
		t.Fatal(err)
	}

	rule, err := gen.GenerateRule(ctx, Transaction{}, "Deny from US region")
	if err != nil {
		t.Fatalf("GenerateRule error: %v", err)
	}
	if rule != `deny(Req) :- region(Req, "US"), amount(Req, X), X < 50.` {
		t.Errorf("unexpected rule: %s", rule)
	}
}

func TestGenerateRule_RiskScore(t *testing.T) {
	ctx := context.Background()
	gen, err := sdk.NewPolicyGenerator(&mockLLMAction{}, sdk.GeneratorOptions{
		RuleHead: "deny(Req)",
	})
	if err != nil {
		t.Fatal(err)
	}

	rule, err := gen.GenerateRule(ctx, Transaction{}, "Block high-risk scores above 0.8")
	if err != nil {
		t.Fatalf("GenerateRule error: %v", err)
	}
	if rule != `deny(Req) :- risk_score(Req, X), X > 0.8.` {
		t.Errorf("unexpected rule: %s", rule)
	}
}

func TestSchemaExtraction(t *testing.T) {
	ctx := context.Background()
	gen, err := sdk.NewPolicyGenerator(&mockLLMAction{}, sdk.GeneratorOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = gen.GenerateRule(ctx, Transaction{}, "test")
	if err != nil {
		t.Fatalf("schema extraction failed: %v", err)
	}
}

// ============================================================================
// Signed-gene lifecycle (x/genes): sign → tamper-evidence → policy-channel
// enforcement. Machine-generated rules reach the gate ONLY here.
// ============================================================================

func TestGeneLifecycleSignTamperEnforce(t *testing.T) {
	ctx := context.Background()
	rule := "deny(Req) :- amount(Req, X), X > 1000.\n"

	gene := genes.Gene{
		Name: "copilot-test", Tier: core.TierT3_User,
		Source: "unit-test", Intents: []string{"process_transaction"}, Rules: rule,
	}
	gene.Sign()
	require.NoError(t, gene.Verify())

	// Whitespace-only tampering must break signature verification.
	tampered := gene
	tampered.Rules = strings.Replace(rule, " :- ", "  :- ", 1)
	require.Error(t, tampered.Verify())

	// Compile check: gene tier T3 with an untyped deny head is acceptable
	// (no tier claims inside rules to disagree with).
	if _, err := genes.Compile([]genes.Gene{gene}); err != nil {
		t.Fatalf("compile advisory gene: %v", err)
	}

	// Enforcement through the policy channel: typed high-value facts deny,
	// low-value facts proceed.
	client, err := sdk.NewClient(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Shutdown(ctx) }()
	require.NoError(t, client.LoadPolicy(ctx, schemaDecls))
	require.NoError(t, genes.ApplyTo(ctx, client, []genes.Gene{gene}))
	act := function.New("process_transaction", func(ctx context.Context, tx Transaction) (string, error) {
		return "ok", nil
	})
	client.RegisterSupervised("process_transaction", act)

	_, err = client.ExecuteByName(ctx, "process_transaction", core.Envelope{
		Payload: "tx", Facts: []string{`amount("Req", 1500).`},
	})
	require.Error(t, err, "generated rule must deny the high-value transaction")
	require.True(t, core.IsPolicyViolationError(err))
}
