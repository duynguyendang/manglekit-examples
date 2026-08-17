// custom_policy_format demonstrates the policy-loader extension point: an
// application-defined policy DSL is compiled to Datalog in user code and
// loaded through LoadPolicy, without any compilation support inside the
// manglekit engine.
//
// The toy DSL (policy.rules) is line-based:
//
//	deny action "<action>" [label "<label>" | meta "<key>" "<value>"] -> "<message>"
//
// compileRules turns each line into a halt Datalog rule that fires on the
// facts the gate injects for Assess — action_operation("Req", Name),
// label("pii"), and meta("key", "value").
//
// Demonstrates: custom loader → Datalog → Client.LoadPolicy, Assess
// (returns AlignmentError on blocks). This is the documented replacement
// for the removed built-in Gherkin compiler.
//
// No API key required (deterministic).

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/core"
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

var denyRe = regexp.MustCompile(`^deny action "([^"]+)" (label "([^"]+)"|meta "([^"]+)" "([^"]+)") -> "([^"]+)"$`)

// compileRule compiles one DSL line into a Datalog halt rule. Rules reference
// the facts the engine injects for the current Assess call:
//
//	action_operation("Req", ActionName)  — always present
//	label("pii")                          — a security label on the envelope
//	meta("key", "value")                 — an envelope metadata entry
func compileRule(line string) (string, error) {
	m := denyRe.FindStringSubmatch(strings.TrimSpace(line))
	if m == nil {
		return "", fmt.Errorf("unrecognized rule: %q", line)
	}
	action := m[1]
	message := m[6]
	actionFact := fmt.Sprintf(`action_operation("Req", %q)`, action)

	var cond string
	switch {
	case m[2] != "" && strings.HasPrefix(m[2], "label"):
		cond = fmt.Sprintf(`label(%q)`, m[3])
	case m[2] != "" && strings.HasPrefix(m[2], "meta"):
		cond = fmt.Sprintf(`meta(%q, %q)`, m[4], m[5])
	}
	return fmt.Sprintf(`halt("Req", %q, "T3") :- %s, %s.`, message, actionFact, cond), nil
}

// compileRules compiles every non-empty, non-comment line into a Datalog
// program string.
func compileRules(src string) (string, error) {
	var b strings.Builder
	for i, line := range strings.Split(src, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		rule, err := compileRule(line)
		if err != nil {
			return "", fmt.Errorf("policy line %d: %w", i+1, err)
		}
		b.WriteString(rule)
		b.WriteString("\n")
	}
	return b.String(), nil
}

func main() {
	ctx := context.Background()

	// MustReadFile is cwd-independent: relative paths resolve against this
	// file's directory when the cwd does not contain them.
	src := string(manglekit.MustReadFile("policy.rules"))

	policy, err := compileRules(src)
	if err != nil {
		log.Fatal(err)
	}

	client := manglekit.MustNewClient(ctx)
	defer client.Shutdown(ctx)

	// The extension point: the compiled Datalog is loaded like any other
	// .dl policy — no special engine support is required.
	if err := client.LoadPolicy(ctx, policy); err != nil {
		log.Fatal(err)
	}

	fmt.Println("=== Custom Policy Format Demo ===")
	fmt.Println()
	fmt.Println("Generated Datalog policy:")
	fmt.Println(policy)
	fmt.Println("Two rules:")
	fmt.Println("  1. Block PII-labeled users from calling llm_generate")
	fmt.Println("  2. Block unverified users from calling sensitive_action")
	fmt.Println()

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