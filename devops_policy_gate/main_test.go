package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

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

// addNum is a tiny test helper that injects a numeric atom.
func addNum(t *testing.T, env *core.Envelope, pred string, n int) {
	t.Helper()
	env.Facts = append(env.Facts, fmt.Sprintf("%s(%d).", pred, n))
}

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
