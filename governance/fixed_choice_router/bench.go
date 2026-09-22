package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// LaneResult accumulates one lane's benchmark statistics.
type LaneResult struct {
	Name     string
	Total    int
	Auto     int
	Review   int
	Escalate int
	Wrong    int // choice != gold (regardless of gate outcome)
	Latency  []time.Duration
	Tokens   int64 // completion tokens (generation lane only)
}

func (r *LaneResult) accuracy() float64 {
	if r.Total == 0 {
		return 0
	}
	return 1 - float64(r.Wrong)/float64(r.Total)
}

func (r *LaneResult) escalateRate() float64 {
	if r.Total == 0 {
		return 0
	}
	return float64(r.Escalate+r.Review) / float64(r.Total)
}

func percentile(ds []time.Duration, q float64) time.Duration {
	if len(ds) == 0 {
		return 0
	}
	s := make([]time.Duration, len(ds))
	copy(s, ds)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	idx := int(q*float64(len(s)-1) + 0.5)
	return s[idx]
}

// runBenchmark is Trục A — the parity heart of the blog: two lanes behind a
// start barrier, scoring vs generation, same policy, same fixtures, same
// (live) server.
//
// Mock mode deliberately suppresses the latency/throughput columns: mock
// timings measure nothing and publishing them would be the exact
// credibility trap the blog's commenters set off. Run with the live env to
// get them.
func runBenchmark(ctx context.Context, scorer Scorer, live bool, cases []datasets.Case, limit int) error {
	if limit <= 0 || limit > len(cases) {
		limit = len(cases)
	}
	set := cases[:limit]

	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		return err
	}
	defer gate.Shutdown(ctx)
	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		return err
	}

	gen, _ := newGenerator(live, set)

	start := make(chan struct{})
	var wg sync.WaitGroup
	var laneS, laneG LaneResult
	laneS.Name, laneG.Name = "scoring", "generation"
	var mu sync.Mutex

	runLane := func(res *LaneResult, work func(ctx context.Context, c datasets.Case) (choice string, outcome string, tokens int64, err error)) {
		defer wg.Done()
		<-start
		for _, c := range set {
			t0 := time.Now()
			choice, outcome, tokens, err := work(ctx, c)
			if err != nil {
				mu.Lock()
				res.Total++ // counts as a failed case, still reported
				mu.Unlock()
				continue
			}
			d := time.Since(t0)
			mu.Lock()
			res.Total++
			switch outcome {
			case OutcomeAuto:
				res.Auto++
			case OutcomeReview:
				res.Review++
			case OutcomeEscalate:
				res.Escalate++
			}
			if choice != c.Gold {
				res.Wrong++
			}
			if live {
				res.Latency = append(res.Latency, d)
				res.Tokens += tokens
			}
			mu.Unlock()
		}
	}

	wg.Add(2)
	go runLane(&laneS, scoreLaneWork(scorer, gate, th))
	go runLane(&laneG, generateLaneWork(gen, gate))
	close(start) // barrier: both lanes fire together, blog-style
	wg.Wait()

	fmt.Printf("\n=== Two-lane benchmark over %d cases (%s backend) ===\n", len(set), modeWord(live))
	fmt.Printf("policy: auto>=%.2f margin>=%.2f review>=%.2f\n", th.Auto, th.Margin, th.Review)
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	if live {
		fmt.Fprintln(w, "lane\ttotal\taccuracy\tescalate+review\tp50\tp95\tthroughput(s/s)\ttokens/case")
		for _, r := range []LaneResult{laneS, laneG} {
			window := totalWindow(r.Latency)
			tps := 0.0
			if window > 0 {
				tps = float64(r.Total) / window
			}
			tokensPerCase := 0.0
			if r.Total > 0 {
				tokensPerCase = float64(r.Tokens) / float64(r.Total)
			}
			fmt.Fprintf(w, "%s\t%d\t%.1f%%\t%.1f%%\t%s\t%s\t%.1f\t%.1f\n",
				r.Name, r.Total, r.accuracy()*100, r.escalateRate()*100,
				percentile(r.Latency, 0.50).Round(time.Microsecond*100),
				percentile(r.Latency, 0.95).Round(time.Microsecond*100),
				tps, tokensPerCase)
		}
	} else {
		fmt.Fprintln(w, "lane\ttotal\taccuracy\tauto\treview\tescalate")
		for _, r := range []LaneResult{laneS, laneG} {
			fmt.Fprintf(w, "%s\t%d\t%.1f%%\t%d\t%d\t%d\n",
				r.Name, r.Total, r.accuracy()*100, r.Auto, r.Review, r.Escalate)
		}
	}
	if err := w.Flush(); err != nil {
		return err
	}
	if !live {
		fmt.Println("latency/throughput hidden in mock mode — they measure the mock, not a model. Set MANGLE_SCORER_BACKEND to see them.")
	}
	fmt.Println("lane S = restricted-softmax scoring + policy gate (Jev's inference path, governed)")
	fmt.Println("lane G = generate-then-parse (letter in the first 100 chars); a choice it cannot parse escalates via the no_valid_choice rule")
	return nil
}

