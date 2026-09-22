package main

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// helper: build mock scorer over the committed fixtures.
func fixtureCases(t *testing.T) []datasets.Case {
	t.Helper()
	cases, err := loadFixtures()
	if err != nil {
		t.Fatalf("load fixtures: %v", err)
	}
	if len(cases) != 300 {
		t.Fatalf("expected 300 fixture cases, got %d", len(cases))
	}
	return cases
}

func mustScore(t *testing.T, s Scorer, c datasets.Case) *Decision {
	t.Helper()
	opts := optionsFor(c)
	dec, err := s.Decide(context.Background(), BuildPrompt(c, opts), optionLetters, opts)
	if err != nil {
		t.Fatalf("decide %s: %v", c.ID, err)
	}
	return dec
}

// TestFixtureGolden pins the committed fixtures.jsonl against silent drift:
// regenerate from the deterministic rules and compare. If it fails, rerun
// `go run . --mode=gen-fixtures` and review the diff.
func TestFixtureGolden(t *testing.T) {
	committed := fixtureCases(t)
	regen := datasets.Generate()
	if len(regen) != len(committed) {
		t.Fatalf("generator produced %d cases, committed fixtures carry %d", len(regen), len(committed))
	}
	for i := range regen {
		if !reflect.DeepEqual(regen[i], committed[i]) {
			a, _ := json.Marshal(regen[i])
			b, _ := json.Marshal(committed[i])
			t.Fatalf("fixture drift at %d:\n generated: %s\n committed: %s", i, a, b)
		}
	}
}

// TestMockDecisionIsBusinessNormalized checks the restricted-softmax
// contract: probabilities sum to 1 over exactly the options, and the choice
// speaks business names, not letters.
func TestMockDecisionIsBusinessNormalized(t *testing.T) {
	cases := fixtureCases(t)
	scorer := NewMockScorer(cases)
	c := cases[0] // confident profile
	dec := mustScore(t, scorer, c)

	sum := 0.0
	for _, v := range dec.Distribution {
		sum += v
	}
	if math.Abs(sum-1) > 1e-9 {
		t.Fatalf("distribution must sum to 1, got %v", sum)
	}
	if dec.Choice != c.Gold {
		t.Fatalf("confident case must score to gold %q, got %q", c.Gold, dec.Choice)
	}
	if len(dec.Distribution) != 3 {
		t.Fatalf("distribution must cover exactly the 3 options, got %d", len(dec.Distribution))
	}
	if dec.Choice == "A" || dec.Choice == "B" || dec.Choice == "C" {
		t.Fatal("choice must be a business option, not an option letter")
	}
}

// TestSingleTokenValidationRejectsMultiCharLabels covers the blog's " A" vs
// "A" trap: a label that is not a single token cannot be scored.
func TestSingleTokenValidationRejectsMultiCharLabels(t *testing.T) {
	ctx := context.Background()
	if err := validateSingleTokens(ctx, MockTokenizer{}, []string{"A", "B", "C"}); err != nil {
		t.Fatalf("single letters must pass validation: %v", err)
	}
	if err := validateSingleTokens(ctx, MockTokenizer{}, []string{"maybe"}); err == nil {
		t.Fatal("multi-character label must fail single-token validation")
	}
}

// TestGateOutcomes pins every decision shape end-to-end through the real
// Zero-Trust gate: the inner dispatch must run ONLY on auto-route.
func TestGateOutcomes(t *testing.T) {
	ctx := context.Background()
	cases := fixtureCases(t)
	scorer := NewMockScorer(cases)

	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Shutdown(ctx)
	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		t.Fatal(err)
	}
	if th.Auto != 0.80 || th.Margin != 0.20 || th.Review != 0.70 {
		t.Fatalf("strict thresholds: want 0.80/0.20/0.70, got %v", th)
	}

	before := gate.Executed()
	for _, tc := range []struct {
		idx  int
		want string
	}{
		{0, OutcomeAuto},     // P=0.93
		{9, OutcomeAuto},     // P=0.83
		{4, OutcomeReview},   // P=0.74
		{6, OutcomeEscalate}, // P=0.55 < review floor
		{8, OutcomeEscalate}, // P=0.44
	} {
		c := casesByDomain(cases, datasets.DomainSupport)[tc.idx]
		dec := mustScore(t, scorer, c)
		got, err := gate.Route(ctx, c.ID, dec, th)
		if err != nil {
			t.Fatalf("route %s: %v", c.ID, err)
		}
		if got != tc.want {
			t.Fatalf("case %s (P=%.3f): want %s, got %s", c.ID, dec.Distribution[dec.Choice], tc.want, got)
		}
	}
	// Only the 2 auto-route cases may have executed the inner dispatch.
	if ran := gate.Executed() - before; ran != 2 {
		t.Fatalf("inner dispatch must run exactly for the 2 allowed cases, ran=%d", ran)
	}

	// The escape hatch: no parsable choice -> the policy escalates it.
	got, err := gate.RouteNoChoice(ctx, "genlane")
	if err != nil {
		t.Fatal(err)
	}
	if got != OutcomeEscalate {
		t.Fatalf("no-choice must escalate, got %s", got)
	}
}

