package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/core"
)

func repoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filename))
}

func TestGoldTierRouting(t *testing.T) {
	ctx := context.Background()
	client := manglekit.Must(manglekit.NewClient(ctx, manglekit.WithBlueprintPath(filepath.Join(repoRoot(), "autonomous_router/blueprint.dl"))))

	client.RegisterAction("classify", client.Supervise(&RouterAction{}))
	client.RegisterAction("vip_agent", client.Supervise(&VIPAction{}))

	// Gold tier should route to VIP agent via the blueprint's route/1 rule.
	res, err := client.Action("classify").Execute(ctx, core.NewEnvelope(Input{Tier: "gold"}))
	if err != nil {
		t.Fatalf("Gold tier routing failed: %v", err)
	}
	val, ok := res.Payload.(string)
	if !ok {
		t.Fatalf("expected string payload from VIP agent, got %T", res.Payload)
	}
	if val != "VIP Service Executed" {
		t.Fatalf("expected VIP agent output, got %q", val)
	}
}

func TestNonGoldTierNoRouting(t *testing.T) {
	ctx := context.Background()
	client := manglekit.Must(manglekit.NewClient(ctx, manglekit.WithBlueprintPath(filepath.Join(repoRoot(), "autonomous_router/blueprint.dl"))))

	client.RegisterAction("classify", client.Supervise(&RouterAction{}))
	client.RegisterAction("vip_agent", client.Supervise(&VIPAction{}))

	// Silver tier has no route rule; should pass through as Input.
	res, err := client.Action("classify").Execute(ctx, core.NewEnvelope(Input{Tier: "silver"}))
	if err != nil {
		t.Fatalf("Silver tier execution failed: %v", err)
	}
	if _, ok := res.Payload.(Input); !ok {
		t.Fatalf("expected Input payload for non-gold tier, got %T", res.Payload)
	}
}
