// verified_reasoning demonstrates a cheap model + symbolic verifier
// achieving certified-correct output.
//
// A scripted mock LLM answers a constraint problem (x=7, y=3 is the
// only valid solution) wrong → wrong → right. An explicit verify-retry
// loop (not the built-in steering retry, which is gated behind
// steeringEnabled) uses Engine().Query("violation(Reason)") to reject
// and feed back until the symbolic layer certifies zero violations.
//
// Models on math_solver. No API key required (deterministic mock).

package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// scriptedMock returns pre-baked answers: wrong, wrong, right.
type scriptedMock struct {
	attempt int
}

func (m *scriptedMock) Next() (x, y int) {
	m.attempt++
	switch m.attempt {
	case 1:
		return 3, 3 // wrong: x != 7
	case 2:
		return 7, 2 // wrong: y != 3
	default:
		return 7, 3 // right
	}
}

func main() {
	ctx := context.Background()

	policyData, err := os.ReadFile("verified_reasoning/constraints.dl")
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

	mock := &scriptedMock{}
	maxAttempts := 5

	// verifierErr tracks the most recent verifier query error so a
	// non-convergence failure can report a cause instead of going silent.
	var verifierErr error
	verifierFailures := 0

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		x, y := mock.Next()
		fmt.Printf("--- Attempt %d: proposing x=%d, y=%d ---\n", attempt, x, y)

		facts := []string{
			fmt.Sprintf(`solution("x", %d).`, x),
			fmt.Sprintf(`solution("y", %d).`, y),
		}

		// Query for violations
		solutions, err := client.Engine().Query(ctx, facts, `violation(Reason)`)
		if err != nil {
			verifierErr = err
			verifierFailures++
			fmt.Printf("  Query error: %v\n", err)
			// Fail fast: a persistent verifier error means the symbolic
			// layer is broken; retrying will not recover.
			if verifierFailures >= 2 {
				fmt.Printf("FAIL: verifier query errored %d times; last error: %v\n", verifierFailures, verifierErr)
				os.Exit(1)
			}
			continue
		}

		if len(solutions) == 0 {
			fmt.Println("  VERIFIED: zero violations — solution is certified correct.")
			env := core.NewEnvelope(fmt.Sprintf("solve constraint: x=%d, y=%d", x, y))
			env.Facts = facts
			decision, planErr := client.Engine().AssessPlan(ctx, env)
			if planErr != nil {
				fmt.Printf("  AssessPlan error: %v\n", planErr)
			}
			fmt.Printf("  AssessPlan outcome: %s\n", decision.Outcome)
			if decision.AuditTrail != nil {
				fmt.Printf("  AuditTrail: %d rules matched\n", len(decision.AuditTrail.MatchedRules))
			}
			fmt.Println("\nCertified correct.")
			return
		}

		reasons := make([]string, 0, len(solutions))
		for _, sol := range solutions {
			if reason, ok := sol["Reason"]; ok {
				reasons = append(reasons, reason)
			}
		}
		fmt.Printf("  VIOLATIONS: %v\n", reasons)
		fmt.Println("  Rejecting — feeding violations back to mock solver.")
	}

	if verifierErr != nil {
		fmt.Printf("FAIL: did not converge within %d attempts (last verifier error: %v)\n", maxAttempts, verifierErr)
	} else {
		fmt.Printf("FAIL: did not converge within %d attempts\n", maxAttempts)
	}
	os.Exit(1)
}

