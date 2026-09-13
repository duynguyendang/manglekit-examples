// learn_from_code answers: Can the repo learn policy from its own code, answer questions while coding, and only tighten through human promotion? (UC-L8)
// Package main implements the learn_from_code example: the UC-L8
// "learn from code" skill (intent-routed entry point conceptually named
// `manglekit`). It shows that code-derived policy learning fits entirely in
// APPLICATION CODE on top of public surfaces — no kernel changes:
//
//	intent router   deterministic flag/signal classification (LEARN | EVAL |
//	                PROMOTE | STATUS; ambiguous → LEARN first, shadow EVAL;
//	                the chosen mode is always printed as `intent=...`)
//	LEARN           source tree → deterministic signal extraction → template
//	                induction → signed x/genes candidates at advisory tiers
//	                ONLY (T2 composite / T3 per-signal), written to a pool
//	                YAML with Source provenance. Never WithAllowHardTiers.
//	EVAL            pool genes → sdk.Client policy (x/genes.ApplyTo) +
//	                supervised Execute with code_signal facts; prints
//	                allow/deny. Hard-tier genes are skipped unless --confirm.
//	PROMOTE         the ONE human-gated step: requires --confirm, re-signs
//	                the composite gene at T1 and demonstrates that the same
//	                input that was advisory before now blocks.
//	STATUS          list pool genes: name | tier | source | signature.
//
// Guardrails (docs/design/learning-engine-design.md, ADR-003):
// candidates are advisory until human promotion; extraction failures write
// nothing; the example imports no manglekit/internal/* package.
//
// Try it:
//
//	go run ./learn_from_code --learn ../../manglekit          # whole source tree
//	go run ./learn_from_code --learn                          # tree = cwd
//	go run ./learn_from_code --status
//	go run ./learn_from_code --eval --input ./testdata/snippet.go
//	go run ./learn_from_code --promote --gene lc-<sha8>-review --confirm
//	go run ./learn_from_code --eval --input ./testdata/snippet.go --confirm

package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/x/genes"
)

const (
	// poolFile is the default candidate/committed gene pool name.
	poolFile = "genes.yaml"
	// defaultPoolDir keeps all example state in one place.
	defaultPoolDir = "./learned-pool"
	// gateAction is the supervised skill entry the policy is evaluated on.
	gateAction = "review_pr"
	// basePolicy declares the predicate all learned rules query. It lives in
	// the app's base policy (single Decl owner) so multiple genes can
	// reference code_signal without re-declaring it (which would collide).
	basePolicy = "Decl code_signal(Req, Sig)."
)

func main() {
	cfg, err := parseFlags(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, "learn_from_code:", err)
		os.Exit(1)
	}
	if err := run(context.Background(), os.Stdout, cfg); err != nil {
		fmt.Fprintln(os.Stderr, "learn_from_code:", err)
		os.Exit(1)
	}
}

// run is the testable entry point (out is injected for golden assertions).
func run(ctx context.Context, out io.Writer, cfg *config) error {
	decision := classify(cfg)
	fmt.Fprintf(out, "intent=%s", decision.Primary)
	if decision.ShadowEval {
		fmt.Fprintf(out, " (ambiguous input: shadow-EVAL will run after)")
	}
	fmt.Fprintf(out, " — %s\n", decision.Reason)

	switch decision.Primary {
	case ModeLearn:
		_, err := runLearn(ctx, out, cfg)
		if err != nil {
			return err
		}
		if decision.ShadowEval {
			fmt.Fprintln(out, "\n--- shadow EVAL (prediction only, nothing applied) ---")
			// The candidate was just merged into the pool by runLearn; the
			// shadow EVAL reads the pool — no `written` extra (that would
			// load the same rules twice).
			if err := runEval(ctx, out, cfg); err != nil {
				fmt.Fprintf(out, "shadow eval failed: %v\n", err)
			}
		}
		return nil
	case ModeEval:
		return runEval(ctx, out, cfg)
	case ModePromote:
		return runPromote(ctx, out, cfg)
	case ModeStatus:
		return runStatus(out, cfg)
	default:
		return fmt.Errorf("unknown mode %q", decision.Primary)
	}
}

// =========================================================================
// LEARN
// =========================================================================

