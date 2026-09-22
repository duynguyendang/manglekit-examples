// fixed_choice_router answers: Can a routing decision be made with provable
// thresholds — the Jev-style fixed-choice recipe, governed? (UC-G6)
//
// It re-creates the inference path of the "Jev" blog (Avi Chawla, 2026-09):
// a single forward pass scores the probabilities of the option letters A/B/C
// at the answer position, a RESTRICTED SOFTMAX normalizes over exactly those
// labels, and confidence thresholds decide auto-route vs review vs escalate.
//
// Where the blog differs, and this example goes further:
//
//   - The blog's thresholds live in Python. Here threshold/2 is a Datalog
//     FACT: client.ReloadPolicy changes the numbers with no rebuild, no
//     redeploy — and a broken policy file is rejected fail-safe, the old one
//     keeps serving (see threshold_demo.go).
//   - Every denial carries its own proof: client.Explain renders the
//     derivation tree with the P-vs-T bindings the rule saw (see the tour).
//   - Every decision runs through the Zero-Trust pre-check: a T1 halt means
//     route_ticket never executed.
//
// Dual mode (repo contract: no GPU/API key needed by default):
//
//   - Default: deterministic MockScorer replaying committed fixture
//     probabilities from datasets/fixtures.jsonl. Proves the GATE mechanics.
//   - MANGLE_SCORER_BACKEND=http://localhost:30000 (+ MANGLE_SCORER_MODEL=...)
//     switches to a live OpenAI-compatible /v1/score server (SGLang).
//
// Modes: --mode=tour (default) | bench | threshold | calibrate | permute.
// See README.md for the parity matrix against the blog.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// optionLetters are the single-token answer labels; the tokenizer check in
// scoring.go rejects any label that is not exactly one token — the " A" vs
// "A" trap the blog calls out.
var optionLetters = []string{"A", "B", "C"}

// BuildPrompt renders the fixed-choice prompt. Options are enumerated as
// TEXT so permutation studies (permute.go) can re-order what the model sees.
func BuildPrompt(c datasets.Case, opts []string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Route this ticket to exactly one queue. Options: ")
	for i, o := range opts {
		if i > 0 {
			b.WriteString("; ")
		}
		fmt.Fprintf(&b, "%s) %s", optionLetters[i%3], o)
	}
	fmt.Fprintf(&b, ". Answer with the option letter only. Ticket: %q.", c.Text)
	return b.String()
}

// optionsFor returns the canonical A/B/C business options for a case.
func optionsFor(c datasets.Case) []string { return datasets.Options(c.Domain) }

func main() {
	log.SetFlags(0)
	mode := flag.String("mode", "tour", "tour | bench | threshold | calibrate | permute")
	casesFlag := flag.Int("cases", 0, "cap on benchmark cases (0 = all committed fixtures)")
	flag.Parse()

	ctx := context.Background()

	if *mode == "gen-fixtures" {
		c := datasets.Generate()
		if err := datasets.WriteJSONL(exampleDir()+"/datasets/fixtures.jsonl", c); err != nil {
			log.Fatalf("write fixtures: %v", err)
		}
		fmt.Printf("generated %d cases into datasets/fixtures.jsonl\n", len(c))
		return
	}

	cases, err := loadFixtures()
	if err != nil {
		log.Fatalf("load fixtures: %v", err)
	}
	scorer, live := newScorer(cases)

	switch *mode {
	case "tour":
		if err := runTour(ctx, scorer, live, cases); err != nil {
			log.Fatal(err)
		}
	case "bench":
		if err := runBenchmark(ctx, scorer, live, cases, *casesFlag); err != nil {
			log.Fatal(err)
		}
	case "threshold":
		if err := runThresholdDemo(ctx, scorer, cases); err != nil {
			log.Fatal(err)
		}
	case "calibrate":
		if err := runCalibration(ctx, scorer, live, cases); err != nil {
			log.Fatal(err)
		}
	case "permute":
		if err := runPermutation(ctx, scorer, live, cases); err != nil {
			log.Fatal(err)
		}
	default:
		log.Fatalf("unknown mode %q", *mode)
	}
}

