package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func setupClient(t *testing.T) *sdk.Client {
	t.Helper()
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	_, filename, _, _ := runtime.Caller(0)
	dir := filepath.Dir(filename)
	policyBytes, err := os.ReadFile(filepath.Join(dir, "security_gate.dl"))
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyBytes)); err != nil {
		t.Fatal(err)
	}
	return client
}

// addNum is a tiny test helper that injects a numeric atom. Left in place for
// the pure Assess tests; the supervised ExecuteByName path ignores it because
// it only forwards Metadata, not Facts.
func addNum(t *testing.T, env *core.Envelope, pred string, n int) {
	t.Helper()
	env.Facts = append(env.Facts, fmt.Sprintf("%s(%d).", pred, n))
}

// registerSupervisedNoOp registers a supervised, recording no-op action under
// name and returns a pointer to its execution counter so callers can assert
// whether the inner action actually ran.
func registerSupervisedNoOp(t *testing.T, client *sdk.Client, name string) *int32 {
	t.Helper()
	var executed int32
	act := function.New(name, func(_ context.Context, _ map[string]string) (string, error) {
		atomic.AddInt32(&executed, 1)
		return "ok", nil
	})
	client.RegisterAction(name, client.Supervise(act))
	return &executed
}

// ============================================================================
// PURE POLICY-DECISION TESTS (via Engine().Assess)
//
// These exercise the policy engine directly. Assess DOES inject
// action_operation("Req", Name) from ActionMetadata.Name and forwards
// env.Metadata as meta/2, so the meta-based halt rules fire. This is a direct
// policy check, NOT the governed ExecuteByName path: it bypasses the
// supervisor entirely, so it does not prove the inner action was blocked by
// the gate — only that the policy would halt. Keep them as regression guards
// for the Datalog rules themselves.
// ============================================================================

func TestScaleTooHigh(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	addNum(t, &env, "target_replicas", 20)
	attachPrecomputedChecks(&env, "20", "")
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected scale_too_high (20 > 10) to be blocked")
	}
}

func TestScaleAtLimit(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	addNum(t, &env, "target_replicas", 11)
	attachPrecomputedChecks(&env, "11", "")
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected 11 > 10 to be blocked")
	}
}

func TestScaleWithinLimit(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	addNum(t, &env, "target_replicas", 5)
	attachPrecomputedChecks(&env, "5", "")
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, env)
	if core.IsAlignmentError(err) {
		t.Errorf("expected scale within limit to be allowed, got: %v", err)
	}
}

func TestScaleZeroBlocked(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	addNum(t, &env, "target_replicas", 0)
	attachPrecomputedChecks(&env, "0", "")
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected 0 replicas to be blocked")
	}
}

func TestOpenSecurityGroup(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["has_open_security_group"] = "true"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_apply"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected open security group to be blocked")
	}
}

func TestRestrictedSecurityGroup(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["has_open_security_group"] = "false"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_apply"}, env)
	if core.IsAlignmentError(err) {
		t.Errorf("expected restricted security group to be allowed, got: %v", err)
	}
}

func TestProdDeployWithoutApproval(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["target_env"] = "production"
	env.Metadata["has_approval"] = "false"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_deploy"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected unapproved production deploy to be blocked")
	}
}

func TestProdDeployWithApproval(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["target_env"] = "production"
	env.Metadata["has_approval"] = "true"
	env.Metadata["has_rollback_plan"] = "true" // required by the T1 rule
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_deploy"}, env)
	if core.IsAlignmentError(err) {
		t.Errorf("expected approved production deploy to be allowed, got: %v", err)
	}
}

func TestDestroyDuringBusinessHours(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	addNum(t, &env, "current_hour", 14)
	attachPrecomputedChecks(&env, "", "14")
	env.Metadata["has_approval"] = "true"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_destroy"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected destroy during business hours (14) to be blocked")
	}
}

func TestDestroyAtBoundary(t *testing.T) {
	// 17:00 UTC is the last hour still considered "business hours" (H < 18).
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	addNum(t, &env, "current_hour", 17)
	attachPrecomputedChecks(&env, "", "17")
	env.Metadata["has_approval"] = "true"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_destroy"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected destroy at 17:00 to still be blocked")
	}
}

func TestDestroyAfterHours(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	addNum(t, &env, "current_hour", 22)
	attachPrecomputedChecks(&env, "", "22")
	env.Metadata["has_approval"] = "true"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_destroy"}, env)
	if core.IsAlignmentError(err) {
		t.Errorf("expected destroy after hours to be allowed, got: %v", err)
	}
}

func TestPublicDatabase(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["db_publicly_accessible"] = "true"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_apply"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected public database to be blocked")
	}
}

// ============================================================================
// SUPERVISED PATH TESTS (via Supervise + ExecuteByName)
//
// These pin the REAL governed contract: a halt rule must produce a
// *core.PolicyViolationError from ExecuteByName AND the inner action must NOT
// run (pre-check block). Allowed paths must run the inner action with err==nil.
// The blocks here exercise the PRE-CHECK; the POST-check is also
// fail-closed since ADR-001 (see http_service for reload, stream showcase
// for streaming coverage).
// ============================================================================