// runLearn extracts signals, induces advisory genes, and merges them into
// the pool file. Fails closed: any extraction/compile error leaves the pool
// untouched (genes are written only after full validation).
func runLearn(ctx context.Context, out io.Writer, cfg *config) ([]genes.Gene, error) {
	if !cfg.learnSeen && len(cfg.learn) == 0 {
		return nil, fmt.Errorf("LEARN not requested (internal routing error)")
	}
	roots := cfg.learn
	if len(roots) == 0 {
		roots = []string{"."}
	}
	targets, err := resolveLearnPaths(cfg.learn)
	if err != nil {
		return nil, fmt.Errorf("LEARN failed closed (nothing written): %w", err)
	}
	fmt.Fprintf(out, "scanning %d .go file(s) under %s (excluded: tests, testdata, vendor, .git)\n",
		len(targets), strings.Join(roots, " "))
	ex, err := extractSignals(targets)
	if err != nil {
		return nil, fmt.Errorf("extraction failed (nothing written): %w", err)
	}
	if len(ex.Keys) == 0 {
		// A clean tree is an HONEST answer, not a failure: nothing to write.
		fmt.Fprintf(out, "no learnable signals in %d file(s) — pool unchanged\n", len(targets))
		return nil, nil
	}
	fmt.Fprintf(out, "extracted %d signal(s) from %d signal-bearing file(s) [%s]: %s\n",
		len(ex.Keys), len(ex.Files), ex.SHAC8(), strings.Join(ex.Keys, ", "))

	candidates := induceGenes(ex)

	// Validate the full candidate set compiles as ADVISORY policy — a
	// candidate that cannot compile is never written.
	validation, err := genes.Compile(candidates)
	if err != nil {
		return nil, fmt.Errorf("candidate genes failed compile (nothing written): %w", err)
	}
	_ = validation

	poolPath := filepath.Join(cfg.poolDir, poolFile)
	if err := os.MkdirAll(cfg.poolDir, 0o755); err != nil {
		return nil, err
	}
	pool, err := loadOrCreatePool(poolPath)
	if err != nil {
		return nil, fmt.Errorf("existing pool unreadable (nothing written): %w", err)
	}
	for _, g := range candidates {
		pool = upsertGene(pool, g)
		fmt.Fprintf(out, "candidate gene: %s | tier: %s | source: %s | sig: %s\n",
			g.Name, g.Tier, g.Source, shortSig(g))
	}
	f, err := os.Create(poolPath)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if err := genes.WritePool(f, pool); err != nil {
		return nil, err
	}
	fmt.Fprintf(out, "pool written: %s (%d gene(s), all advisory)\n", poolPath, len(pool))
	fmt.Fprintf(out, "next: --status ; --eval --input <file> ; promote with --confirm\n")
	return candidates, nil
}

// =========================================================================
// EVAL
// =========================================================================

