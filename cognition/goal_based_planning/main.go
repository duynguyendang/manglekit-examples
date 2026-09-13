// goal_based_planning answers: Can an agent plan multi-step work that still needs approval before production? (UC-O1)

package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// --- Constants: Datalog Policy ---

const approvalPolicy = `
		halt("Req", "deploy_production blocked: requires manual approval") :-
			action_operation("Req", "deploy_production"),
			!meta("has_approval", "true").
	`

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// MockAction is a simple action that logs execution and returns success.
type MockAction struct {
	name string
}

func (a *MockAction) Execute(ctx context.Context, input core.Envelope) (core.Envelope, error) {
	fmt.Printf("   -> Executing action: %s\n", a.name)
	output := core.NewEnvelope(fmt.Sprintf("completed: %s", a.name))
	output.SetMeta("status", "success")
	// NOTE: The Zero-Trust gate (SupervisedAction.ExecuteInternal) builds the
	// action's inner envelope from the *payload* only — it does NOT thread the
	// caller's envelope metadata into the action input or output. So downstream
	// gated steps can't see facts carried on the initial plan envelope. To model
	// a successful pipeline where prerequisites are satisfied, each mock step
	// stamps the prerequisite facts its successors' halt rules require
	// (tests_passed for deploy_staging, has_approval for deploy_production).
	output.SetMeta("tests_passed", "true")
	output.SetMeta("has_approval", "true")
	return output, nil
}

// recordingAction wraps an action and increments a counter each time it runs.
// Used by the supervised-plan demonstration to prove the registered (supervised)
// actions were actually invoked by ExecutePlan.
type recordingAction struct {
	MockAction
	count *int
}

func (a *recordingAction) Execute(ctx context.Context, input core.Envelope) (core.Envelope, error) {
	*a.count++
	return a.MockAction.Execute(ctx, input)
}

func (a *MockAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: a.name}
}

// FailingAction simulates a prerequisite failure.
type FailingAction struct {
	name string
}

func (a *FailingAction) Execute(ctx context.Context, input core.Envelope) (core.Envelope, error) {
	return core.Envelope{}, fmt.Errorf("prerequisite missing for action: %s", a.name)
}

func (a *FailingAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: a.name}
}

