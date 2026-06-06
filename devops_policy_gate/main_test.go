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

func setupClient(t *testing.T) *sdk.Client {
	t.Helper()
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
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

func TestScaleTooHigh(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["scale_too_high"] = "true"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected scale_too_high=true to be blocked")
	}
}

func TestScaleWithinLimit(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["target_replicas"] = "5"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "kubectl_scale"}, env)
	if core.IsAlignmentError(err) {
		t.Errorf("expected scale within limit to be allowed, got: %v", err)
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
	env.Metadata["current_hour"] = "14"
	env.Metadata["has_approval"] = "true"
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "terraform_destroy"}, env)
	if !core.IsAlignmentError(err) {
		t.Error("expected destroy during business hours to be blocked")
	}
}

func TestDestroyAfterHours(t *testing.T) {
	client := setupClient(t)
	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{})
	env.Metadata["current_hour"] = "22"
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