// newScorer picks the backend: a live OpenAI-compatible server only when the
// env contract is fully set, the deterministic mock otherwise (the default
// everyone runs — repo contract: no GPU/API keys required).
func newScorer(cases []datasets.Case) (Scorer, bool) {
	if strings.TrimSpace(os.Getenv("MANGLE_SCORER_BACKEND")) != "" {
		s, err := NewSGLangScorerFromEnv()
		if err != nil {
			log.Fatalf("MANGLE_SCORER_BACKEND is set but the live scorer could not be built: %v", err)
		}
		fmt.Printf("scorer backend: live %s (model %s)\n", s.BaseURL, s.Model)
		return s, true
	}
	fmt.Println("scorer backend: deterministic mock (set MANGLE_SCORER_BACKEND + MANGLE_SCORER_MODEL for a live /v1/score server)")
	return NewMockScorer(cases), false
}

// runTour walks 10 fixture cases across the decision shapes and, for the
// two blocked ones, prints the policy's own proof of WHY.
func runTour(ctx context.Context, scorer Scorer, live bool, cases []datasets.Case) error {
	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		return err
	}
	defer gate.Shutdown(ctx)

	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		return err
	}
	fmt.Printf("\nthresholds loaded FROM the policy (threshold/2 rules): auto=%.2f margin=%.2f review=%.2f\n", th.Auto, th.Margin, th.Review)
	fmt.Println("(edit policy.dl and rerun — the numbers, not the code, changed)")

	picks := tourPicks(cases)
	fmt.Printf("\n=== Tour: %d fixture cases through score -> restricted softmax -> policy gate ===\n\n", len(picks))
	for _, c := range picks {
		opts := optionsFor(c)
		dec, err := scorer.Decide(ctx, BuildPrompt(c, opts), optionLetters, opts)
		if err != nil {
			return fmt.Errorf("score %s: %w", c.ID, err)
		}
		outcome, err := gate.Route(ctx, c.ID, dec, th)
		if err != nil {
			return fmt.Errorf("route %s: %w", c.ID, err)
		}
		fmt.Printf("case %s\n", c.ID)
		fmt.Printf("  prompt-opt: %s\n", summarizeOptions(opts))
		fmt.Printf("  P: %s  margin=%.3f entropy=%.3f\n", formatDist(dec.Distribution), dec.Margin, dec.Entropy)
		fmt.Printf("  -> %s\n", outcome)
	}

	// The generation lane's escape hatch: no parsable choice -> the policy,
	// not Go, refuses to guess.
	esc, err := gate.RouteNoChoice(ctx, "tour-genlane-noparse")
	if err != nil {
		return err
	}
	fmt.Printf("case tour-genlane-noparse (generation lane could not parse a letter)\n  -> %s\n\n", esc)

	// Explainable deny: derive the halt from the same facts the gate saw.
	blocked := picks[6] // a review-band case by construction
	if err := explainBlocked(ctx, gate.client, scorer, blocked); err != nil {
		return err
	}

	// Micro hot-reload shot: the relaxed policy moves this same case from
	// review to auto-route — no rebuild, no restart.
	fmt.Println("=== Hot swap: ReloadPolicy(policy_relaxed.dl): threshold auto 0.80 -> 0.70 ===")
	if err := gate.client.ReloadPolicy(ctx, relaxedPolicyPath()); err != nil {
		return fmt.Errorf("reload relaxed policy: %w", err)
	}
	th2, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		return err
	}
	opts := optionsFor(blocked)
	dec, err := scorer.Decide(ctx, BuildPrompt(blocked, opts), optionLetters, opts)
	if err != nil {
		return err
	}
	outcome, err := gate.Route(ctx, blocked.ID, dec, th2)
	if err != nil {
		return err
	}
	fmt.Printf("case %s with auto=%.2f: P=%.3f -> %s\n\n", blocked.ID, th2.Auto, dec.Distribution[dec.Choice], outcome)

	// Fail-safe: a broken policy file is rejected; the active one keeps
	// serving unchanged.
	fmt.Println("=== Fail-safe reload: ReloadPolicy(policy_broken.dl) must be REJECTED ===")
	if err := gate.client.ReloadPolicy(ctx, brokenPolicyPath()); err == nil {
		return fmt.Errorf("broken policy was accepted — fail-safe is broken")
	} else {
		fmt.Printf("rejected as expected: %v\n", err)
	}
	if err := gate.client.ReloadPolicy(ctx, policyPath()); err != nil {
		return fmt.Errorf("restore strict policy: %w", err)
	}
	fmt.Println("policy.dl restored; the strict thresholds are back.")
	if live {
		fmt.Println("\n(live backend was used for scoring; rerun without MANGLE_SCORER_BACKEND for the deterministic demo)")
	}
	return nil
}