func TestSupervisedScaleTooHighBlocked(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	executed := registerSupervisedNoOp(t, client, "kubectl_scale")

	_, err := client.ExecuteByName(ctx, "kubectl_scale", map[string]string{},
		sdk.WithMetadata("scale_too_high", "true"))
	if !core.IsPolicyViolationError(err) {
		t.Fatalf("expected PolicyViolationError, got: %v", err)
	}
	if atomic.LoadInt32(executed) != 0 {
		t.Errorf("inner action should NOT have executed, ran %d time(s)", atomic.LoadInt32(executed))
	}
}

func TestSupervisedScaleWithinLimitAllowed(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	executed := registerSupervisedNoOp(t, client, "kubectl_scale")

	// No scale flag set => not a violation.
	_, err := client.ExecuteByName(ctx, "kubectl_scale", map[string]string{},
		sdk.WithMetadata("scale_too_high", "false"))
	if err != nil {
		t.Fatalf("expected allowed, got error: %v", err)
	}
	if atomic.LoadInt32(executed) != 1 {
		t.Errorf("inner action should have executed exactly once, ran %d time(s)", atomic.LoadInt32(executed))
	}
}

func TestSupervisedProdDeployNoApprovalBlocked(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	executed := registerSupervisedNoOp(t, client, "kubectl_deploy")

	_, err := client.ExecuteByName(ctx, "kubectl_deploy", map[string]string{},
		sdk.WithMetadata("target_env", "production"),
		sdk.WithMetadata("has_approval", "false"))
	if !core.IsPolicyViolationError(err) {
		t.Fatalf("expected PolicyViolationError, got: %v", err)
	}
	if atomic.LoadInt32(executed) != 0 {
		t.Errorf("inner action should NOT have executed, ran %d time(s)", atomic.LoadInt32(executed))
	}
}

func TestSupervisedOpenSecurityGroupBlocked(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	executed := registerSupervisedNoOp(t, client, "terraform_apply")

	_, err := client.ExecuteByName(ctx, "terraform_apply", map[string]string{},
		sdk.WithMetadata("has_open_security_group", "true"))
	if !core.IsPolicyViolationError(err) {
		t.Fatalf("expected PolicyViolationError, got: %v", err)
	}
	if atomic.LoadInt32(executed) != 0 {
		t.Errorf("inner action should NOT have executed, ran %d time(s)", atomic.LoadInt32(executed))
	}
}

// ============================================================================
// Tier semantics (P0.7) + explainable governance
// ============================================================================

func TestSupervisedTierT1Blocks(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	executed := registerSupervisedNoOp(t, client, "kubectl_deploy")

	_, err := client.ExecuteByName(ctx, "kubectl_deploy", map[string]string{},
		sdk.WithMetadata("target_env", "production"),
		sdk.WithMetadata("has_approval", "true")) // but no rollback plan
	if !core.IsPolicyViolationError(err) {
		t.Fatalf("T1 rule must block, got %v", err)
	}
	var pve *core.PolicyViolationError
	if !errors.As(err, &pve) {
		t.Fatal("expected *core.PolicyViolationError")
	}
	if pve.Tier != "T1" {
		t.Errorf("structured deny Tier = %q, want T1", pve.Tier)
	}
	if !strings.Contains(pve.MatchedRule, "rollback plan") {
		t.Errorf("MatchedRule = %q, want the T1 rollback rule", pve.MatchedRule)
	}
	if pve.ActionName != "kubectl_deploy" {
		t.Errorf("ActionName = %q", pve.ActionName)
	}
	if atomic.LoadInt32(executed) != 0 {
		t.Error("inner action must not run on T1 deny")
	}
}

// The same shape at T2: advisory — execution proceeds. This is the P0.7
// contract: explicit soft tiers never block, tier-less/unknown still do.
func TestSupervisedTierT2AdvisoryAllows(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	executed := registerSupervisedNoOp(t, client, "kubectl_deploy")

	_, err := client.ExecuteByName(ctx, "kubectl_deploy", map[string]string{},
		sdk.WithMetadata("target_env", "production"),
		sdk.WithMetadata("has_approval", "true"),
		sdk.WithMetadata("has_rollback_plan", "true"),
		sdk.WithMetadata("has_canary", "false")) // trips the T2 playbook rule
	if err != nil {
		t.Fatalf("T2 advisory must not block: %v", err)
	}
	if atomic.LoadInt32(executed) != 1 {
		t.Errorf("inner action should have run once, ran %d", atomic.LoadInt32(executed))
	}
}

// Explain turns a deny into a proof tree — the "why" is queryable, not a log
// scavenger hunt.
func TestExplainableDenyProof(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()

	expl, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, []string{
		`action_operation("Req", "kubectl_deploy").`,
		`meta("target_env", "production").`,
		`meta("has_approval", "true").`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !expl.Outcome {
		t.Fatal("expected the T1 rollback rule to derive a halt")
	}
	rendered := expl.String()
	if !strings.Contains(rendered, "rollback plan") || !strings.Contains(rendered, "[T1]") {
		t.Errorf("proof tree missing rule/tier:\n%s", rendered)
	}
	if !strings.Contains(rendered, "!meta(\"has_rollback_plan\",\"true\")") {
		t.Errorf("proof tree should expose the failed negation:\n%s", rendered)
	}
}
