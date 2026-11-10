package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
	"github.com/duynguyendang/manglekit/adapters/scoring"
)

// TestBenchmarkMockModeRuns runs the FULL two-lane harness in mock mode —
// barrier, concurrent lanes, metrics assembly — so bench.go is covered by
// CI without any GPU. Latency columns are intentionally absent here.
func TestBenchmarkMockModeRuns(t *testing.T) {
	cases := fixtureCases(t)
	out := captureStdout(t, func() {
		if err := runBenchmark(context.Background(), NewMockScorer(cases), false, cases, 60); err != nil {
			t.Errorf("benchmark: %v", err)
		}
	})
	if strings.Contains(out, "p50") {
		t.Fatalf("mock mode must not publish latency numbers:\n%s", out)
	}
	for _, want := range []string{"Two-lane benchmark over 60 cases", "scoring", "generation", "accuracy"} {
		if !strings.Contains(out, want) {
			t.Fatalf("bench output must contain %q, got:\n%s", want, out)
		}
	}
	// Regression guard for the shared-gate race: both lanes used to share one
	// client, so the "did the dispatch run?" counter raced and ~20% of calls
	// errored out silently (Total counted them, no outcome did). Every case
	// must land in exactly one outcome bucket, with zero errors.
	if !strings.Contains(out, "scoring     60     0  ") {
		t.Fatalf("scoring lane must report 60 cases and 0 errors:\n%s", out)
	}
	if strings.Contains(out, "!!") {
		t.Fatalf("bench must not report lane errors:\n%s", out)
	}
}

// TestThresholdDemoMeasuresPolicyEffect runs Truc B end-to-end.
func TestThresholdDemoMeasuresPolicyEffect(t *testing.T) {
	cases := fixtureCases(t)
	out := captureStdout(t, func() {
		if err := runThresholdDemo(context.Background(), NewMockScorer(cases), cases[:30]); err != nil {
			t.Errorf("threshold demo: %v", err)
		}
	})
	for _, want := range []string{"REJECTED", "old policy still active", "policy.mg", "policy_relaxed.mg"} {
		if !strings.Contains(out, want) {
			t.Fatalf("threshold demo output must contain %q, got:\n%s", want, out)
		}
	}
	// Direction, not exact percentages: the mix depends on the per-class bars
	// in policy.mg, and pinning a number here would break every time a bar
	// moves. What must hold is that relaxing the bars auto-routes MORE and
	// escalates no fewer.
	strictAuto := parseAutoPct(t, out, "policy.mg")
	relaxedAuto := parseAutoPct(t, out, "policy_relaxed.mg")
	if relaxedAuto <= strictAuto {
		t.Fatalf("relaxing the auto bar (800 -> 700 per-mille) must increase the auto-routed share, got strict=%.1f%% relaxed=%.1f%%",
			strictAuto, relaxedAuto)
	}
}

// TestCalibrationReportsAndTemperatureHelps pins the calibration code:
// ECE raw must improve (or at least not worsen) after the fitted scalar.
func TestCalibrationReportsAndTemperatureHelps(t *testing.T) {
	cases := fixtureCases(t)
	obs := make([]calObservation, 0, len(cases))
	scorer := NewMockScorer(cases)
	for _, c := range cases {
		opts := optionsFor(c)
		dec, err := scorer.Score(context.Background(), BuildPrompt(c, opts), optionLetters, opts)
		if err != nil {
			t.Fatal(err)
		}
		p := make([]float64, len(opts))
		goldIdx := -1
		for i, o := range opts {
			p[i] = dec.Distribution[o]
			if o == c.Gold {
				goldIdx = i
			}
		}
		obs = append(obs, calObservation{probs: p, goldIdx: goldIdx})
	}
	rawECE := ece(rawEntries(obs))
	if rawECE <= 0 {
		t.Fatalf("fixture set should carry measurable miscalibration, got ECE=%v", rawECE)
	}
	bestT, _ := fitTemperature(obs)
	scaledECE := ece(scaledEntriesAt(obs, bestT))
	if scaledECE > rawECE {
		t.Fatalf("temperature scaling must not worsen ECE: raw=%.4f scaled=%.4f (T=%.2f)", rawECE, scaledECE, bestT)
	}
}

