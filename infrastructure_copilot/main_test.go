package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/duynguyendang/manglekit"
)

func repoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filename))
}

func TestK8sGuardrailAllowed(t *testing.T) {
	ctx := context.Background()
	client := manglekit.Must(manglekit.NewClient(ctx, manglekit.WithBlueprintPath(filepath.Join(repoRoot(), "infrastructure_copilot/safety.dl"))))

	deletePod := func(ctx context.Context, req KubernetesRequest) (string, error) {
		return "ok", nil
	}
	action := manglekit.Define(client, "k8s_guardrail", deletePod)

	req := KubernetesRequest{
		Operation:  "READ",
		IsPeakHour: "true",
		Namespace:  "production",
		Tier:       "web",
	}
	if _, err := action.Run(ctx, req); err != nil {
		t.Fatalf("Read in production should be allowed: %v", err)
	}
}

func TestK8sGuardrailDeniedDeleteCritical(t *testing.T) {
	ctx := context.Background()
	client := manglekit.Must(manglekit.NewClient(ctx, manglekit.WithBlueprintPath(filepath.Join(repoRoot(), "infrastructure_copilot/safety.dl"))))

	deletePod := func(ctx context.Context, req KubernetesRequest) (string, error) {
		return "ok", nil
	}
	action := manglekit.Define(client, "k8s_guardrail", deletePod)

	req := KubernetesRequest{
		Operation:  "DELETE",
		IsPeakHour: "false",
		Namespace:  "default",
		Tier:       "critical",
	}
	if _, err := action.Run(ctx, req); err == nil {
		t.Fatal("Delete critical pod should be denied")
	}
}

func TestK8sGuardrailDeniedWriteDuringPeak(t *testing.T) {
	ctx := context.Background()
	client := manglekit.Must(manglekit.NewClient(ctx, manglekit.WithBlueprintPath(filepath.Join(repoRoot(), "infrastructure_copilot/safety.dl"))))

	deletePod := func(ctx context.Context, req KubernetesRequest) (string, error) {
		return "ok", nil
	}
	action := manglekit.Define(client, "k8s_guardrail", deletePod)

	req := KubernetesRequest{
		Operation:  "UPDATE",
		IsPeakHour: "true",
		Namespace:  "production",
		Tier:       "web",
	}
	if _, err := action.Run(ctx, req); err == nil {
		t.Fatal("Update in production during peak should be denied")
	}
}
