// ooda_genkit_flow demonstrates the OODA loop exposed as Genkit flows:
//
//  - OODAFlow: wraps RunOODA with typed I/O (OODAFlowInput/Output)
//
//  - DefineFlow: registers the OODA loop as a Genkit HTTP flow endpoint
//
//  - FlowRegistry: manages multiple named OODA flows
//
// The example uses a mock Brain (no LLM required). With GOOGLE_API_KEY,
// a real Genkit model can be plugged into the Brain.
//
// No API key required (mock Brain + deterministic registry).

package main

import (
	"context"
	"fmt"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/x/ooda"
	oodaflow "github.com/duynguyendang/manglekit/x/oodaflow"
)

// mockBrain implements ooda.Brain with deterministic output.
type mockBrain struct{}

func (b *mockBrain) Evaluate(_ context.Context, frame *ooda.CognitiveFrame) (*core.Decision, error) {
	return &core.Decision{
		Outcome: core.DecisionProceed,
		Action:  core.NewActionEnvelope("generate", nil),
	}, nil
}

func (b *mockBrain) Verify(_ context.Context, frame *ooda.CognitiveFrame) (*core.AuditTrail, error) {
	return &core.AuditTrail{}, nil
}

func (b *mockBrain) LoadPolicy(_ context.Context, rules string) error {
	return nil
}

// mockExecutor runs tools from the registry.
type mockExecutor struct{}

func (e *mockExecutor) Execute(ctx context.Context, frame *ooda.CognitiveFrame, decision *core.Decision) (any, error) {
	if decision.Action != nil {
		return fmt.Sprintf("[flow] executed: %s", decision.Action.Name), nil
	}
	return "[flow] executed: unknown", nil
}

func (e *mockExecutor) Rollback(ctx context.Context, frame *ooda.CognitiveFrame, result any) error {
	return nil
}

func main() {
	ctx := context.Background()

	fmt.Println("=== OODA Genkit Flow Demo ===")
	fmt.Println()

	brain := &mockBrain{}
	executor := &mockExecutor{}

	// =====================================================================
	// Demo 1: OODAFlow.Run() — direct invocation
	// =====================================================================
	fmt.Println("--- Demo 1: OODAFlow.Run() (direct) ---")

	flow := oodaflow.NewOODAFlow(&oodaflow.OODAFlowConfig{
		Brain:      brain,
		Executor:   executor,
		MaxRetries: 3,
	})

	result, err := flow.Run(ctx, &oodaflow.OODAFlowInput{
		Input:  "Generate a compliance report",
		Intent: "document_generation",
	})
	if err != nil {
		fmt.Printf("  Flow error: %v\n", err)
	} else {
		fmt.Printf("  Status: %s\n", result.Status)
		fmt.Printf("  Output: %s\n", result.Output)
		fmt.Printf("  Phases: %v\n", result.PhaseDurations)
	}

	// =====================================================================
	// Demo 2: FlowRegistry — multiple named flows
	// =====================================================================
	fmt.Println()
	fmt.Println("--- Demo 2: FlowRegistry (multiple flows) ---")

	registry := oodaflow.NewFlowRegistry(nil) // nil Genkit = no HTTP registration

	flowReport := oodaflow.NewOODAFlow(&oodaflow.OODAFlowConfig{
		Brain:    brain,
		Executor: executor,
	})
	flowSummary := oodaflow.NewOODAFlow(&oodaflow.OODAFlowConfig{
		Brain:    brain,
		Executor: executor,
	})

	registry.Register("report_generation", flowReport)
	registry.Register("summary_generation", flowSummary)

	fmt.Printf("  Registered flows: report_generation, summary_generation\n")

	result, err = registry.Run(ctx, "report_generation", &oodaflow.OODAFlowInput{
		Input: "Quarterly financial report",
	})
	if err != nil {
		fmt.Printf("  report_generation error: %v\n", err)
	} else {
		fmt.Printf("  report_generation: status=%s output=%s\n", result.Status, result.Output)
	}

	result, err = registry.Run(ctx, "summary_generation", &oodaflow.OODAFlowInput{
		Input: "Executive summary of Q4 results",
	})
	if err != nil {
		fmt.Printf("  summary_generation error: %v\n", err)
	} else {
		fmt.Printf("  summary_generation: status=%s output=%s\n", result.Status, result.Output)
	}

	fmt.Println()
	fmt.Println("Done.")
}