// runEval applies the pool's advisory genes to a fresh client and assesses
// code_signal facts on the supervised path. Hard-tier genes are skipped
// unless --confirm is passed (human-review opt-in, same discipline as
// x/genes.WithAllowHardTiers).
func runEval(ctx context.Context, out io.Writer, cfg *config, extra ...genes.Gene) error {
	// Signals to assess: re-extract from --input, or take --signals csv.
	// A clean draft is the happy path of ASK and needs no pool at all.
	keys, err := evalSignals(cfg)
	if err != nil {
		return err
	}
	if len(keys) == 0 {
		if cfg.input != "" || len(cfg.signals) > 0 {
			fmt.Fprintln(out, "verdict: ALLOW (draft trips no known code signal — nothing for the learned material to say)")
			return nil
		}
		return fmt.Errorf("nothing to evaluate (pass --input <file.go> or --signals a,b)")
	}
	fmt.Fprintf(out, "assessing %d signal(s): %s\n", len(keys), strings.Join(keys, ", "))

	poolPath := cfg.pool
	if poolPath == "" {
		poolPath = filepath.Join(cfg.poolDir, poolFile)
	}
	pool, err := genes.LoadPoolFile(poolPath)
	if err != nil {
		return fmt.Errorf("load pool: %w", err)
	}
	advisory, hard := splitByTier(pool.Genes())
	if len(hard) > 0 {
		if !cfg.confirm {
			names := make([]string, len(hard))
			for i, g := range hard {
				names[i] = fmt.Sprintf("%s(%s)", g.Name, g.Tier)
			}
			fmt.Fprintf(out, "skipping hard-tier gene(s) %s — pass --confirm to apply them\n",
				strings.Join(names, ", "))
		} else {
			advisory = append(advisory, hard...)
		}
	}
	applied := append(append([]genes.Gene{}, advisory...), extra...)
	if len(applied) == 0 {
		return fmt.Errorf("no advisory genes in %s — run --learn first", poolPath)
	}

	client, err := sdk.NewClient(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Shutdown(ctx) }()

	if err := client.LoadPolicy(ctx, basePolicy); err != nil {
		return fmt.Errorf("load base declaration: %w", err)
	}
	opts := []genes.CompileOption{}
	if cfg.confirm {
		opts = append(opts, genes.WithAllowHardTiers())
	}
	if err := genes.ApplyTo(ctx, client, applied, opts...); err != nil {
		return fmt.Errorf("apply genes: %w", err)
	}
	client.RegisterSupervised(gateAction, &reviewAction{})

	facts := make([]string, 0, len(keys))
	for _, k := range keys {
		facts = append(facts, fmt.Sprintf("code_signal(%q, %q).", core.EntityInput, k))
	}
	env := core.Envelope{Payload: "learn_from_code eval", Facts: facts}

	_, execErr := client.ExecuteByName(ctx, gateAction, env)
	switch {
	case execErr == nil:
		fmt.Fprintln(out, "verdict: ALLOW (no blocking-tier violation)")
		// The gate swallows advisory halts by design (they must not block
		// callers) — but an agent ASKING deserves to know WHICH lessons
		// its draft tripped. Surface them via a direct engine Assess,
		// which reports every halt tier.
		if assessErr := client.Engine().Assess(ctx, core.ActionMetadata{Name: gateAction}, env); assessErr != nil {
			var align *core.AlignmentError
			if errors.As(assessErr, &align) {
				fmt.Fprintf(out, "advisory lesson fired: tier=%s reason=%q\n", align.Tier, align.Message)
				fmt.Fprintln(out, "→ check --status provenance: change the draft or be ready to justify it in the PR")
			}
		}
		return nil
	case core.IsPolicyViolationError(execErr):
		fmt.Fprintf(out, "verdict: DENY — %v\n", execErr)
		return nil
	default:
		return fmt.Errorf("gate error: %w", execErr)
	}
}

// =========================================================================
// PROMOTE
// =========================================================================

// runPromote is the single human-gated write: it converts the advisory T2
// composite gene into a T1 governance gene (rewritten rules, re-signed) and
// re-evaluates the same input to show advisory → DENY. Without --confirm it
// refuses by design.
func runPromote(ctx context.Context, out io.Writer, cfg *config) error {
	if cfg.gene == "" {
		return fmt.Errorf("PROMOTE needs --gene <name> (see --status)")
	}
	if !cfg.confirm {
		fmt.Fprintln(out, "REFUSED: promotion turns an advisory lesson into a blocking rule.")
		fmt.Fprintln(out, "This is a human-review action — re-run with --confirm to acknowledge review.")
		fmt.Fprintln(out, "No gene was changed.")
		return nil
	}
	poolPath := cfg.pool
	if poolPath == "" {
		poolPath = filepath.Join(cfg.poolDir, poolFile)
	}
	loaded, err := genes.LoadPoolFile(poolPath)
	if err != nil {
		return err
	}
	pool := loaded.Genes()
	idx := -1
	for i, g := range pool {
		if g.Name == cfg.gene {
			idx = i
		}
	}
	if idx < 0 {
		return fmt.Errorf("gene %q not found in %s (candidates: %s)",
			cfg.gene, poolPath, strings.Join(geneNames(pool), ", "))
	}
	g := pool[idx]
	if g.Tier != core.TierT2_Playbook {
		return fmt.Errorf("gene %q is %s — this example promotes only the T2 composite; T3 per-signal genes stay advisory", g.Name, g.Tier)
	}

	promoted := genes.Gene{
		Name:    g.Name + "-promoted",
		Tier:    core.TierT1_Governance,
		Source:  fmt.Sprintf("human-reviewed promotion of %s via learn_from_code --confirm", g.Name),
		Intents: g.Intents,
		Rules:   strings.ReplaceAll(g.Rules, `"`+string(core.TierT2_Playbook)+`"`, `"`+string(core.TierT1_Governance)+`"`),
	}
	promoted.Sign()
	if _, err := genes.Compile([]genes.Gene{promoted}, genes.WithAllowHardTiers()); err != nil {
		return fmt.Errorf("promoted gene failed validation (nothing written): %w", err)
	}
	pool = upsertGene(pool, promoted)
	f, err := os.Create(poolPath)
	if err != nil {
		return err
	}
	defer f.Close()
	if err := genes.WritePool(f, pool); err != nil {
		return err
	}
	fmt.Fprintf(out, "promoted: %s → %s (tier %s, re-signed %s)\n", g.Name, promoted.Name, promoted.Tier, shortSig(promoted))

	// Re-evaluate the SAME code that produced the lesson: the source files
	// are recovered from the gene's provenance (Source field), so promotion
	// always demonstrates its own effect.
	cfgCopy := *cfg
	cfgCopy.confirm = true
	if len(cfgCopy.signals) == 0 && cfgCopy.input == "" {
		// Re-evaluate the lesson itself: replay the signal keys the gene's
		// rules react to (path-free, proven by the rules — location-stable
		// like the pool). Source stays human provenance for the reviewer.
		cfgCopy.signals = ruleSignalKeys(g.Rules)
	}
	fmt.Fprintln(out, "\n--- re-EVAL with --confirm (policy now contains a T1 gene) ---")
	return runEval(ctx, out, &cfgCopy)
}

