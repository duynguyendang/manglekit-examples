// ooda_east_generation demonstrates the OODA v4 EAST engine
// (`manglekit/sdk/ooda/`):
//   - RunOODA: deterministic tool flow (no EAST)
//   - RunOODAEAST: generation flow with EAST steering
//   - PinAxiom / AddContext / ShaveContext: mixed-precision memory
//   - EAST state: entropy, saliency, trust tier, steering magnitude
//   - Teacher-Student retry loop in eastPostAct
//   - Sentinel errors: ErrT0Violation, ErrMaxRefinementIterations
//
// No API key required (deterministic mock generator + Brain).

package main

import (
	"context"
	"fmt"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk/ooda"
)

// =========================================================================
// Mock Brain implementations
// =========================================================================

// mockBrain implements ooda.Brain with a controllable decision.
type mockBrain struct {
	decision  *core.Decision
	auditPath string
}

func (b *mockBrain) Evaluate(_ context.Context, frame *ooda.CognitiveFrame) (*core.Decision, error) {
	return b.decision, nil
}

func (b *mockBrain) Verify(_ context.Context, frame *ooda.CognitiveFrame) (*core.AuditTrail, error) {
	return &core.AuditTrail{}, nil
}

func (b *mockBrain) LoadPolicy(_ context.Context, rules string) error {
	return nil
}

// failingBrain simulates a Brain whose Verify returns violations.
type failingBrain struct {
	mockBrain
	failNext int
}

func (b *failingBrain) Verify(_ context.Context, frame *ooda.CognitiveFrame) (*core.AuditTrail, error) {
	b.failNext--
	if b.failNext >= 0 {
		return &core.AuditTrail{
			MatchedRules: []core.RuleInference{
				{RuleName: "quality_check", Tier: "T2", Predicate: "halt"},
			},
		}, nil
	}
	return &core.AuditTrail{}, nil
}

// =========================================================================
// Main
// =========================================================================

