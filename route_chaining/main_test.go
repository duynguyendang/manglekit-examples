package main

import (
	"context"
	"os"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/sdk/ooda"
)

func loadRoutingPolicy(t *testing.T, ctx context.Context, client *sdk.Client) {
	t.Helper()
	policyData, err := os.ReadFile("routing.dl")
	if err != nil {
		t.Fatal(err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyData)); err != nil {
		t.Fatal(err)
	}
}

func TestEvaluateSteering_TextRoutes(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadRoutingPolicy(t, ctx, client)

	env := core.NewEnvelope("process data")
	env.SetMeta("data_type", "text")

	decision, meta, err := client.Engine().EvaluateSteering(ctx, env)
	if err != nil {
		t.Fatalf("EvaluateSteering error: %v", err)
	}
	if decision != core.DecisionRoute {
		t.Errorf("expected route decision, got %s", decision)
	}
	if meta[core.KeyNextStep] != "text_handler" {
		t.Errorf("expected text_handler, got %s", meta[core.KeyNextStep])
	}
}

func TestEvaluateSteering_ImageRoutes(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadRoutingPolicy(t, ctx, client)

	env := core.NewEnvelope("process data")
	env.SetMeta("data_type", "image")

	decision, meta, err := client.Engine().EvaluateSteering(ctx, env)
	if err != nil {
		t.Fatalf("EvaluateSteering error: %v", err)
	}
	if decision != core.DecisionRoute {
		t.Errorf("expected route decision, got %s", decision)
	}
	if meta[core.KeyNextStep] != "image_handler" {
		t.Errorf("expected image_handler, got %s", meta[core.KeyNextStep])
	}
}

func TestEvaluateSteering_DefaultRoutes(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
	loadRoutingPolicy(t, ctx, client)

	env := core.NewEnvelope("process data")

	decision, meta, err := client.Engine().EvaluateSteering(ctx, env)
	if err != nil {
		t.Fatalf("EvaluateSteering error: %v", err)
	}
	if decision != core.DecisionRoute {
		t.Errorf("expected route decision, got %s", decision)
	}
	if meta[core.KeyNextStep] != "fallback_handler" {
		t.Errorf("expected fallback_handler, got %s", meta[core.KeyNextStep])
	}
}

func TestSteerKB_FastPath(t *testing.T) {
	frame := ooda.NewBuilder().WithInput("trusted input").Build()
	reasoner := &mockReasoningPort{
		rules: map[string]bool{"fast_path(" + frame.ID.String() + ")": true},
	}
	frame.ReasoningPort = reasoner
	frame.EAST.TrustTier = ooda.Tier0Kernel

	path := frame.EAST.SteerKB(context.Background(), frame, reasoner)
	if path != ooda.PathFast {
		t.Errorf("expected fast path, got %s", pathName(path))
	}
}

func TestSteerKB_SlowPath(t *testing.T) {
	frame := ooda.NewBuilder().WithInput("review input").Build()
	reasoner := &mockReasoningPort{
		rules: map[string]bool{"slow_path(" + frame.ID.String() + ")": true},
	}
	frame.ReasoningPort = reasoner
	frame.EAST.TrustTier = ooda.Tier0Kernel

	path := frame.EAST.SteerKB(context.Background(), frame, reasoner)
	if path != ooda.PathSlow {
		t.Errorf("expected slow path, got %s", pathName(path))
	}
}

func TestSteerKB_StandardPath(t *testing.T) {
	frame := ooda.NewBuilder().WithInput("neutral input").Build()
	reasoner := &mockReasoningPort{rules: make(map[string]bool)}
	frame.ReasoningPort = reasoner
	frame.EAST.TrustTier = ooda.Tier0Kernel

	path := frame.EAST.SteerKB(context.Background(), frame, reasoner)
	if path != ooda.PathStandard {
		t.Errorf("expected standard path, got %s", pathName(path))
	}
}

func TestParadoxInjection(t *testing.T) {
	east := &ooda.EASTState{
		LogicSuccess:      0.1,
		EntropyCoefficient: 1.0,
		ParadoxThreshold:  0.8,
	}
	mag := east.CalculateMagnitude()
	if !east.ShouldInjectParadox() {
		t.Errorf("expected paradox injection at magnitude %.3f > 0.8", mag)
	}
}

func TestNoParadoxWhenLow(t *testing.T) {
	east := &ooda.EASTState{
		LogicSuccess:      0.9,
		EntropyCoefficient: 2.0,
		ParadoxThreshold:  0.8,
	}
	mag := east.CalculateMagnitude()
	if east.ShouldInjectParadox() {
		t.Errorf("unexpected paradox injection at magnitude %.3f", mag)
	}
}