// scoreLaneWork: score -> gate. Returns the business choice and the gate
// outcome; tokens is always 0 (the score API generates nothing).
func scoreLaneWork(scorer Scorer, gate *Gate, th Thresholds) func(context.Context, datasets.Case) (string, string, int64, error) {
	return func(ctx context.Context, c datasets.Case) (string, string, int64, error) {
		opts := optionsFor(c)
		dec, err := scorer.Decide(ctx, BuildPrompt(c, opts), optionLetters, opts)
		if err != nil {
			return "", "", 0, err
		}
		outcome, err := gate.Route(ctx, c.ID, dec, th)
		if err != nil {
			return "", "", 0, err
		}
		return dec.Choice, outcome, 0, nil
	}
}

// generateLaneWork: generate -> parse -> gate. Without a probability the
// policy's only evidence is "a letter came back"; the gate still decides,
// and an unparseable completion escalates through the rule, not if-code.
func generateLaneWork(gen Generator, gate *Gate) func(context.Context, datasets.Case) (string, string, int64, error) {
	return func(ctx context.Context, c datasets.Case) (string, string, int64, error) {
		opts := optionsFor(c)
		completion, completionTokens, err := gen.Generate(ctx, BuildPrompt(c, opts))
		if err != nil {
			return "", "", 0, err
		}
		letter := ParseChoice(completion)
		if letter == "" {
			outcome, err := gate.RouteNoChoice(ctx, c.ID)
			return "", outcome, completionTokens, err
		}
		idx := -1
		for i, l := range optionLetters {
			if l == letter {
				idx = i
			}
		}
		if idx < 0 {
			outcome, err := gate.RouteNoChoice(ctx, c.ID)
			return "", outcome, completionTokens, err
		}
		choice := opts[idx]
		dec := &Decision{
			Choice:       choice,
			Distribution: map[string]float64{choice: 1},
			Margin:       1,
		}
		// The generation lane produces no probability, so there is nothing
		// for a confidence threshold to bite on — every parsed answer sails
		// through with a forced 1.0. That asymmetry is exactly what the
		// scoring lane's calibrated gate replaces.
		outcome, err := gate.Route(ctx, c.ID, dec, Thresholds{})
		return choice, outcome, completionTokens, err
	}
}

// newGenerator mirrors newScorer for the generation lane.
func newGenerator(live bool, cases []datasets.Case) (Generator, bool) {
	if live {
		if g, ok := NewSGLangGeneratorFromEnv(); ok {
			fmt.Printf("generation lane: live %s /v1/chat/completions (model %s)\n", g.BaseURL, g.Model)
			return g, true
		}
	}
	// Simulated primacy bias keeps lane G order-sensitive on purpose —
	// see permute.go and the note in MockGenerator.
	mg := NewMockGenerator(cases)
	mg.PrimacyBias = 0.06
	fmt.Println("generation lane: deterministic mock (simulated primacy bias +0.06 on the first-listed option)")
	return mg, false
}

func modeWord(live bool) string {
	if live {
		return "live"
	}
	return "mock"
}

func totalWindow(ds []time.Duration) float64 {
	var sum time.Duration
	for _, d := range ds {
		sum += d
	}
	return sum.Seconds()
}