// TestPermutationFlipsAreDetected proves the harness catches order
// sensitivity on the order-EXPOSED lane (mock generator with simulated
// primacy bias) and reports order invariance on the scoring lane.
func TestPermutationFlipsAreDetected(t *testing.T) {
	cases := fixtureCases(t)
	out := captureStdout(t, func() {
		if err := runPermutation(context.Background(), NewMockScorer(cases), false, cases); err != nil {
			t.Errorf("permutation: %v", err)
		}
	})
	if !strings.Contains(out, "scoring lane    flip-rate: 0.0%") {
		t.Fatalf("fixture mock scorer must be order-invariant:\n%s", out)
	}
	if strings.Contains(out, "generation lane flip-rate: 0.0%") {
		t.Fatalf("mock generator carries a simulated primacy bias; the harness must detect flips:\n%s", out)
	}
}

// TestAverageOverPermutationsGatesCleanly exercises the mitigation path:
// the averaged distribution stays normalized and the gate accepts it.
func TestAverageOverPermutationsGatesCleanly(t *testing.T) {
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
	dec, err := AverageOverPermutations(ctx, scorer, cases[0])
	if err != nil {
		t.Fatal(err)
	}
	sum := 0.0
	for _, v := range dec.Distribution {
		sum += v
	}
	if sum < 0.999 || sum > 1.001 {
		t.Fatalf("averaged distribution must stay normalized, got %v", sum)
	}
	if dec.Choice != cases[0].Gold {
		t.Fatalf("confident case must keep its gold choice after averaging: %v", dec.Choice)
	}
	if _, err := gate.Route(ctx, cases[0].ID, dec, th); err != nil {
		t.Fatal(err)
	}
}

// TestLiveSmoke validates the real /v1/score + /v1/chat/completions path
// against a running OpenAI-compatible server. It is env-gated: without the
// backend configured it SKIPS, so `make test` stays green with no GPU and
// no keys (repo contract).
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("MANGLE_SCORER_BACKEND") == "" {
		t.Skip("MANGLE_SCORER_BACKEND not set — live smoke test skipped")
	}
	base := strings.TrimRight(os.Getenv("MANGLE_SCORER_BACKEND"), "/")
	scorer, err := scoring.NewScoreServer(scoring.ScoreConfig{
		BaseURL:     base,
		Model:       os.Getenv("MANGLE_SCORER_MODEL"),
		Tokenizer:   scoring.NewHTTPTokenizer(base, nil),
		SpacePrefix: os.Getenv("MANGLE_SCORER_SPACE") == "1",
	})
	if err != nil {
		t.Fatalf("live scorer: %v", err)
	}
	ctx := context.Background()
	c := datasets.Case{
		ID:     "live-smoke-000",
		Domain: datasets.DomainSupport,
		Text:   "I was charged twice this month, please refund the duplicate. (ref live-smoke-000)",
		Gold:   "billing",
	}
	opts := optionsFor(c)
	dec, err := scorer.Score(ctx, BuildPrompt(c, opts), optionLetters, opts)
	if err != nil {
		t.Fatalf("live decide: %v", err)
	}
	if _, ok := dec.Distribution["billing"]; !ok {
		t.Fatalf("live distribution must cover the business options: %v", dec.Distribution)
	}
	t.Logf("live decision: %s %v (margin %.3f)", dec.Choice, dec.Distribution, dec.Margin)
}

// parseAutoPct reads the "auto" column of the threshold-effect table for the
// named policy row.
func parseAutoPct(t *testing.T, out, policyName string) float64 {
	t.Helper()
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(strings.TrimSpace(line), policyName) {
			continue
		}
		fields := strings.Fields(line)
		// <policy> <auto%> <review%> <escalate%>
		if len(fields) < 4 {
			continue
		}
		var v float64
		if _, err := fmt.Sscanf(strings.TrimSuffix(fields[1], "%"), "%f", &v); err == nil {
			return v
		}
	}
	t.Fatalf("no threshold-effect row for %s in:\n%s", policyName, out)
	return 0
}
