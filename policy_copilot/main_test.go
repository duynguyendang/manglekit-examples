package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/sdk"
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