func main() {
	ctx := context.Background()

	fmt.Println("Goal-Based Planning with Manglekit")
	fmt.Println("===================================")
	fmt.Println("Demonstrating Datalog-driven action planning:")
	fmt.Println("1. Goal decomposition into ordered plan steps")
	fmt.Println("2. Prerequisite verification via Datalog queries")
	fmt.Println("3. Policy enforcement at each plan step")
	fmt.Println()

	// 1. Initialize Manglekit Client
	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	// 2. Load Planning Policy
	policyBytes, err := os.ReadFile(filepath.Join(exampleDir(), "planning_rules.dl"))
	if err != nil {
		log.Fatalf("Failed to read planning_rules.dl: %v", err)
	}

	if err := client.Engine().LoadPolicy(ctx, string(policyBytes)); err != nil {
		log.Fatalf("Failed to load planning policy: %v", err)
	}
	fmt.Println("Loaded planning_rules.dl policy with goal decomposition rules.")
	fmt.Println()

	// 3. Plan for "deploy_to_production"
	fmt.Println("--- Plan 1: deploy_to_production ---")
	deploySteps, err := client.Plan(ctx, "deploy_to_production")
	if err != nil {
		log.Fatalf("Planning failed for deploy_to_production: %v", err)
	}
	fmt.Printf("Generated %d plan steps:\n", len(deploySteps))
	for i, step := range deploySteps {
		fmt.Printf("  %d. %s (order=%d)\n", i+1, step.ActionName, step.Order)
	}
	fmt.Println()

	// 4. Plan for "onboard_user"
	fmt.Println("--- Plan 2: onboard_user ---")
	onboardSteps, err := client.Plan(ctx, "onboard_user")
	if err != nil {
		log.Fatalf("Planning failed for onboard_user: %v", err)
	}
	fmt.Printf("Generated %d plan steps:\n", len(onboardSteps))
	for i, step := range onboardSteps {
		fmt.Printf("  %d. %s (order=%d)\n", i+1, step.ActionName, step.Order)
	}
	fmt.Println()

	// 5. Verify prerequisites using Query
	fmt.Println("--- Prerequisite Verification ---")
	verifyPrerequisites(ctx, client)
	fmt.Println()

	// 6. Register mock actions and execute the deploy plan
	fmt.Println("--- Executing deploy_to_production Plan ---")
	registerDeployActions(client)
	// The deploy steps are now SUPERVISED, so the Zero-Trust gate runs at each
	// step. planning_rules.dl halts deploy_staging unless tests_passed=true and
	// deploy_production unless has_approval=true; the MockAction stamps those
	// prerequisite facts on its output so each gated successor sees them and the
	// gate allows the step to proceed (see MockAction.Execute).
	result, err := client.ExecutePlan(ctx, deploySteps, core.NewEnvelope("deploy-request-001"))
	if err != nil {
		fmt.Printf("Plan execution failed: %v\n", err)
	} else {
		fmt.Printf("Plan completed successfully. Final output: %v\n", result.Payload)
	}
	fmt.Println()

	// 7. Register mock actions and execute the onboard plan
	fmt.Println("--- Executing onboard_user Plan ---")
	registerOnboardActions(client)
	result, err = client.ExecutePlan(ctx, onboardSteps, core.NewEnvelope("onboard-request-001"))
	if err != nil {
		fmt.Printf("Plan execution failed: %v\n", err)
	} else {
		fmt.Printf("Plan completed successfully. Final output: %v\n", result.Payload)
	}
	fmt.Println()

	// 8. Demonstrate missing prerequisite causing plan failure
	fmt.Println("--- Demonstrating Missing Prerequisite Failure ---")
	demonstratePrerequisiteFailure(ctx, client)
	fmt.Println()

	// 9. Demonstrate policy violation detection
	fmt.Println("--- Demonstrating Policy Violation Detection ---")
	demonstratePolicyViolation(ctx, client)
	fmt.Println()

	fmt.Println("Goal-Based Planning demonstration complete!")
}

func verifyPrerequisites(ctx context.Context, client *sdk.Client) {
	// Check if deploy_production requires approval
	facts := []string{
		`requires("deploy_production", "approval")`,
	}
	query := `requires("deploy_production", What)`
	results, err := client.Engine().Query(ctx, facts, query)
	if err != nil {
		fmt.Printf("  Query error: %v\n", err)
		return
	}
	if len(results) > 0 {
		fmt.Printf("  deploy_production requires: %s\n", results[0]["What"])
	}

	// Check if deploy_staging requires tests_pass
	facts = []string{
		`requires("deploy_staging", "tests_pass")`,
	}
	query = `requires("deploy_staging", What)`
	results, err = client.Engine().Query(ctx, facts, query)
	if err != nil {
		fmt.Printf("  Query error: %v\n", err)
		return
	}
	if len(results) > 0 {
		fmt.Printf("  deploy_staging requires: %s\n", results[0]["What"])
	}
}

func registerDeployActions(client *sdk.Client) {
	deploySteps := []string{
		"check_tests",
		"check_lint",
		"deploy_staging",
		"run_smoke_tests",
		"deploy_production",
	}
	// Register SUPERVISED wrappers. ExecutePlan (sdk/executor.go) chains
	// ExecuteByName for each step, which invokes whatever is registered here —
	// so registering supervised actions makes every plan step go through the
	// Zero-Trust Gatekeeper (pre-check).
	for _, step := range deploySteps {
		client.RegisterAction(step, client.Supervise(&MockAction{name: step}))
	}
	fmt.Println("  Registered (supervised) deploy actions: check_tests, check_lint, deploy_staging, run_smoke_tests, deploy_production")
}

