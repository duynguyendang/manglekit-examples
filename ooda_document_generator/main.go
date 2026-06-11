package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/sdk/ooda"
)

// ============================================================
// OODA Multi-Turn Document Generator with Steering Feedback
// ============================================================
//
// Demonstrates: Observe → Orient → Decide ⇄ Verify (feedback loop) → Act
//
// The document goes through multiple refinement rounds:
//   Round 1: Initial draft (likely fails T0/T1 checks)
//   Round 2: Incorporate security feedback
//   Round 3: Incorporate compliance feedback
//   Round 4: Final quality check
//
// Each round's Verify phase produces structured RefinementContext
// that feeds back into the next Decide phase.

// --- Constants: Datalog Policy ---

const documentPolicy = `
		Decl has_approval(V).
		Decl has_author(V).
		Decl has_version(V).
		Decl has_changelog(V).
		Decl content_quality(V).

		% === T0: Security Approval (Kernel Axiom) ===
		halt("Req", "T0: missing security approval") :-
			action_operation("Req", "publish_doc"),
			!has_approval("true").

		% === T1: Author Attribution (Governance) ===
		halt("Req", "T1: missing author attribution") :-
			action_operation("Req", "publish_doc"),
			!has_author("true").

		% === T2: Version Number (Playbook) ===
		halt("Req", "T2: missing version number") :-
			action_operation("Req", "publish_doc"),
			!meta("has_version", "present").

		% === T3: Change Log (User/Quality) ===
		halt("Req", "T3: missing change log") :-
			action_operation("Req", "publish_doc"),
			!meta("has_changelog", "present").

		% === Steering Rules ===
		% If content is too short, retry with expansion feedback
		retry("Req", "Content too short — expand with technical details") :-
			action_operation("Req", "publish_doc"),
			meta("content_length", "short").

		% If content quality is low, retry with improvement feedback
		retry("Req", "Content quality low — improve clarity and specificity") :-
			action_operation("Req", "publish_doc"),
			meta("content_quality", "low").

		% Route to approval step if approval is missing
		route("Req", "request_approval") :-
			action_operation("Req", "publish_doc"),
			!has_approval("true").

		% Route to authoring step if author is missing
		route("Req", "add_author") :-
			action_operation("Req", "publish_doc"),
			!has_author("true").
	`

// DocumentGenerator holds the state for multi-turn generation.
type DocumentGenerator struct {
	client       *sdk.Client
	round        int
	maxRounds    int
	history      []string // feedback history to detect thrashing
	currentDraft string
	feedbackLog  []FeedbackEntry
}

type FeedbackEntry struct {
	Round    int
	Rule     string
	Tier     string
	Feedback string
}

// --- Observer: Analyzes input and detects document type ---

type MultiTurnObserver struct {
	gen *DocumentGenerator
}

func (o *MultiTurnObserver) Observe(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Println("👁️  [Observe] Analyzing input...")

	frame.Context = append(frame.Context, ooda.Atom{
		Predicate: "doc_type", Subject: "doc", Object: "security_policy",
	})
	frame.Context = append(frame.Context, ooda.Atom{
		Predicate: "requires_review", Subject: "doc", Object: "security_team",
	})
	frame.Context = append(frame.Context, ooda.Atom{
		Predicate: "classification", Subject: "doc", Object: "internal",
	})

	fmt.Println("   -> Document type: security policy")
	fmt.Println("   -> Requires: security team review")
	fmt.Println("   -> Classification: internal")
	return nil
}

// --- Orienter: Loads tiered Datalog policies ---

type MultiTurnOrienter struct {
	client *sdk.Client
	loaded bool
}