func main() {
	ctx := context.Background()

	fmt.Println("OODA EAST Generation Engine")
	fmt.Println("============================")

	// -----------------------------------------------------------------------
	// Demo 1: RunOODA — deterministic tool flow
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Demo 1: RunOODA (deterministic tool flow) ---")

	registry1 := ooda.NewRegistry()
	registry1.MustRegister("generate_doc", func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "[mock] generated document: architecture overview", nil
	})

	brain1 := &mockBrain{decision: &core.Decision{
		Outcome: core.DecisionProceed,
		Action:  core.NewActionEnvelope("generate_doc", nil),
	}}

	frame1 := ooda.NewBuilder().
		WithInput("Generate architecture overview document").
		WithBrain(brain1).
		WithRegistry(registry1).
		Build()

	// Pin an axiom before running
	ooda.PinAxiom(frame1, ooda.Atom{Predicate: "axiom", Subject: "safety", Object: "no_external_share", Weight: 1.0})

	// Add some context
	existing := []ooda.Atom{
		{Predicate: "project_type", Subject: "system", Object: "microservices", Weight: 0.7},
		{Predicate: "team", Subject: "system", Object: "platform", Weight: 0.5},
	}
	for _, a := range existing {
		ooda.AddContext(frame1, a)
	}

	fmt.Printf("  AttentionSink (FP32): %d axioms pinned\n", len(ooda.GetAttentionSink(frame1)))
	fmt.Printf("  Context (INT8) before shave: %d atoms\n", len(ooda.GetContext(frame1)))

	// Shave context to show pruning
	ooda.ShaveContext(frame1, 20)
	fmt.Printf("  Context (INT8) after shave:  %d atoms\n", len(ooda.GetContext(frame1)))

	result1, err := ooda.RunOODA(ctx, frame1)
	if err != nil {
		fmt.Printf("  RunOODA error: %v\n", err)
	} else {
		fmt.Printf("  Status: %s\n", result1.Status)
		fmt.Printf("  Result: %v\n", result1.ActionResult)
		fmt.Printf("  Phases: %v\n", result1.PhaseDurations)
	}

	// -----------------------------------------------------------------------
	// Demo 2: RunOODAEAST — generation with EAST steering
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Demo 2: RunOODAEAST (generation + EAST steering) ---")

	registry2 := ooda.NewRegistry()
	registry2.MustRegister("generate", func(ctx context.Context, args map[string]interface{}) (string, error) {
		return "[mock] generated: compliance report v1", nil
	})

	brain2 := &mockBrain{decision: &core.Decision{
		Outcome: core.DecisionProceed,
		Action:  core.NewActionEnvelope("generate", nil),
	}}

	frame2 := ooda.NewBuilder().
		WithInput("Generate compliance report for payment system").
		WithBrain(brain2).
		WithRegistry(registry2).
		Build()

	result2, err := ooda.RunOODAEAST(ctx, frame2)
	if err != nil {
		fmt.Printf("  RunOODAEAST error: %v\n", err)
	} else {
		fmt.Printf("  Status: %s\n", result2.Status)
		fmt.Printf("  Result: %v\n", result2.ActionResult)
		fmt.Printf("  EAST Entropy: %.2f\n", result2.EAST.Entropy)
		fmt.Printf("  EAST Saliency: %v\n", result2.EAST.Saliency)
		fmt.Printf("  EAST TrustTier: %s\n", result2.EAST.TrustTier)
		fmt.Printf("  EAST Magnitude: %.2f\n", result2.EAST.SteeringMagnitude)
	}

	// -----------------------------------------------------------------------
	// Demo 3: EAST Teacher-Student retry loop (Verify returns violations, then passes)
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Demo 3: EAST Teacher-Student Retry Loop ---")

	registry3 := ooda.NewRegistry()
	retryAttempt := 0
	registry3.MustRegister("generate", func(ctx context.Context, args map[string]interface{}) (string, error) {
		retryAttempt++
		return fmt.Sprintf("[mock attempt %d] generated document", retryAttempt), nil
	})

	brain3 := &failingBrain{
		mockBrain: mockBrain{decision: &core.Decision{
			Outcome: core.DecisionProceed,
			Action:  core.NewActionEnvelope("generate", nil),
		}},
		failNext: 1, // fail once, then pass
	}

	frame3 := ooda.NewBuilder().
		WithInput("Generate a document that may need refinement").
		WithBrain(brain3).
		WithRegistry(registry3).
		WithMaxRetries(3).
		Build()

	result3, err := ooda.RunOODAEAST(ctx, frame3)
	if err != nil {
		fmt.Printf("  RunOODAEAST error: %v\n", err)
	} else {
		fmt.Printf("  Status: %s (after %d refinement iterations)\n", result3.Status, result3.RetryCount)
		fmt.Printf("  Final Result: %v\n", result3.ActionResult)
		fmt.Printf("  EAST Entropy: %.2f\n", result3.EAST.Entropy)
	}

	// -----------------------------------------------------------------------
	// Demo 4: Memory operations — PinAxiom, AddContext, ShaveContext, PruneColdAtoms
	// -----------------------------------------------------------------------
	fmt.Println("\n--- Demo 4: Mixed-Precision Memory (INT8 vs FP32) ---")

	frame4 := ooda.NewBuilder().
		WithInput("memory demo").
		Build()

	// Pin axioms (FP32, never pruned)
	safetyAxioms := []ooda.Atom{
		{Predicate: "axiom", Subject: "t0", Object: "never_delete"},
		{Predicate: "axiom", Subject: "t0", Object: "require_approval"},
	}
	for _, a := range safetyAxioms {
		ooda.PinAxiom(frame4, a)
	}

	// Add context atoms with varying weights (INT8, prunable)
	for i := 0; i < 20; i++ {
		weight := float64(9-(i%10)) / 10.0 // weights: 0.9, 0.8, ..., 0.0, 0.9, ...
		ooda.AddContext(frame4, ooda.Atom{
			Predicate: fmt.Sprintf("fact_%d", i),
			Subject:   "ctx",
			Object:    fmt.Sprintf("value_%d", i),
			Weight:    weight,
		})
	}

	fmt.Printf("  Before shave:\n")
	fmt.Printf("    AttentionSink (FP32): %d axioms\n", len(ooda.GetAttentionSink(frame4)))
	fmt.Printf("    Context (INT8):       %d atoms\n", len(ooda.GetContext(frame4)))
	fmt.Printf("    Estimated tokens:     %d\n", ooda.EstimateTotalTokens(frame4))

	// Shave context to a small budget — low-weight atoms dropped
	ooda.ShaveContext(frame4, 10)

	fmt.Printf("  After shave (maxTokens=10):\n")
	fmt.Printf("    Context (INT8):       %d atoms (high-weight only)\n", len(ooda.GetContext(frame4)))
	for _, a := range ooda.GetContext(frame4) {
		fmt.Printf("      - %s.%s(%s) w=%.1f\n", a.Predicate, a.Subject, a.Object, a.Weight)
	}

	fmt.Println("\nDone.")
}
