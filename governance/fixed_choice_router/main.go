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
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
	"github.com/duynguyendang/manglekit/adapters/scoring"
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

// newScorer picks the scoring backend. Default is the deterministic mock —
// the repo contract is that examples and tests run with no GPU and no keys.
//
// Two live paths, both opt-in:
//
//	MANGLE_SCORER=openai  (or OPENAI_API_KEY + MANGLE_SCORER=openai)
//	                     key-only: /chat/completions with logprobs, the
//	                     restricted softmax built client-side. No server.
//	MANGLE_SCORER=sglang  (default when MANGLE_SCORER_BACKEND is set)
//	                     SGLang native /v1/score + label_token_ids: the
//	                     server does the restricted softmax. Needs a
//	                     scoring server, but never truncates candidates.
func newScorer(cases []datasets.Case) (Scorer, bool) {
	switch chooseBackend() {
	case backendHosted:
		s, err := scoring.NewChatScorer(scoring.ChatConfig{
			BaseURL:     hostedBaseURL(),
			APIKey:      os.Getenv("OPENAI_API_KEY"),
			Model:       hostedModel(),
			TopLogProbs: hostedTopLogProbs(),
		})
		if err != nil {
			log.Fatalf("MANGLE_SCORER=openai but the key-only scorer could not be built: %v", err)
		}
		fmt.Printf("scorer backend: key-only chat logprobs (model %s @ %s, top_logprobs=%d)\n", s.Model, s.BaseURL, s.TopLogProbs)
		return s, true
	case backendSGLang:
		base := strings.TrimRight(os.Getenv("MANGLE_SCORER_BACKEND"), "/")
		s, err := scoring.NewScoreServer(scoring.ScoreConfig{
			BaseURL:     base,
			Model:       os.Getenv("MANGLE_SCORER_MODEL"),
			Tokenizer:   scoring.NewHTTPTokenizer(base, nil),
			SpacePrefix: os.Getenv("MANGLE_SCORER_SPACE") == "1",
		})
		if err != nil {
			log.Fatalf("SGLang scorer could not be built: %v", err)
		}
		fmt.Printf("scorer backend: live %s (model %s)\n", s.BaseURL, s.Model)
		return s, true
	default:
		fmt.Println("scorer backend: deterministic mock (MANGLE_SCORER=openai for key-only chat logprobs; MANGLE_SCORER_BACKEND=... for a /v1/score server)")
		return NewMockScorer(cases), false
	}
}

// hostedBaseURL / hostedModel / hostedTopLogProbs mirror the kernel's
// OpenAI-compatible contract (OPENAI_BASE_URL, OPENAI_MODEL) so one env works
// for both the scorer and the generation lane.
func hostedBaseURL() string {
	if base := strings.TrimRight(strings.TrimSpace(os.Getenv("OPENAI_BASE_URL")), "/"); base != "" {
		return base
	}
	return "https://api.openai.com/v1"
}

func hostedModel() string {
	if m := strings.TrimSpace(os.Getenv("OPENAI_MODEL")); m != "" {
		return m
	}
	return "gpt-4o-mini"
}

func hostedTopLogProbs() int {
	if v := strings.TrimSpace(os.Getenv("MANGLE_LLM_TOP_LOGPROBS")); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= len(optionLetters) {
			return n
		}
		log.Printf("ignoring MANGLE_LLM_TOP_LOGPROBS=%q (want an int >= %d)", v, len(optionLetters))
	}
	return 20
}

type backendKind int

const (
	backendMock backendKind = iota
	backendSGLang
	backendHosted
)