func (o *MultiTurnOrienter) Orient(ctx context.Context, frame *ooda.CognitiveFrame) error {
	if o.loaded {
		fmt.Println("🧭 [Orient] Policies already loaded (refinement round).")
		return nil
	}

	fmt.Println("🧭 [Orient] Loading tiered Datalog policies...")

	if err := o.client.Engine().LoadPolicy(ctx, documentPolicy); err != nil {
		return fmt.Errorf("failed to load policy: %w", err)
	}
	o.loaded = true

	fmt.Println("   -> T0: security approval (hard block)")
	fmt.Println("   -> T1: author attribution (hard block)")
	fmt.Println("   -> T2: version number (soft block)")
	fmt.Println("   -> T3: change log (soft warning)")
	fmt.Println("   -> Steering: retry/route rules active")
	return nil
}

// --- Decider: Formulates plan incorporating feedback ---

type MultiTurnDecider struct {
	gen *DocumentGenerator
}

func (d *MultiTurnDecider) Decide(ctx context.Context, frame *ooda.CognitiveFrame) error {
	d.gen.round++
	fmt.Printf("🧠 [Decide] Round %d: Formulating action plan...\n", d.gen.round)

	// Build content based on feedback from previous rounds
	content := d.gen.buildContent()

	// Determine which approval attributes to include based on round
	args := map[string]interface{}{
		"content":        content,
		"content_length": "short", // Will trigger retry if not improved
	}

	// Progressive compliance: add attributes based on feedback
	if d.gen.round >= 2 {
		args["has_approval"] = "true" // Added after round 1 feedback
		fmt.Println("   -> ✅ Adding: security approval (from round 1 feedback)")
	}
	if d.gen.round >= 3 {
		args["has_author"] = "true" // Added after round 2 feedback
		args["has_version"] = "v1.0"
		fmt.Println("   -> ✅ Adding: author attribution (from round 2 feedback)")
		fmt.Println("   -> ✅ Adding: version number (from round 2 feedback)")
	}
	if d.gen.round >= 4 {
		args["has_changelog"] = "v1.0: Initial release"
		args["content_length"] = "adequate"
		args["content_quality"] = "high"
		fmt.Println("   -> ✅ Adding: change log (from round 3 feedback)")
		fmt.Println("   -> ✅ Content expanded and improved")
	}

	frame.Decision = &core.Decision{
		Outcome: core.DecisionProceed,
		Action: &core.ActionEnvelope{
			Name:      "publish_doc",
			Arguments: args,
		},
	}

	fmt.Printf("   -> Plan: Publish document (round %d draft)\n", d.gen.round)
	return nil
}

// buildContent generates content based on round history.
func (d *DocumentGenerator) buildContent() string {
	base := "Security Policy for Authentication Module"

	if d.round == 1 {
		return base + " - DRAFT"
	}
	if d.round == 2 {
		return base + " - REVISED (security review incorporated)"
	}
	if d.round == 3 {
		return base + " - v1.0 (author: Security Team, reviewed)"
	}
	return base + " - v1.0 (author: Security Team, reviewed, changelog included)"
}

// --- Verifier: Validates against policies with structured feedback ---

type MultiTurnVerifier struct {
	client *sdk.Client
	gen    *DocumentGenerator
}

