package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/adapters/ai"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk/ooda"
)

func TestOODAFlow_Run(t *testing.T) {
	brain := &mockBrain{}
	executor := &mockExecutor{}

	flow := ai.NewOODAFlow(&ai.OODAFlowConfig{
		Brain:      brain,
		Executor:   executor,
		MaxRetries: 3,
	})

	result, err := flow.Run(context.Background(), &ai.OODAFlowInput{
		Input:  "test input",
		Intent: "test",
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Status != ooda.VerifyStatusPassed {
		t.Errorf("unexpected status: %s", result.Status)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestFlowRegistry_Run(t *testing.T) {
	brain := &mockBrain{}
	executor := &mockExecutor{}

	registry := ai.NewFlowRegistry(nil)
	flow := ai.NewOODAFlow(&ai.OODAFlowConfig{
		Brain:    brain,
		Executor: executor,
	})
	registry.Register("test_flow", flow)

	result, err := registry.Run(context.Background(), "test_flow", &ai.OODAFlowInput{
		Input: "registry test",
	})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if result.Output == "" {
		t.Error("expected non-empty output")
	}
}

func TestFlowRegistry_NotFound(t *testing.T) {
	registry := ai.NewFlowRegistry(nil)
	_, err := registry.Run(context.Background(), "nonexistent", &ai.OODAFlowInput{
		Input: "test",
	})
	if err == nil {
		t.Error("expected error for nonexistent flow")
	}
}

func TestOODAFlowConfig_Defaults(t *testing.T) {
	flow := ai.NewOODAFlow(nil)
	if flow == nil {
		t.Fatal("expected non-nil flow")
	}
}

func TestMockBrain(t *testing.T) {
	brain := &mockBrain{}
	decision, err := brain.Evaluate(context.Background(), &ooda.CognitiveFrame{})
	if err != nil {
		t.Fatalf("Evaluate error: %v", err)
	}
	if decision.Outcome != core.DecisionProceed {
		t.Errorf("expected proceed, got %s", decision.Outcome)
	}
}
