// routing.go - three routing patterns (merged from route_chaining, 2026-09-12):
//
//  1. Basic ROUTE: EvaluateSteering routes by envelope metadata
//     (data_type → specialized handler via route(Req, Target, Tier) rules).
//
//  2. EAST Steering: SteerKB uses Datalog fast_path/slow_path rules
//     to determine execution path (Fast, Standard, or Slow).
//
//  3. Paradox Injection: When SteeringMagnitude exceeds the threshold,
//     ShouldInjectParadox() signals that the system is in cognitive
//     overload — used to trigger fallback or human escalation.
//
// No API key required (deterministic mock).

package main

import (
	"context"
	"fmt"
	"math"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/x/east"
	"github.com/duynguyendang/manglekit/x/ooda"
)

// mockReasoningPort implements ports.ReasoningPort with Datalog rules
// for EAST steering (fast_path / slow_path).
type mockReasoningPort struct {
	rules map[string]bool
}

func (m *mockReasoningPort) VerifyWithDatalog(_ context.Context, query string) ([]map[string]string, error) {
	if m.rules[query] {
		return []map[string]string{{"Result": "true"}}, nil
	}
	return nil, nil
}

func pathName(p ooda.ExecutionPath) string {
	switch p {
	case ooda.PathFast:
		return "Fast"
	case ooda.PathSlow:
		return "Slow"
	default:
		return "Standard"
	}
}

func demoRoutingSteering(ctx context.Context) error {
	routingPolicy := manglekit.MustReadFile("routing.dl")

	client, err := sdk.NewClient(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Shutdown(ctx) }()

	if err := client.Engine().LoadPolicy(ctx, string(routingPolicy)); err != nil {
		return err
	}

	// =====================================================================
	// Demo 1: Basic ROUTE — EvaluateSteering routes by metadata
	// =====================================================================
	fmt.Println("=== Demo 1: Basic ROUTE via EvaluateSteering ===")
	fmt.Println()
	fmt.Println("Policy routes:")
	fmt.Println("  meta(data_type=text)  → text_handler (T1)")
	fmt.Println("  meta(data_type=image) → image_handler (T1)")
	fmt.Println("  default               → fallback_handler (T3)")
	fmt.Println()

	routeCases := []struct {
		name       string
		dataType   string
		wantTarget string
	}{
		{"Text input", "text", "text_handler"},
		{"Image input", "image", "image_handler"},
		{"Unknown input", "csv", "fallback_handler"},
	}

	allPass := true
	for _, tc := range routeCases {
		env := core.NewEnvelope("process data")
		env.SetMeta("data_type", tc.dataType)
		// toMangleFacts generates value("Req", "process data") from the string
		// payload, which binds Req in the3-arity routing rules.

		decision, meta, err := client.Engine().EvaluateSteering(ctx, env)
		if err != nil {
			fmt.Printf("  %s: EvaluateSteering error: %v\n", tc.name, err)
			allPass = false
			continue
		}

		target := meta[core.KeyNextStep]
		fmt.Printf("  %s: decision=%s target=%s\n", tc.name, decision, target)

		if decision != core.DecisionRoute || target != tc.wantTarget {
			fmt.Printf("    FAIL: want route→%s, got %s→%s\n", tc.wantTarget, decision, target)
			allPass = false
		}
	}

	// =====================================================================
	// Demo 2: EAST Steering — SteerKB with Datalog rules
	// =====================================================================
	fmt.Println()
	fmt.Println("=== Demo 2: EAST Steering via SteerKB ===")
	fmt.Println()
	fmt.Println("Datalog rules queried by SteerKB:")
	fmt.Println("  fast_path(Frame) :- meta(trusted_source, true).")
	fmt.Println("  slow_path(Frame) :- meta(requires_review, true).")
	fmt.Println()

	eastCases := []struct {
		name           string
		trustedSource  bool
		requiresReview bool
		wantPath       ooda.ExecutionPath
	}{
		{"Trusted source", true, false, ooda.PathFast},
		{"Requires review", false, true, ooda.PathSlow},
		{"Neutral input", false, false, ooda.PathStandard},
	}

	for _, tc := range eastCases {
		frame := ooda.NewBuilder().
			WithInput(tc.name).
			Build()

		reasoner := &mockReasoningPort{rules: make(map[string]bool)}
		if tc.trustedSource {
			reasoner.rules[fmt.Sprintf("fast_path(%s)", frame.ID.String())] = true
		}
		if tc.requiresReview {
			reasoner.rules[fmt.Sprintf("slow_path(%s)", frame.ID.String())] = true
		}
		frame.ReasoningPort = reasoner
		frame.EAST.TrustTier = ooda.Tier0Kernel

		path := east.SteerKB(ctx, &frame.EAST, frame, reasoner)
		fmt.Printf("  %s → %s path\n", tc.name, pathName(path))

		if path != tc.wantPath {
			fmt.Printf("    FAIL: want %s, got %s\n", pathName(tc.wantPath), pathName(path))
			allPass = false
		}
	}

	// =====================================================================
	// Demo 3: Paradox Injection — magnitude > threshold
	// =====================================================================
	fmt.Println()
	fmt.Println("=== Demo 3: Paradox Injection ===")
	fmt.Println()
	fmt.Println("SteeringMagnitude = exp(1 - LogicSuccess) / EntropyCoefficient")
	fmt.Println("Paradox triggers when magnitude > threshold (default 0.8)")
	fmt.Println()

	paradoxCases := []struct {
		name            string
		logicSuccess    float64
		entropyCoeff    float64
		wantParadox     bool
		wantTemperature float64
	}{
		// exp(1-0.9)/2.0 = 0.553 → temp=0.7 (0.3 < mag < 0.6)
		{"High success, low entropy → no paradox", 0.9, 2.0, false, 0.7},
		// exp(1-0.1)/1.0 = 2.460 → temp=0.1 (mag > 0.8)
		{"Low success, high entropy → paradox", 0.1, 1.0, true, 0.1},
		// exp(1-0.5)/1.5 = 1.099 → temp=0.1 (mag > 0.8)
		{"Medium entropy → paradox", 0.5, 1.5, true, 0.1},
	}

	for _, tc := range paradoxCases {
		es := &ooda.EASTState{
			LogicSuccess:       tc.logicSuccess,
			EntropyCoefficient: tc.entropyCoeff,
			ParadoxThreshold:   0.8,
		}
		mag := east.CalculateMagnitude(es)
		paradox := east.ShouldInjectParadox(es)
		temp := east.Temperature(es)

		fmt.Printf("  %s:\n", tc.name)
		fmt.Printf("    Magnitude=%.3f, Paradox=%v, Temperature=%.1f\n", mag, paradox, temp)

		if paradox != tc.wantParadox {
			fmt.Printf("    FAIL: want paradox=%v, got %v\n", tc.wantParadox, paradox)
			allPass = false
		}
		if math.Abs(temp-tc.wantTemperature) > 0.01 {
			fmt.Printf("    FAIL: want temperature=%.1f, got %.1f\n", tc.wantTemperature, temp)
			allPass = false
		}
	}

	fmt.Println()
	if !allPass {
		return fmt.Errorf("routing checks failed")
	}
	fmt.Println("All routing checks passed.")
	return nil
}
