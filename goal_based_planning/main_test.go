package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

func TestPlanOnboardUser(t *testing.T) {
	ctx := context.Background()

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize client: %v", err)
	}

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
