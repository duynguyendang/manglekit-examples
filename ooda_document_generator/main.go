package main

import (
	"context"
	"fmt"
	"log"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/sdk/ooda"
)

// Observer analyzes and normalizes raw input.
type DocumentObserver struct{}

func (o *DocumentObserver) Observe(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Println("👁️  [Observe] Analyzing raw input...")
	frame.Context = append(frame.Context, ooda.Atom{
		Predicate: "input_type",
		Subject:   "doc",
		Object:    "technical_spec",
	})
	frame.Context = append(frame.Context, ooda.Atom{
		Predicate: "requires",
		Subject:   "doc",
		Object:    "security_review",
	})
	fmt.Printf("   -> Identified as technical spec requiring security review.\n")
	return nil
}

// Orienter retrieves domain context and rules.
type DocumentOrienter struct {
	client *sdk.Client
}

func (o *DocumentOrienter) Orient(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Println("🧭 [Orient] Retrieving domain context and policies...")
	// Load a policy that requires security review for technical specs
	policy := `
		requires_security_review("technical_spec").
		halt("Req", "Missing security review") :- action_operation("Req", "generate_doc"), requires_security_review("technical_spec").
	`
	if err := o.client.Engine().LoadPolicy(ctx, policy); err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}
	frame.Context = append(frame.Context, ooda.Atom{
		Predicate: "context_loaded",
		Subject:   "security_policy",
		Object:    "active",
	})
	fmt.Println("   -> Security policy loaded and context hydrated.")
	return nil
}

// Decider formulates a plan.
type DocumentDecider struct{}

func (d *DocumentDecider) Decide(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Println("🧠 [Decide] Formulating action plan...")
	// Propose an action to generate the document, but it lacks security approval initially
	frame.Decision = &core.Decision{
		Outcome: core.DecisionProceed,
		Action: &core.ActionEnvelope{
			Name: "generate_doc",
			Arguments: map[string]interface{}{
				"content": "Draft technical specification",
			},
		},
	}
	fmt.Println("   -> Plan: Generate draft document.")
	return nil
}

// Verifier validates the plan against policies.
type DocumentVerifier struct {
	client *sdk.Client
}

func (v *DocumentVerifier) Verify(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Println("🛡️  [Verify] Validating plan against policies...")

	// Create an envelope to check against the engine
	env := core.NewEnvelope(frame.Decision.Action.Arguments)
	env.Metadata["has_security_approval"] = "false" // Simulate missing approval

	err := v.client.Engine().Assess(ctx, core.ActionMetadata{Name: "generate_doc"}, env)
	if core.IsAlignmentError(err) {
		fmt.Printf("   -> ❌ Policy Violation: %v\n", err)
		fmt.Println("   -> 🔄 Self-Correction: Requesting security approval before proceeding.")

		// Self-correction: Update decision to request approval first
		frame.Decision.Outcome = core.DecisionRoute
		frame.Decision.Target = "request_security_approval"
		return nil // Return nil to allow the loop to continue with the corrected plan
	}

	fmt.Println("   -> ✅ Plan validated successfully.")
	return nil
}

// Actor executes the final approved action.
type DocumentActor struct{}

func (a *DocumentActor) Act(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Println("⚡ [Act] Executing approved action...")

	if frame.Decision.Target == "request_security_approval" {
		fmt.Println("   -> 📧 Security approval request sent. Workflow paused for human-in-the-loop.")
		frame.ActionResult = "Approval request pending"
	} else {
		fmt.Println("   -> 📄 Document generated successfully.")
		frame.ActionResult = "Final technical specification generated"
	}

	return nil
}

func main() {
	ctx := context.Background()

	// 1. Initialize Client (provides the policy engine)
	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	// 2. Instantiate OODA components
	observer := &DocumentObserver{}
	orienter := &DocumentOrienter{client: client}
	decider := &DocumentDecider{}
	verifier := &DocumentVerifier{client: client}
	actor := &DocumentActor{}

	// 3. Create and run the OODA Loop
	loop := ooda.NewLoop(observer, orienter, decider, verifier, actor)

	input := "Create a new technical specification for the authentication module."
	frame := ooda.NewCognitiveFrame(input, "session-123", ooda.TaskTypeGeneration)

	fmt.Println("Starting OODA Loop for Document Generation...")

	resultFrame, err := loop.Run(ctx, input, frame)
	if err != nil {
		log.Fatalf("OODA Loop failed: %v", err)
	}

	fmt.Println("\n✅ OODA Loop completed successfully!")
	fmt.Printf("Final State: %s\n", resultFrame.ActionResult)

	// Print audit summary if available
	if summary := resultFrame.GetAuditSummary(); summary != "No audit trail available" {
		fmt.Printf("\nAudit Summary:\n%s\n", summary)
	}
}
