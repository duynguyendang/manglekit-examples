package main

import (
	"context"
	"os"
	"testing"

	"github.com/duynguyendang/manglekit/sdk"
)

func loadConstraints(t *testing.T, ctx context.Context, client *sdk.Client) {
	t.Helper()
	policyData, err := os.ReadFile("constraints.dl")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyData)); err != nil {
		t.Fatal(err)
	}
}

func TestWrongSolutionRejected(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadConstraints(t, ctx, client)

	facts := []string{`solution("x", 3).`, `solution("y", 3).`}
	solutions, err := client.Engine().Query(ctx, facts, `violation(Reason)`)
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if len(solutions) == 0 {
		t.Error("expected violations for x=3, y=3 (sum=6 ≠ 10), got none")
	}
}

func TestCorrectSolutionAccepted(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadConstraints(t, ctx, client)

	facts := []string{`solution("x", 7).`, `solution("y", 3).`}
	solutions, err := client.Engine().Query(ctx, facts, `violation(Reason)`)
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if len(solutions) != 0 {
		t.Errorf("expected zero violations for x=7, y=3, got %d", len(solutions))
	}
}

func TestNeverCertifiesWrong(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadConstraints(t, ctx, client)

	// An always-wrong mock: x=0, y=0 (violates all three constraints)
	facts := []string{`solution("x", 0).`, `solution("y", 0).`}
	solutions, err := client.Engine().Query(ctx, facts, `violation(Reason)`)
	if err != nil {
		t.Fatalf("Query error: %v", err)
	}
	if len(solutions) == 0 {
		t.Error("NEVER certify wrong: x=0, y=0 should produce violations")
	}
}

func TestVerifyRetryConverges(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadConstraints(t, ctx, client)

	mock := &scriptedMock{}
	maxAttempts := 5

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		x, y := mock.Next()
		facts := []string{
			`solution("x", ` + itoa(x) + `).`,
			`solution("y", ` + itoa(y) + `).`,
		}
		solutions, err := client.Engine().Query(ctx, facts, `violation(Reason)`)
		if err != nil {
			t.Fatalf("Query error on attempt %d: %v", attempt, err)
		}
		if len(solutions) == 0 {
			if attempt != 3 {
				t.Errorf("expected convergence on attempt 3, converged on attempt %d", attempt)
			}
			return
		}
	}
	t.Error("verify-retry loop did not converge within max attempts")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	if n < 0 {
		return "-" + itoa(-n)
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}