// explainBlocked renders the derivation tree for one review-band decision.
func explainBlocked(ctx context.Context, client *manglekit.Client, scorer Scorer, c datasets.Case) error {
	opts := optionsFor(c)
	dec, err := scorer.Decide(ctx, BuildPrompt(c, opts), optionLetters, opts)
	if err != nil {
		return err
	}
	top := dec.Distribution[dec.Choice]
	facts := []string{
		`action_operation("Req", "route_ticket").`,
		`meta("below_auto", "true").`,
		fmt.Sprintf("confidence(%q).", fmt.Sprintf("%.3f", top)),
		fmt.Sprintf("margin_value(%q).", fmt.Sprintf("%.3f", dec.Margin)),
	}
	expl, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, facts)
	if err != nil {
		return fmt.Errorf("explain: %w", err)
	}
	fmt.Printf("=== Why %s was NOT auto-route (client.Explain proof tree) ===\n", c.ID)
	fmt.Printf("derived: %v\n", expl.Outcome)
	fmt.Println(expl.String())
	return nil
}

// tourPicks selects 10 fixture cases covering every decision shape: 5
// confident, 3 review-band, 1 escalate, 1 decoy (a confidently-wrong
// fixture case that the gate routes — calibration will expose it).
func tourPicks(cases []datasets.Case) []datasets.Case {
	support := make([]datasets.Case, 0, 100)
	for _, c := range cases {
		if c.Domain == datasets.DomainSupport {
			support = append(support, c)
		}
	}
	// profile rotation guarantees indices 0-3 confident, 4-5 review,
	// 6 ambiguous, 7 decoy, 8 escalate, 9 auto_margin; take 10 spread.
	want := []int{0, 1, 2, 9, 3, 4, 5, 6, 8, 7}
	out := make([]datasets.Case, 0, len(want))
	for _, i := range want {
		if i < len(support) {
			out = append(out, support[i])
		}
	}
	return out
}

func summarizeOptions(opts []string) string {
	parts := make([]string, len(opts))
	for i, o := range opts {
		parts[i] = fmt.Sprintf("%s=%s", optionLetters[i], o)
	}
	return strings.Join(parts, " ")
}

func formatDist(dist map[string]float64) string {
	keys := make([]string, 0, len(dist))
	for k := range dist {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return dist[keys[i]] > dist[keys[j]] })
	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		parts = append(parts, fmt.Sprintf("%s:%.3f", k, dist[k]))
	}
	return strings.Join(parts, " ")
}

// loadFixtures reads the committed JSONL fixtures (cwd-safe).
func loadFixtures() ([]datasets.Case, error) {
	path := exampleDir() + "/datasets/fixtures.jsonl"
	return datasets.ReadJSONL(path)
}
