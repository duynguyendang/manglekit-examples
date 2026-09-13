package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func testExampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

func loadTestPolicy(t *testing.T, ctx context.Context, client *sdk.Client) {
	t.Helper()
	policyBytes, err := os.ReadFile(filepath.Join(testExampleDir(), "planning_rules.dl"))
	if err != nil {
		t.Fatalf("Failed to read planning_rules.dl: %v", err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyBytes)); err != nil {
		t.Fatalf("Failed to load planning policy: %v", err)
	}
}

func TestPlanDeployToProduction(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	loadTestPolicy(t, ctx, client)

	steps, err := client.Plan(ctx, "deploy_to_production")
	if err != nil {
		t.Fatalf("Plan failed for deploy_to_production: %v", err)
	}

	if len(steps) != 5 {
		t.Fatalf("Expected 5 plan steps, got %d", len(steps))
	}

	expectedSteps := []struct {
		action string
		order  int
	}{
		{"check_tests", 1},
		{"check_lint", 2},
		{"deploy_staging", 3},
		{"run_smoke_tests", 4},
		{"deploy_production", 5},
	}

	for i, expected := range expectedSteps {
		if steps[i].ActionName != expected.action {
			t.Errorf("Step %d: expected action %q, got %q", i+1, expected.action, steps[i].ActionName)
		}
		if steps[i].Order != expected.order {
			t.Errorf("Step %d: expected order %d, got %d", i+1, expected.order, steps[i].Order)
		}
	}
}

// TestPolicyViolationCoversApprovalGate covers the scenario in
// main.go's demonstratePolicyViolation helper: the approval policy
// must block deploy_production when has_approval is false and allow
// it when true. Subtests exercise each branch.
//
// NOTE: This pins the REAL governed contract — the supervised
// Supervise+ExecuteByName path — rather than a raw Engine().Assess() demo.
// A pure Assess demo bypasses the supervisor. The governed path is
// ExecuteByName (post-check fail-closed since ADR-001).
func TestPolicyViolationCoversApprovalGate(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	if err := client.Engine().LoadPolicy(ctx, approvalPolicy); err != nil {
		t.Fatalf("Failed to load approval policy: %v", err)
	}

	// Register a supervised deploy_production action so the gate runs via
	// ExecuteByName (the same path ExecutePlan uses for plan steps).
	client.RegisterAction("deploy_production", client.Supervise(&MockAction{name: "deploy_production"}))

	t.Run("UnapprovedBlocked", func(t *testing.T) {
		_, err := client.ExecuteByName(ctx, "deploy_production", "test-deploy",
			sdk.WithMetadata("has_approval", "false"))
		if !core.IsPolicyViolationError(err) {
			t.Errorf("expected unapproved deploy to be blocked, got: %v", err)
		}
	})

	t.Run("ApprovedPermitted", func(t *testing.T) {
		_, err := client.ExecuteByName(ctx, "deploy_production", "test-deploy-approved",
			sdk.WithMetadata("has_approval", "true"))
		if err != nil {
			t.Errorf("expected approved deploy to be allowed, got: %v", err)
		}
	})
}

func TestPlanOnboardUser(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	loadTestPolicy(t, ctx, client)

	steps, err := client.Plan(ctx, "onboard_user")
	if err != nil {
		t.Fatalf("Plan failed for onboard_user: %v", err)
	}

	if len(steps) != 4 {
		t.Fatalf("Expected 4 plan steps, got %d", len(steps))
	}

	expectedSteps := []struct {
		action string
		order  int
	}{
		{"create_account", 1},
		{"assign_role", 2},
		{"send_welcome", 3},
		{"setup_mfa", 4},
	}

	for i, expected := range expectedSteps {
		if steps[i].ActionName != expected.action {
			t.Errorf("Step %d: expected action %q, got %q", i+1, expected.action, steps[i].ActionName)
		}
		if steps[i].Order != expected.order {
			t.Errorf("Step %d: expected order %d, got %d", i+1, expected.order, steps[i].Order)
		}
	}
}

// TestExecutePlanUsesSupervisedActions proves that ExecutePlan chains
// ExecuteByName (confirmed in sdk/executor.go) so that the REGISTERED
// (supervised) actions are the ones actually invoked for each plan step.
//
// A pure Engine().Assess demo would bypass the supervisor entirely; this
// test exercises the governed path. planning_rules.dl has no halt rules for
// the deploy steps, so the plan proceeds and all 5 supervised actions run.
func TestExecutePlanUsesSupervisedActions(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	loadTestPolicy(t, ctx, client)

	steps, err := client.Plan(ctx, "deploy_to_production")
	if err != nil {
		t.Fatalf("Plan failed for deploy_to_production: %v", err)
	}
	if len(steps) != 5 {
		t.Fatalf("Expected 5 plan steps, got %d", len(steps))
	}

	// Register supervised wrappers that increment a shared counter on execution.
	execCount := 0
	deploySteps := []string{
		"check_tests",
		"check_lint",
		"deploy_staging",
		"run_smoke_tests",
		"deploy_production",
	}
	for _, step := range deploySteps {
		client.RegisterAction(step, client.Supervise(&recordingAction{
			MockAction: MockAction{name: step},
			count:      &execCount,
		}))
	}

	// planning_rules.dl halts deploy_staging unless tests_passed=true and
	// deploy_production unless has_approval=true. Carry those facts so the
	// supervised gate allows the plan to proceed and all 5 actions run.
	deployEnv := core.NewEnvelope("deploy-request-001")
	deployEnv.SetMeta("tests_passed", "true")
	deployEnv.SetMeta("has_approval", "true")

	if _, err := client.ExecutePlan(ctx, steps, deployEnv); err != nil {
		t.Fatalf("ExecutePlan failed: %v", err)
	}

	if execCount != 5 {
		t.Fatalf("Expected all 5 supervised actions to execute, got %d", execCount)
	}
}
