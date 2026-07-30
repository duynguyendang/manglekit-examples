// gherkin_policy demonstrates Gherkin BDD feature files compiled to
// Datalog policies via LoadGherkinPolicy. The feature file defines
// two scenarios — PII blocking and metadata checks — which the engine
// compiles into halt Datalog rules.
//
// Demonstrates: LoadGherkinPolicy, Assess (returns AlignmentError on
// blocks), with action_operation facts injected automatically.
//
// No API key required (deterministic).

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

type case_ struct {
	Name      string
	Action    string
	Label     string
	MetaKey   string
	MetaValue string
	WantHalt  bool
	WantMsg   string
}

func main() {
	ctx := context.Background()

	featureContent, err := os.ReadFile("gherkin_policy/data_governance.feature")
	if err != nil {
		log.Fatal(err)
	}

	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatal(err)
	}
	defer client.Shutdown(ctx)

	fmt.Println("=== Gherkin Policy Demo ===")
	fmt.Println()
	fmt.Println("Feature file defines two policies:")
	fmt.Println("  1. Block PII-labeled users from calling llm_generate")
	fmt.Println("  2. Block unverified users from calling sensitive_action")
	fmt.Println()

	if err := client.LoadGherkinPolicy(ctx, string(featureContent)); err != nil {
		log.Fatal(err)
	}

	// Assess injects action_operation("Req", ActionName) automatically
	// from ActionMetadata, so the compiled halt rules fire correctly.
	cases := []case_{
		{
			Name:     "PII label → halt",
			Action:   "llm_generate",
			Label:    "pii",
			WantHalt: true,
			WantMsg:  "PII data must not leave the organization",
		},
		{
			Name:     "Public label → proceed",
			Action:   "llm_generate",
			Label:    "public",
			WantHalt: false,
		},
		{
			Name:      "Unverified user → halt",
			Action:    "sensitive_action",
			MetaKey:   "user_verified",
			MetaValue: "false",
			WantHalt:  true,
			WantMsg:   "User must be verified before sensitive actions",
		},
		{
			Name:      "Verified user → proceed",
			Action:    "sensitive_action",
			MetaKey:   "user_verified",
			MetaValue: "true",
			WantHalt:  false,
		},
	}

	allPass := true
	for i, tc := range cases {
		fmt.Printf("--- Case %d: %s ---\n", i+1, tc.Name)

		env := core.NewEnvelope(fmt.Sprintf("payload for %s", tc.Action))
		if tc.Label != "" {
			env.AddLabel(tc.Label)
		}
		if tc.MetaKey != "" {
			env.SetMeta(tc.MetaKey, tc.MetaValue)
		}

		// Assess returns AlignmentError when a halt rule fires.
		err := client.Engine().Assess(ctx, core.ActionMetadata{Name: tc.Action}, env)
		gotHalt := err != nil && core.IsAlignmentError(err)

		if gotHalt {
			fmt.Println("  Outcome: HALT")
			alignErr := err.(*core.AlignmentError)
			fmt.Printf("  Reason:  %s\n", alignErr.Message)
		} else if err != nil {
			fmt.Printf("  Outcome: ERROR (%v)\n", err)
			allPass = false
		} else {
			fmt.Println("  Outcome: PROCEED")
		}

		if gotHalt != tc.WantHalt {
			fmt.Printf("  FAIL: want halt=%v, got halt=%v\n", tc.WantHalt, gotHalt)
			allPass = false
		}

		if tc.WantHalt && tc.WantMsg != "" {
			if alignErr, ok := err.(*core.AlignmentError); ok {
				if alignErr.Message != tc.WantMsg {
					fmt.Printf("  FAIL: expected reason %q, got %q\n", tc.WantMsg, alignErr.Message)
					allPass = false
				}
			} else {
				fmt.Printf("  FAIL: expected reason %q, got no alignment error\n", tc.WantMsg)
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