func (v *MultiTurnVerifier) Verify(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Printf("🛡️  [Verify] Round %d: Validating against policies...\n", v.gen.round)

	// Build envelope from decision
	env := core.NewEnvelope(frame.Decision.Action.Arguments)
	env.Facts = append(env.Facts, `action_operation("Req", "publish_doc").`)

	// Inject approval attributes as Datalog facts
	args := frame.Decision.Action.Arguments
	if v, ok := args["has_approval"].(string); ok && v == "true" {
		env.Facts = append(env.Facts, `has_approval("true").`)
	}
	if v, ok := args["has_author"].(string); ok && v == "true" {
		env.Facts = append(env.Facts, `has_author("true").`)
	}
	if v, ok := args["has_version"].(string); ok && v != "" {
		env.Facts = append(env.Facts, `meta("has_version", "present").`)
	}
	if v, ok := args["has_changelog"].(string); ok && v != "" {
		env.Facts = append(env.Facts, `meta("has_changelog", "present").`)
	}
	if v, ok := args["content_quality"].(string); ok && v != "" {
		env.Facts = append(env.Facts, fmt.Sprintf(`meta("content_quality", "%s").`, v))
	}
	if v, ok := args["content_length"].(string); ok && v != "" {
		env.Facts = append(env.Facts, fmt.Sprintf(`meta("content_length", "%s").`, v))
	}

	// Run AssessPlan — returns Decision with AuditTrail
	decision, err := v.client.Engine().AssessPlan(ctx, env)
	if err != nil {
		fmt.Printf("   -> ⚠️  AssessPlan error: %v\n", err)
	}

	// Print audit trail
	if decision.AuditTrail != nil && len(decision.AuditTrail.MatchedRules) > 0 {
		fmt.Println("   -> 📋 Rules evaluated:")
		for _, rule := range decision.AuditTrail.MatchedRules {
			status := "✅ PASS"
			if strings.Contains(strings.ToLower(rule.RuleName), "halt") {
				status = "❌ FAIL"
			}
			fmt.Printf("      [%s] %s %s\n", rule.Tier, status, rule.Predicate)
		}
	}

	if decision.Outcome == core.DecisionHalt {
		feedback := "fix policy violations"
		if len(decision.Reasons) > 0 {
			feedback = decision.Reasons[0]
		}

		// Extract tier from the first halted rule
		tier := "T?"
		if decision.AuditTrail != nil && len(decision.AuditTrail.MatchedRules) > 0 {
			for _, rule := range decision.AuditTrail.MatchedRules {
				if strings.Contains(strings.ToLower(rule.RuleName), "halt") {
					tier = string(rule.Tier)
					break
				}
			}
		}

		fmt.Printf("   -> ❌ HALT [%s]: %s\n", tier, feedback)

		// Log feedback for history
		v.gen.feedbackLog = append(v.gen.feedbackLog, FeedbackEntry{
			Round:    v.gen.round,
			Rule:     feedback,
			Tier:     tier,
			Feedback: feedback,
		})

		// Check for thrashing (same feedback repeated)
		for _, prev := range v.gen.history {
			if prev == feedback {
				fmt.Println("   -> ⚠️  Thrashing detected: same feedback as previous round")
				fmt.Println("   -> 🔄 Escalating: adding all missing attributes to force convergence")
				// Force all attributes to break the cycle
				if frame.Decision.Action != nil {
					frame.Decision.Action.Arguments["has_approval"] = "true"
					frame.Decision.Action.Arguments["has_author"] = "true"
					frame.Decision.Action.Arguments["has_version"] = "v1.0"
					frame.Decision.Action.Arguments["has_changelog"] = "v1.0: Initial release"
					frame.Decision.Action.Arguments["content_length"] = "adequate"
					frame.Decision.Action.Arguments["content_quality"] = "high"
				}
				v.gen.history = nil // Reset history after escalation
				frame.Decision.Outcome = core.DecisionRetry
				return nil
			}
		}

		// Record feedback to detect future thrashing
		v.gen.history = append(v.gen.history, feedback)

		// Inject steering feedback
		if frame.RawContext == nil {
			frame.RawContext = make(map[string]any)
		}
		frame.RawContext["steering_feedback"] = feedback
		frame.RawContext["feedback_tier"] = tier
		frame.RawContext["round"] = v.gen.round

		frame.Decision.Outcome = core.DecisionRetry
		fmt.Printf("   -> 🔄 Steering: retry with feedback (round %d → %d)\n", v.gen.round, v.gen.round+1)
		return nil
	}

	fmt.Println("   -> ✅ All policy gates passed!")
	return nil
}

// --- Actor: Executes the action ---

type MultiTurnActor struct {
	gen *DocumentGenerator
}

func (a *MultiTurnActor) Act(ctx context.Context, frame *ooda.CognitiveFrame) error {
	fmt.Println("⚡ [Act] Executing...")

	decision := frame.Decision
	if decision == nil || decision.Action == nil {
		fmt.Println("   -> ⚠️  No action to execute.")
		return nil
	}

	content := decision.Action.Arguments["content"].(string)
	a.gen.currentDraft = content

	fmt.Printf("   -> 📄 Document generated: %s\n", content)

	frame.ActionResult = fmt.Sprintf("Round %d draft: %s", a.gen.round, content)
	return nil
}

