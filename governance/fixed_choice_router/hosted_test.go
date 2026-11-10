package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
	"github.com/duynguyendang/manglekit/adapters/scoring"
)

// The scoring backends now live in the kernel (manglekit/adapters/scoring,
// ADR-005) and are unit-tested there against httptest stubs. These tests cover
// what is example-specific: that the hosted, key-only path wires into THIS
// gate — score → restricted softmax → policy → outcome — including the
// truncated-distribution escape hatch.

type topLogProb struct {
	Token   string  `json:"token"`
	LogProb float64 `json:"logprob"`
}

type contentLogProb struct {
	Token       string       `json:"token"`
	LogProb     float64      `json:"logprob"`
	TopLogProbs []topLogProb `json:"top_logprobs"`
}

// hostedStub serves /chat/completions with one answer-position logprob set.
func hostedStub(t *testing.T, content contentLogProb) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{
				"message":  map[string]any{"role": "assistant", "content": strings.TrimSpace(content.Token)},
				"logprobs": map[string]any{"content": []contentLogProb{content}},
			}},
		})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func hostedScorer(t *testing.T, url string) Scorer {
	t.Helper()
	s, err := scoring.NewChatScorer(scoring.ChatConfig{
		BaseURL: url, APIKey: "test-key", Model: "stub", TopLogProbs: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// TestHostedScorerDrivesTheGate: a review-band distribution arriving over HTTP
// must be held back by the same T1 rule that holds back the mock's, and the
// dispatch must not have run.
func TestHostedScorerDrivesTheGate(t *testing.T) {
	content := contentLogProb{
		Token:   " B",
		LogProb: -0.30,
		TopLogProbs: []topLogProb{
			{Token: " B", LogProb: -0.30}, // ~0.74 after renorm -> review band
			{Token: " A", LogProb: -1.71},
			{Token: " C", LogProb: -2.53},
			{Token: " The", LogProb: -5.5}, // non-option: must be discarded
		},
	}
	srv := hostedStub(t, content)
	scorer := hostedScorer(t, srv.URL)

	ctx := context.Background()
	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Shutdown(ctx)
	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		t.Fatal(err)
	}

	c := casesByDomain(fixtureCases(t), datasets.DomainSupport)[4]
	opts := optionsFor(c)
	dec, err := scorer.Score(ctx, BuildPrompt(c, opts), optionLetters, opts)
	if err != nil {
		t.Fatal(err)
	}
	if top := dec.Distribution[dec.Choice]; top < 0.70 || top >= 0.80 {
		t.Fatalf("fixture intent broken: expected a review-band top, got %.4f", top)
	}
	if _, ok := dec.Distribution["The"]; ok {
		t.Fatal("non-option tokens must not enter the distribution")
	}

	before := gate.Executed()
	outcome, err := gate.Route(ctx, c.ID, dec, th)
	if err != nil {
		t.Fatal(err)
	}
	if outcome != OutcomeReview {
		t.Fatalf("hosted review-band case: want %s, got %s", OutcomeReview, outcome)
	}
	if gate.Executed() != before {
		t.Fatal("a T1 halt must mean the dispatch never ran")
	}
}

// TestHostedTruncatedDistributionGatesOnTheLowerBound is the honesty property
// end to end, under the bounds contract: a provider that omits an option from
// top_logprobs no longer dead-ends — the scorer returns a sound LOWER bound
// (truncation cannot change the winner, since any missing label has
// p <= p_K <= p_top), and the policy gates on that bound. What must never
// happen is renormalizing over the survivors and calling it confidence.
func TestHostedTruncatedDistributionGatesOnTheLowerBound(t *testing.T) {
	content := contentLogProb{
		Token:   "A",
		LogProb: -0.2,
		TopLogProbs: []topLogProb{
			{Token: "A", LogProb: -0.2},
			{Token: "B", LogProb: -1.8},
			{Token: "maybe", LogProb: -2.5}, // C never appears; p_K = e^-2.5
		},
	}
	srv := hostedStub(t, content)
	scorer := hostedScorer(t, srv.URL)

	ctx := context.Background()
	dec, err := scorer.Score(ctx, "Route this", optionLetters, []string{"billing", "technical_support", "account_access"})
	if err != nil {
		t.Fatalf("truncation must yield bounds, not an error: %v", err)
	}
	if !dec.Truncated {
		t.Fatal("Truncated must be set")
	}
	if len(dec.Missing) != 1 || dec.Missing[0] != "C" {
		t.Fatalf("Missing must name the unobserved label, got %v", dec.Missing)
	}
	if dec.Choice != "billing" {
		t.Fatalf("truncation cannot change the winner, got %q", dec.Choice)
	}
	sum := 0.0
	for _, v := range dec.Distribution {
		sum += v
	}
	if sum >= 1 {
		t.Fatalf("Distribution must hold LOWER bounds (unknown mass exists), got sum %.4f", sum)
	}
	if dec.Distribution["account_access"] != 0 {
		t.Fatalf("the unobserved option must not be given invented mass, got %v", dec.Distribution["account_access"])
	}

	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Shutdown(ctx)
	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		t.Fatal(err)
	}

	before := gate.Executed()
	outcome, err := gate.Route(ctx, "truncated", dec, th)
	if err != nil {
		t.Fatal(err)
	}
	// The arithmetic, spelled out: observed e^-0.2 = 0.8187 (billing) and
	// e^-1.8 = 0.1653 (technical_support), so the unknown mass is at most
	// 1 * p_K = e^-2.5 = 0.0821. Hence
	//     LCB(billing) = 0.8187 / (0.9840 + 0.0821) = 0.76793
	// which TRUNCATES to 767 per-mille (rounding down is deliberate: a
	// borderline score must fail the bar, not sneak over it). Billing's bar is
	// 800, so the bounded score does NOT clear it and the ticket goes to
	// review. That is the point — survivor-normalizing would have reported
	// 0.832 and auto-routed it.
	if pm := scoring.ToPerMille(dec.Distribution["billing"]); pm != 767 {
		t.Fatalf("LCB(billing) = %d per-mille, want 767", pm)
	}
	if outcome != OutcomeReview {
		t.Fatalf("a bounded score below the class bar must go to review, got %s", outcome)
	}
	if gate.Executed() != before {
		t.Fatal("a halted route must not dispatch")
	}

	// And the proof tree must say the evidence was truncated.
	env := scoring.Request(RouteTicketInput{CaseID: "truncated", Choice: dec.Choice}, dec, th)
	facts := append([]string{`action_operation("Req", "route_ticket").`}, env.Facts...)
	expl, err := gate.client.Explain(ctx, `halt("Req", Reason, Tier)`, facts)
	if err != nil {
		t.Fatal(err)
	}
	if !expl.Outcome || !strings.Contains(expl.String(), "truncated top_logprobs") {
		t.Fatalf("truncation must be visible in the audit trail (T2 advisory):\n%s", expl.String())
	}
}

// TestHostedNothingObservedStillEscalates: with NO option among the returned
// candidates there is nothing to bound, so it stays a hard error and the policy
// escalates via unobserved_distribution.
func TestHostedNothingObservedStillEscalates(t *testing.T) {
	content := contentLogProb{
		Token:   "The",
		LogProb: -0.1,
		TopLogProbs: []topLogProb{
			{Token: "The", LogProb: -0.1},
			{Token: "maybe", LogProb: -2.5},
		},
	}
	srv := hostedStub(t, content)
	scorer := hostedScorer(t, srv.URL)

	ctx := context.Background()
	_, err := scorer.Score(ctx, "Route this", optionLetters, []string{"billing", "technical_support", "account_access"})
	if !errors.Is(err, scoring.ErrLabelsUnobserved) {
		t.Fatalf("want ErrLabelsUnobserved, got %v", err)
	}

	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		t.Fatal(err)
	}
	defer gate.Shutdown(ctx)
	before := gate.Executed()
	outcome, err := gate.RouteUnobserved(ctx, "nothing-observed")
	if err != nil {
		t.Fatal(err)
	}
	if outcome != OutcomeEscalate {
		t.Fatalf("want %s, got %s", OutcomeEscalate, outcome)
	}
	if gate.Executed() != before {
		t.Fatal("escalated case must not dispatch")
	}
}

// TestLiveHostedScorer exercises the real key-only path. Env-gated so
// `make test` stays keyless (repo contract).
func TestLiveHostedScorer(t *testing.T) {
	if os.Getenv("MANGLE_SCORER") != "openai" || os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("set MANGLE_SCORER=openai + OPENAI_API_KEY to exercise the hosted scorer")
	}
	scorer, live := newScorer(fixtureCases(t))
	if !live {
		t.Fatal("MANGLE_SCORER=openai must select a live backend")
	}
	ctx := context.Background()
	c := tourPicks(fixtureCases(t))[0]
	opts := optionsFor(c)
	dec, err := scorer.Score(ctx, BuildPrompt(c, opts), optionLetters, opts)
	if errors.Is(err, scoring.ErrLabelsUnobserved) {
		t.Skipf("provider truncated top_logprobs for this prompt (raise MANGLE_LLM_TOP_LOGPROBS): %v", err)
	}
	if err != nil {
		t.Fatalf("live score: %v", err)
	}
	t.Logf("hosted decision: %s %v margin=%.3f", dec.Choice, dec.Distribution, dec.Margin)
}
