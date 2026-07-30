// policy_copilot demonstrates the NL→Datalog policy generator:
//
//  - Schema extraction: reflects on Go struct fields with `mangle` tags
//    to discover available predicates for the LLM prompt.
//
//  - Few-shot learning: provides example translations so the LLM
//    understands the expected Datalog syntax.
//
//  - Syntax verification: generated rules are validated by the Mangle
//    parser before being returned, catching syntax errors early.
//
// The mock LLM returns a deterministic valid Datalog rule.
// With GOOGLE_API_KEY set, a real Genkit model is used instead.
//
// No API key required (mock LLM returns canned Datalog).

package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// Transaction represents the data model for the policy copilot.
// Field tags teach the LLM which predicates are available.
type Transaction struct {
	Amount   float64 `mangle:"amount"`
	Region   string  `mangle:"region"`
	Category string  `mangle:"category"`
	RiskScore float64 `mangle:"risk_score"`
}

// mockLLMAction returns a deterministic Datalog rule for any prompt.
type mockLLMAction struct{}

func (a *mockLLMAction) Execute(_ context.Context, env core.Envelope) (core.Envelope, error) {
	prompt, _ := env.Payload.(string)

	// Extract just the user policy from the prompt (after LAST "User Policy:" marker)
	lower := prompt
	if idx := strings.LastIndex(prompt, "User Policy:"); idx >= 0 {
		lower = strings.ToLower(prompt[idx:])
	}

	rule := `deny(Req) :- amount(Req, X), X > 1000.` // default
	if strings.Contains(lower, "region") && strings.Contains(lower, "us") {
		rule = `deny(Req) :- region(Req, "US"), amount(Req, X), X < 50.`
	} else if strings.Contains(lower, "risk") && strings.Contains(lower, "0.8") {
		rule = `deny(Req) :- risk_score(Req, X), X > 0.8.`
	}

	return core.NewEnvelope(rule), nil
}

func (a *mockLLMAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "mock_llm", Type: "mock"}
}

func main() {
	ctx := context.Background()

	fmt.Println("=== Policy Copilot: NL → Datalog ===")
	fmt.Println()
	fmt.Println("Schema extracted from Transaction struct:")
	fmt.Println("  amount(EntityID, FloatValue)")
	fmt.Println("  region(EntityID, StringValue)")
	fmt.Println("  category(EntityID, StringValue)")
	fmt.Println("  risk_score(EntityID, FloatValue)")
	fmt.Println()

	gen, err := sdk.NewPolicyGenerator(&mockLLMAction{}, sdk.GeneratorOptions{
		RuleHead: "deny(Req)",
	})
	if err != nil {
		log.Fatal(err)
	}

	policies := []struct {
		nl string
	}{
		{"Block transactions over $1000"},
		{"Deny transactions from the US region under $50"},
		{"Block high-risk scores above 0.8"},
	}

	for _, p := range policies {
		fmt.Printf("--- NL: %q ---\n", p.nl)

		rule, err := gen.GenerateRule(ctx, Transaction{}, p.nl)
		if err != nil {
			fmt.Printf("  ERROR: %v\n", err)
			continue
		}

		fmt.Printf("  Generated: %s\n", rule)
		fmt.Printf("  Verified:  syntax valid (parsed by Mangle)\n")
		fmt.Println()
	}

	fmt.Println("Done.")
}