func registerOnboardActions(client *sdk.Client) {
	onboardSteps := []string{
		"create_account",
		"assign_role",
		"send_welcome",
		"setup_mfa",
	}
	for _, step := range onboardSteps {
		client.RegisterAction(step, client.Supervise(&MockAction{name: step}))
	}
	fmt.Println("  Registered (supervised) onboard actions: create_account, assign_role, send_welcome, setup_mfa")
}

func demonstratePrerequisiteFailure(ctx context.Context, client *sdk.Client) {
	// Register a failing action for deploy_production to simulate missing approval
	client.RegisterAction("deploy_production", &FailingAction{name: "deploy_production"})

	steps, err := client.Plan(ctx, "deploy_to_production")
	if err != nil {
		log.Fatalf("Planning failed: %v", err)
	}

	fmt.Println("  Executing plan with failing deploy_production (missing approval)...")
	// Provide the facts the gate requires so the plan clears the supervised
	// pre-checks (tests_passed, has_approval) and the failure surfaces from the
	// registered FailingAction at the deploy_production step.
	failEnv := core.NewEnvelope("deploy-fail-test")
	failEnv.SetMeta("tests_passed", "true")
	failEnv.SetMeta("has_approval", "true")
	_, err = client.ExecutePlan(ctx, steps, failEnv)
	if err != nil {
		fmt.Printf("  Plan failed as expected: %v\n", err)
	}

	// Restore the mock action
	client.RegisterAction("deploy_production", &MockAction{name: "deploy_production"})
}

// demonstratePolicyViolation shows the approval gate in action.
//
// NOTE: This now runs through the governed path (Supervise + ExecuteByName),
// not a raw Engine().Assess() call. The approvalPolicy halt rule is
// `action_operation("Req","deploy_production"), !meta("has_approval","true")`.
// ExecuteByName injects action_operation("Req","deploy_production") automatically,
// and WithMetadata supplies the meta(...) facts, so the gate is exercised for real.
//
// (For contrast: a pure Engine().Assess demo bypasses the supervisor entirely.
// The governed path is ExecuteByName — pre- AND post-check fail-closed since
// ADR-001 — which is what plan steps actually use via ExecutePlan.)
func demonstratePolicyViolation(ctx context.Context, client *sdk.Client) {
	// Load a policy that blocks deploy_production without approval.
	if err := client.Engine().LoadPolicy(ctx, approvalPolicy); err != nil {
		fmt.Printf("  Failed to load policy: %v\n", err)
		return
	}

	// Register a supervised deploy_production action. Because ExecutePlan (and
	// ExecuteByName) invoke whatever is registered, a supervised wrapper means
	// the gate runs on this action too.
	client.RegisterAction("deploy_production", client.Supervise(&MockAction{name: "deploy_production"}))

	// Without approval: gate blocks via the halt rule -> PolicyViolationError.
	_, err := client.ExecuteByName(ctx, "deploy_production", "test-deploy",
		sdk.WithMetadata("has_approval", "false"))
	if core.IsPolicyViolationError(err) {
		fmt.Printf("  Policy violation detected (no approval): %v\n", err)
	} else if err != nil {
		fmt.Printf("  Unexpected error: %v\n", err)
	} else {
		fmt.Println("  No policy violation (unexpected)")
	}

	// With approval: gate allows the action through.
	_, err = client.ExecuteByName(ctx, "deploy_production", "test-deploy-approved",
		sdk.WithMetadata("has_approval", "true"))
	if core.IsPolicyViolationError(err) {
		fmt.Printf("  Policy violation (unexpected): %v\n", err)
	} else if err != nil {
		fmt.Printf("  Unexpected error: %v\n", err)
	} else {
		fmt.Println("  Approved: deploy_production allowed with approval.")
	}
}
