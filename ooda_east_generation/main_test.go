package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk/ooda"
)

func TestRunOODA_Success(t *testing.T) {
	ctx := context.Background()
	registry := ooda.NewRegistry()
	registry.MustRegister("test", func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "success", nil
	})
	brain := &mockBrain{decision: &core.Decision{
		Outcome: core.DecisionProceed,
		Action:  core.NewActionEnvelope("test", nil),
	}}
	frame := ooda.NewBuilder().WithInput("test").WithBrain(brain).WithRegistry(registry).Build()
	result, err := ooda.RunOODA(ctx, frame)
	if err != nil {
		t.Fatalf("RunOODA: %v", err)
	}
	if result.Status != ooda.VerifyStatusPassed {
		t.Errorf("expected PASSED, got %s", result.Status)
	}
}

func TestRunOODAEAST_Success(t *testing.T) {
	ctx := context.Background()
	registry := ooda.NewRegistry()
	registry.MustRegister("gen", func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "generated", nil
	})
	brain := &mockBrain{decision: &core.Decision{
		Outcome: core.DecisionProceed,
		Action:  core.NewActionEnvelope("gen", nil),
	}}
	frame := ooda.NewBuilder().WithInput("gen test").WithBrain(brain).WithRegistry(registry).Build()
	result, err := ooda.RunOODAEAST(ctx, frame)
	if err != nil {
		t.Fatalf("RunOODAEAST: %v", err)
	}
	if result.Status != ooda.VerifyStatusPassed {
		t.Errorf("expected PASSED, got %s", result.Status)
	}
	if result.EAST.Entropy < 0 {
		t.Errorf("expected non-negative entropy")
	}
}

func TestRunOODAEAST_Retry(t *testing.T) {
	ctx := context.Background()
	registry := ooda.NewRegistry()
	registry.MustRegister("gen", func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "generated", nil
	})
	brain := &failingBrain{
		mockBrain: mockBrain{decision: &core.Decision{
			Outcome: core.DecisionProceed,
			Action:  core.NewActionEnvelope("gen", nil),
		}},
		failNext: 1,
	}
	frame := ooda.NewBuilder().WithInput("retry test").WithBrain(brain).WithRegistry(registry).WithMaxRetries(3).Build()
	result, err := ooda.RunOODAEAST(ctx, frame)
	if err != nil {
		t.Fatalf("Failed after retry: %v", err)
	}
	if result.Status != ooda.VerifyStatusPassed {
		t.Errorf("expected PASSED after retry, got %s", result.Status)
	}
}

func TestPinAxiom(t *testing.T) {
	frame := ooda.NewBuilder().WithInput("pin test").Build()
	ooda.PinAxiom(frame, ooda.Atom{Predicate: "axiom", Subject: "t0", Object: "rule", Weight: 1.0})
	if len(ooda.GetAttentionSink(frame)) != 1 {
		t.Errorf("expected 1 axiom, got %d", len(ooda.GetAttentionSink(frame)))
	}
	if ooda.GetAttentionSink(frame)[0].Weight != 1.0 {
		t.Errorf("expected weight 1.0, got %f", ooda.GetAttentionSink(frame)[0].Weight)
	}
}

func TestShaveContext(t *testing.T) {
	frame := ooda.NewBuilder().WithInput("shave test").Build()
	for i := 0; i < 20; i++ {
		w := float64(9-(i%10)) / 10.0
		ooda.AddContext(frame, ooda.Atom{Predicate: "f", Subject: "s", Object: "o", Weight: w})
	}
	before := len(ooda.GetContext(frame))
	ooda.ShaveContext(frame, 5)
	after := len(ooda.GetContext(frame))
	if after >= before {
		t.Errorf("expected shaving to reduce: before=%d, after=%d", before, after)
	}
}

func TestMixedPrecisionMemory(t *testing.T) {
	frame := ooda.NewBuilder().WithInput("mixed test").Build()
	ooda.PinAxiom(frame, ooda.Atom{Predicate: "axiom", Subject: "t0", Object: "a1", Weight: 1.0})
	ooda.PinAxiom(frame, ooda.Atom{Predicate: "axiom", Subject: "t0", Object: "a2", Weight: 1.0})
	ooda.AddContext(frame, ooda.Atom{Predicate: "ctx", Subject: "s", Object: "high", Weight: 0.9})
	ooda.AddContext(frame, ooda.Atom{Predicate: "ctx", Subject: "s", Object: "low", Weight: 0.3})
	if len(ooda.GetAttentionSink(frame)) != 2 {
		t.Errorf("expected 2 sink atoms")
	}
	if len(ooda.GetContext(frame)) != 2 {
		t.Errorf("expected 2 context atoms")
	}
	// Shave with a tight token budget — the 0.3-weight atom gets dropped
	ooda.ShaveContext(frame, 1)
	if len(ooda.GetContext(frame)) != 0 {
		t.Errorf("expected 0 context atoms after tight shave, got %d", len(ooda.GetContext(frame)))
	}
}

func TestRunOODA_HaltDecision(t *testing.T) {
	ctx := context.Background()
	brain := &mockBrain{decision: &core.Decision{
		Outcome: core.DecisionHalt,
		Action:  core.NewActionEnvelope("none", nil),
	}}
	frame := ooda.NewBuilder().WithInput("halt").WithBrain(brain).Build()
	_, err := ooda.RunOODA(ctx, frame)
	if err == nil {
		t.Error("expected error for HALT")
	}
}

func TestRunOODAEAST_HaltDecision(t *testing.T) {
	ctx := context.Background()
	brain := &mockBrain{decision: &core.Decision{
		Outcome: core.DecisionHalt,
		Action:  core.NewActionEnvelope("blocked", nil),
	}}
	frame := ooda.NewBuilder().WithInput("halt").WithBrain(brain).Build()
	_, err := ooda.RunOODAEAST(ctx, frame)
	if err == nil {
		t.Error("expected error for HALT")
	}
}

func TestEstimateTotalTokens(t *testing.T) {
	frame := ooda.NewBuilder().WithInput("tokens").Build()
	ooda.PinAxiom(frame, ooda.Atom{Predicate: "a", Subject: "s", Object: "o", Weight: 1.0})
	ooda.AddContext(frame, ooda.Atom{Predicate: "b", Subject: "s2", Object: "o2", Weight: 0.5})
	tokens := ooda.EstimateTotalTokens(frame)
	if tokens <= 0 {
		t.Errorf("expected positive tokens, got %d", tokens)
	}
}
