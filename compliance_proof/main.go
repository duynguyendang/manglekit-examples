// compliance_proof demonstrates machine-checkable GDPR compliance using
// tiered Datalog. Each case runs AssessPlan, then renders the AuditTrail
// as a human-readable proof showing which rules fired, their tier, and
// variable bindings.
//
// Tier mapping:
//   T0 (Axiom)   — Art.9 special-category data
//   T1 (Govern)  — Art.6 lawful basis
//
// No API key required (deterministic mock TextGenerator).

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

type Case struct {
	Name     string
	Action   string
	Labels   []string
	Facts    []string
	WantHalt bool
	WantMsg  string
}

func main() {
	ctx := context.Background()

	policyData, err := os.ReadFile("compliance_proof/gdpr_policy.dl")
	if err != nil {
		log.Fatal(err)
	}

	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Shutdown(ctx)

	if err := client.Engine().LoadPolicy(ctx, string(policyData)); err != nil {
		log.Fatal(err)
	}

	cases := []Case{
		{
			Name:     "Art.9: special-category without explicit consent",
			Action:   "process_data",
			Labels:   []string{"special"},
			WantHalt: true,
			WantMsg:  "Art.9: special-category data requires explicit consent",
		},
		{
			Name:     "Art.6: personal data without any lawful basis",
			Action:   "process_data",
			Labels:   []string{"personal"},
			Facts:    []string{`data_category("personal").`},
			WantHalt: true,
			WantMsg:  "Art.6: no lawful basis for processing",
		},
		{
			Name:     "Art.6: personal data with consent",
			Action:   "process_data",
			Labels:   []string{"personal"},
			Facts:    []string{`data_category("personal").`, `has_lawful_basis("consent").`},
			WantHalt: false,
		},
	}

	allPass := true
	for i, tc := range cases {
		fmt.Printf("--- Case %d: %s ---\n", i+1, tc.Name)

		env := core.NewEnvelope("process payment data")
		env.SecurityLabels = tc.Labels
		// AssessPlan does not inject action_operation from ActionMetadata
		// (it passes core.ActionMetadata{} internally). Halt rules in
		// gdpr_policy.dl require this fact, so the caller must append it.
		env.Facts = append(env.Facts, fmt.Sprintf("action_operation(%q, %q).", "Req", tc.Action))
		for _, f := range tc.Facts {
			env.Facts = append(env.Facts, f)
		}

		decision, err := client.Engine().AssessPlan(ctx, env)
		if err != nil {
			fmt.Printf("  AssessPlan error: %v\n", err)
			allPass = false
			continue
		}

		fmt.Printf("  Outcome: %s\n", decision.Outcome)

		// Render the AuditTrail as a human-readable proof
		if decision.AuditTrail != nil {
			proof := renderProof(decision.AuditTrail)
			fmt.Printf("  Proof:\n%s\n", proof)
		} else {
			fmt.Println("  Proof: (no audit trail)")
		}

		// Verify outcome
		gotHalt := decision.Outcome == core.DecisionHalt
		if gotHalt != tc.WantHalt {
			fmt.Printf("  FAIL: want halt=%v, got halt=%v\n", tc.WantHalt, gotHalt)
			allPass = false
		}

		if tc.WantHalt && tc.WantMsg != "" {
			found := false
			for _, r := range decision.Reasons {
				if r == tc.WantMsg {
					found = true
					break
				}
			}
			if !found {
				fmt.Printf("  FAIL: expected reason %q, got %v\n", tc.WantMsg, decision.Reasons)
				allPass = false
			}
		}

		fmt.Println()
	}

	if !allPass {
		fmt.Println("Some cases FAILED.")
		os.Exit(1)
	}
	fmt.Println("All cases passed.")
}

// renderProof converts an AuditTrail into a human-readable proof string
// showing rules fired, tier, and variable bindings.
func renderProof(trail *core.AuditTrail) string {
	if trail == nil || len(trail.MatchedRules) == 0 {
		return "  (no rules matched — no proof generated)"
	}

	out := ""
	for _, rule := range trail.MatchedRules {
		bindings := ""
		if len(rule.Bindings) > 0 {
			pairs := ""
			for k, v := range rule.Bindings {
				if pairs != "" {
					pairs += ", "
				}
				pairs += fmt.Sprintf("%s=%s", k, v)
			}
			bindings = fmt.Sprintf("  bindings: {%s}", pairs)
		}
		out += fmt.Sprintf("  Rule: %s [tier=%s] predicate=%s\n%s\n",
			rule.RuleName, rule.Tier, rule.Predicate, bindings)
	}
	return out
}