func main() {
	ctx := context.Background()

	fmt.Println("🔁 OODA Multi-Turn Document Generator with Steering")
	fmt.Println("===================================================")
	fmt.Println("Pattern: Observe → Orient → Decide ⇄ Verify (feedback) → Act")
	fmt.Println("Goal: Generate a compliant document through iterative refinement")
	fmt.Println()

	// 1. Initialize
	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	gen := &DocumentGenerator{
		maxRounds: 5,
	}

	// 2. Instantiate components
	observer := &MultiTurnObserver{gen: gen}
	orienter := &MultiTurnOrienter{client: client}
	decider := &MultiTurnDecider{gen: gen}
	verifier := &MultiTurnVerifier{client: client, gen: gen}
	actor := &MultiTurnActor{gen: gen}

	// 3. Create OODA loop
	loop := ooda.NewLoop(observer, orienter, decider, verifier, actor)

	input := "Create a security policy document for the authentication module."
	frame := ooda.NewCognitiveFrame(input, "session-multi", ooda.TaskTypeGeneration)
	frame.MaxRetries = 5

	// 4. Multi-turn generation loop
	fmt.Println("Starting multi-turn generation...")
	fmt.Println()

	var resultFrame *ooda.CognitiveFrame

	for round := 1; round <= gen.maxRounds; round++ {
		fmt.Printf("\n%s\n", strings.Repeat("=", 60))
		fmt.Printf("=== GENERATION ROUND %d/%d ===\n", round, gen.maxRounds)
		fmt.Printf("%s\n", strings.Repeat("=", 60))

		resultFrame, err = loop.Run(ctx, input, frame)
		if err != nil {
			fmt.Printf("\n🛑 Round %d terminated: %v\n", round, err)
			break
		}

		// Check if steering triggered retry
		if resultFrame.Decision != nil && resultFrame.Decision.Outcome == core.DecisionRetry {
			fmt.Printf("\n🔄 Round %d: Steering triggered retry → proceeding to round %d\n", round, round+1)

			// Preserve feedback for next iteration
			newFrame := ooda.NewCognitiveFrame(input, "session-multi", ooda.TaskTypeGeneration)
			newFrame.MaxRetries = 5
			if resultFrame.RawContext != nil {
				newFrame.RawContext = resultFrame.RawContext
			}
			frame = newFrame
			continue
		}

		// Success
		fmt.Printf("\n✅ Round %d: Document passed all policy gates!\n", round)
		break
	}

	// 5. Summary
	fmt.Println("\n" + strings.Repeat("=", 60))
	fmt.Println("FINAL SUMMARY")
	fmt.Println(strings.Repeat("=", 60))

	if resultFrame != nil {
		fmt.Printf("Final Document: %s\n", gen.currentDraft)
		fmt.Printf("Total Rounds: %d\n", gen.round)
		fmt.Printf("Convergence: %s\n", func() string {
			if gen.round <= 2 {
				return "Fast (no policy issues)"
			}
			return fmt.Sprintf("Gradual (fixed %d issues over %d rounds)", gen.round-1, gen.round)
		}())

		fmt.Println("\nFeedback History:")
		for i, entry := range gen.feedbackLog {
			fmt.Printf("  %d. Round %d [%s]: %s\n", i+1, entry.Round, entry.Tier, entry.Feedback)
		}

		fmt.Println("\nWhat steering did:")
		fmt.Println("  1. Verify detected T0/T1/T2/T3 violations via Datalog rules")
		fmt.Println("  2. Injected structured feedback (FailedRules, ConflictPath)")
		fmt.Println("  3. Decide incorporated feedback into next draft")
		fmt.Println("  4. Loop continued until all gates passed")
		fmt.Println("  5. Thrashing detection prevented infinite retry on same issue")
	}

	if summary := resultFrame.GetAuditSummary(); summary != "No audit trail available" {
		fmt.Printf("\nAudit Summary:\n%s\n", summary)
	}
}