// chooseBackend resolves the MANGLE_SCORER name, defaulting to SGLang when a
// scoring server is configured (v0.10.3 behaviour) and to the mock otherwise.
func chooseBackend() backendKind {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("MANGLE_SCORER"))) {
	case "openai", "llm", "chat", "hosted":
		return backendHosted
	case "sglang", "score", "server":
		return backendSGLang
	case "mock":
		return backendMock
	default:
		if strings.TrimSpace(os.Getenv("MANGLE_SCORER_BACKEND")) != "" {
			return backendSGLang
		}
		return backendMock
	}
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
	fmt.Printf("\nthresholds loaded FROM the policy (threshold_pm/2, per-mille integers): auto=%.2f margin=%.2f review=%.2f\n", th.Auto, th.Margin, th.Review)
	fmt.Println("(the policy also owns the COMPARISON — :lt/:ge over score_pm vs class_bar; edit policy.mg and rerun)")

	picks := tourPicks(cases)
	fmt.Printf("\n=== Tour: %d fixture cases through score -> restricted softmax -> policy gate ===\n\n", len(picks))

	shots := make([]tourShot, 0, len(picks))
	for _, c := range picks {
		opts := optionsFor(c)
		dec, err := scorer.Score(ctx, BuildPrompt(c, opts), optionLetters, opts)
		if errors.Is(err, ErrLabelsUnobserved) {
			// The distribution is unknowable for this case — let the policy
			// refuse, exactly as it refuses an unparseable generation.
			outcome, rerr := gate.RouteUnobserved(ctx, c.ID)
			if rerr != nil {
				return fmt.Errorf("route unobserved %s: %w", c.ID, rerr)
			}
			fmt.Printf("case %s\n  %v\n  -> %s\n", c.ID, err, outcome)
			shots = append(shots, tourShot{c: c, outcome: outcome})
			continue
		}
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
		shots = append(shots, tourShot{c: c, dec: dec, outcome: outcome})
	}

	// The generation lane's escape hatch: no parsable choice -> the policy,
	// not Go, refuses to guess.
	esc, err := gate.RouteNoChoice(ctx, "tour-genlane-noparse")
	if err != nil {
		return err
	}
	fmt.Printf("case tour-genlane-noparse (generation lane could not parse a letter)\n  -> %s\n\n", esc)

	// Explainable deny: pick a case the gate ACTUALLY held back under the
	// live thresholds and replay the exact facts it saw. If every case
	// cleared the bar, say so instead of manufacturing a deny.
	blocked := firstBlocked(shots)
	if blocked == nil {
		fmt.Printf("=== Explainable deny: skipped — no case was held back under auto>=%.2f ===\n", th.Auto)
		fmt.Println("    (every case cleared the bar; lower threshold(\"auto\") in policy.mg to see a proof tree)")
		fmt.Println()
	} else if err := explainBlocked(ctx, gate.client, th, *blocked); err != nil {
		return err
	}

	// Micro hot-reload shot: re-run the same scored cases under the relaxed
	// policy and show how the mix moves. Reported as counts, so it stays
	// honest whatever the fixture/backend shape happens to be.
	fmt.Println("=== Hot swap: ReloadPolicy(policy_relaxed.mg) — same cases, relaxed thresholds ===")
	if err := gate.client.ReloadPolicy(ctx, relaxedPolicyPath()); err != nil {
		return fmt.Errorf("reload relaxed policy: %w", err)
	}
	th2, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		return err
	}
	before := countOutcomes(shots)
	after := make(map[string]int)
	for _, s := range shots {
		if s.dec == nil { // unobserved cases stay escalated under any thresholds
			after[s.outcome]++
			continue
		}
		outcome, err := gate.Route(ctx, s.c.ID, s.dec, th2)
		if err != nil {
			return fmt.Errorf("re-route %s: %w", s.c.ID, err)
		}
		after[outcome]++
	}
	fmt.Printf("    thresholds now: auto>=%.2f review>=%.2f\n", th2.Auto, th2.Review)
	for _, k := range []string{OutcomeAuto, OutcomeReview, OutcomeEscalate} {
		arrow := ""
		if after[k] != before[k] {
			arrow = fmt.Sprintf("  (%+d)", after[k]-before[k])
		}
		fmt.Printf("    %-16s %d -> %d%s\n", k, before[k], after[k], arrow)
	}
	fmt.Println()

	// Fail-safe: a broken policy file is rejected; the active one keeps
	// serving unchanged.
	fmt.Println("=== Fail-safe reload: ReloadPolicy(policy_broken.mg) must be REJECTED ===")
	if err := gate.client.ReloadPolicy(ctx, brokenPolicyPath()); err == nil {
		return fmt.Errorf("broken policy was accepted — fail-safe is broken")
	} else {
		fmt.Printf("rejected as expected: %v\n", err)
	}
	if err := gate.client.ReloadPolicy(ctx, policyPath()); err != nil {
		return fmt.Errorf("restore strict policy: %w", err)
	}
	fmt.Println("policy.mg restored; the strict thresholds are back.")
	if live {
		fmt.Println("\n(live backend was used for scoring; unset MANGLE_SCORER / MANGLE_SCORER_BACKEND for the deterministic demo)")
	}
	return nil
}

// tourShot is one tour case with the decision it scored and the outcome the
// gate returned — kept so the Explain and hot-swap sections replay the SAME
// numbers the gate saw, never a re-scored approximation.
type tourShot struct {
	c       datasets.Case
	dec     *Decision
	outcome string
}

// firstBlocked returns the first case the gate actually held back (review
// preferred, since that is the threshold-bound story).
func firstBlocked(shots []tourShot) *tourShot {
	for i := range shots {
		if shots[i].outcome == OutcomeReview && shots[i].dec != nil {
			return &shots[i]
		}
	}
	for i := range shots {
		if shots[i].outcome == OutcomeEscalate && shots[i].dec != nil {
			return &shots[i]
		}
	}
	return nil
}

func countOutcomes(shots []tourShot) map[string]int {
	m := map[string]int{}
	for _, s := range shots {
		m[s.outcome]++
	}
	return m
}

// explainBlocked renders the derivation tree for one case the gate held back.
// The facts fed to Explain are built from the SAME scoring.Request the gate
// saw, so the proof is over the real input — not a hand-written approximation
// that can drift away from what the policy actually evaluated.
func explainBlocked(ctx context.Context, client *manglekit.Client, th Thresholds, s tourShot) error {
	env := scoring.Request(RouteTicketInput{CaseID: s.c.ID, Choice: s.dec.Choice}, s.dec, th)
	facts := append([]string{`action_operation("Req", "route_ticket").`}, env.Facts...)
	for k, v := range env.Metadata {
		facts = append(facts, fmt.Sprintf("meta(%q, %q).", k, fmt.Sprint(v)))
	}
	sort.Strings(facts) // deterministic proof order for golden assertions

	expl, err := client.Explain(ctx, `halt("Req", Reason, Tier)`, facts)
	if err != nil {
		return fmt.Errorf("explain: %w", err)
	}
	fmt.Printf("=== Why %s became %s (client.Explain proof tree) ===\n", s.c.ID, s.outcome)
	fmt.Printf("derived: %v\n", expl.Outcome)
	fmt.Println(expl.String())
	fmt.Printf("    the policy itself compared the score against the class bar (auto=%.2f, review=%.2f)\n\n",
		th.Auto, th.Review)
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