// codeSignalKeyRE extracts the signal keys a compiled rule reacts to.
var codeSignalKeyRE = regexp.MustCompile(`code_signal\("[^"]*",\s*"([^"]+)"\)`)

// ruleSignalKeys replays a gene's own signal vocabulary — the re-eval after
// promotion proves exactly what the lesson keys on, independent of file
// locations.
func ruleSignalKeys(rules string) []string {
	var keys []string
	for _, m := range codeSignalKeyRE.FindAllStringSubmatch(rules, -1) {
		keys = append(keys, m[1])
	}
	return keys
}

// =========================================================================
// STATUS
// =========================================================================

func runStatus(out io.Writer, cfg *config) error {
	poolPath := cfg.pool
	if poolPath == "" {
		poolPath = filepath.Join(cfg.poolDir, poolFile)
	}
	pool, err := genes.LoadPoolFile(poolPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(out, "pool %s: empty (run --learn first)\n", poolPath)
			return nil
		}
		return err
	}
	fmt.Fprintf(out, "pool %s:\n", poolPath)
	for _, g := range pool.Genes() {
		fmt.Fprintf(out, "  %-32s tier=%-2s sig=%s source=%s\n", g.Name, g.Tier, shortSig(g), g.Source)
	}
	return nil
}

// =========================================================================
// shared plumbing
// =========================================================================

// reviewAction is the mock supervised capability (no API key).
type reviewAction struct{}

func (*reviewAction) Execute(_ context.Context, in core.Envelope) (core.Envelope, error) {
	return core.Envelope{Payload: "reviewed: " + fmt.Sprint(in.Payload)}, nil
}

func (*reviewAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: gateAction}
}

func loadOrCreatePool(path string) ([]genes.Gene, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	pool, err := genes.LoadPool(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return pool.Genes(), nil
}

func upsertGene(pool []genes.Gene, g genes.Gene) []genes.Gene {
	for i := range pool {
		if pool[i].Name == g.Name {
			pool[i] = g
			return pool
		}
	}
	return append(pool, g)
}

func splitByTier(gs []genes.Gene) (advisory, hard []genes.Gene) {
	for _, g := range gs {
		if g.Tier == core.TierT0_Axiom || g.Tier == core.TierT1_Governance {
			hard = append(hard, g)
			continue
		}
		advisory = append(advisory, g)
	}
	return
}

func geneNames(gs []genes.Gene) []string {
	out := make([]string, len(gs))
	for i, g := range gs {
		out[i] = g.Name
	}
	return out
}

func shortSig(g genes.Gene) string { return g.SignatureHex()[:8] }

func evalSignals(cfg *config) ([]string, error) {
	if len(cfg.signals) > 0 {
		return cfg.signals, nil
	}
	if cfg.input != "" {
		targets, err := resolveLearnPaths([]string{cfg.input})
		if err != nil {
			return nil, err
		}
		ex, err := extractSignals(targets)
		if err != nil {
			return nil, err
		}
		return ex.Keys, nil
	}
	return nil, nil
}