func casesByDomain(all []datasets.Case, domain string) []datasets.Case {
	var out []datasets.Case
	for _, c := range all {
		if c.Domain == domain {
			out = append(out, c)
		}
	}
	return out
}

// TestExplainDenyCarriesProof asserts the derivation tree names the firing
// rule AND grounds the numbers the rule matched (P vs T) — the audit trail
// the blog's Python if-chain can never produce.
func TestExplainDenyCarriesProof(t *testing.T) {
	ctx := context.Background()
	cases := fixtureCases(t)
	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Shutdown(ctx)

	reviewCase := casesByDomain(cases, datasets.DomainSupport)[4] // P=0.740
	expl, err := gate.client.Explain(ctx, `halt("Req", Reason, Tier)`, []string{
		`action_operation("Req", "route_ticket").`,
		`meta("below_auto", "true").`,
		`confidence("0.740").`,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !expl.Outcome {
		t.Fatal("Explain must derive the halt")
	}
	tree := expl.String()
	for _, want := range []string{"low_confidence_send_to_review", "0.740", "0.80"} {
		if !strings.Contains(tree, want) {
			t.Fatalf("proof tree must ground %q, tree:\n%s", want, tree)
		}
	}
	_ = reviewCase
}

// TestReloadFlipsDecisionAndFailsSafe is the superset story in one test:
// the same scored case re-decides after ReloadPolicy, and a broken program
// is rejected with the old policy still serving.
func TestReloadFlipsDecisionAndFailsSafe(t *testing.T) {
	ctx := context.Background()
	cases := fixtureCases(t)
	scorer := NewMockScorer(cases)

	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Shutdown(ctx)

	// P=0.710: review band under strict, auto-route under relaxed.
	c := casesByDomain(cases, datasets.DomainSupport)[5]
	dec := mustScore(t, scorer, c)
	top := dec.Distribution[dec.Choice]
	if top >= 0.70 && top >= 0.80 {
		t.Fatalf("fixture assumption broken: P=%.3f", top)
	}

	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := gate.Route(ctx, c.ID, dec, th); err != nil || got != OutcomeReview {
		t.Fatalf("strict policy: want review, got %s (%v)", got, err)
	}

	if err := gate.client.ReloadPolicy(ctx, relaxedPolicyPath()); err != nil {
		t.Fatalf("reload relaxed: %v", err)
	}
	th2, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		t.Fatal(err)
	}
	if th2.Auto != 0.70 || th2.Review != 0.60 {
		t.Fatalf("after reload thresholds must be the relaxed ones, got %v", th2)
	}
	if got, err := gate.Route(ctx, c.ID, dec, th2); err != nil || got != OutcomeAuto {
		t.Fatalf("relaxed policy: want auto_route, got %s (%v)", got, err)
	}

	// Fail-safe: broken program rejected; the relaxed policy keeps serving.
	if err := gate.client.ReloadPolicy(ctx, brokenPolicyPath()); err == nil {
		t.Fatal("broken policy must be rejected")
	}
	th3, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		t.Fatal(err)
	}
	if th3.Auto != 0.70 {
		t.Fatalf("after failed reload the active policy must be unchanged, got %v", th3)
	}
}

// TestTourRuns exercises the default path end-to-end (mock mode).
func TestTourRuns(t *testing.T) {
	os.Setenv("MANGLE_SCORER_BACKEND", "")
	tourOutput := captureStdout(t, func() {
		if err := runTour(context.Background(), NewMockScorer(fixtureCases(t)), false, fixtureCases(t)); err != nil {
			t.Fatalf("tour: %v", err)
		}
	})
	for _, want := range []string{
		"-> auto_route",
		"-> send_to_review",
		"-> escalate_human",
		"low_confidence_send_to_review",
		"threshold(\"auto\",\"0.80\")",
		"policy.dl restored",
	} {
		if !strings.Contains(tourOutput, want) {
			t.Fatalf("tour output must contain %q, got:\n%s", want, tourOutput)
		}
	}
}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	done := make(chan string, 1)
	go func() {
		var sb strings.Builder
		buf := make([]byte, 4096)
		for {
			n, err := r.Read(buf)
			if n > 0 {
				sb.Write(buf[:n])
			}
			if err != nil {
				break
			}
		}
		done <- sb.String()
	}()
	defer func() { os.Stdout = old }()
	fn()
	w.Close()
	return <-done
}
