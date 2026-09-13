// skill_learning answers: Do agents get better across sessions without the kernel deciding anything? (UC-L1, UC-L2)
// skill_learning demonstrates CROSS-SESSION skill learning on the OODA
// loop, using only public extension points (no kernel changes):
//
//   - ooda.Memory (Recall/Commit/Store/Query): the experience store.
//     Recall hydrates the Orient phase with learned skill hints;
//     Commit is called automatically by east.RunOODAEAST when the
//     frame passes validation — the implementation acts as the learner,
//     distilling "what fixed the draft" into a persistent skill atom.
//   - ports.ReasoningPort (SteerKB hook): RunOODAEAST consults it for
//     fast_path(<frame-id>) routing decisions. The implementation answers
//     from a persisted routes file, so a proven task gets the fast path
//     in every later session.
//   - File-backed state (JSON Lines) survives process restarts: session 2
//     builds a NEW client, memory, and reasoner from scratch and still
//     inherits everything session 1 learned.
//
// Flow: session 1 runs a task cold (quality violation → teacher-student
// refinement → pass → auto-Commit learns the skill). Session 2 re-runs the
// same task after a simulated restart: Recall injects the learned hint,
// validation passes on the FIRST attempt, and SteerKB takes the learned
// fast path.
//
// No API key required (deterministic mock brain + tool registry).

package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/x/east"
	"github.com/duynguyendang/manglekit/x/ooda"
	"github.com/google/uuid"
)

const task = "Generate architecture overview for the payments service"

// skillKey gives a frame a STABLE identity per task, so learned routes
// (fast_path keyed by frame.ID) are reusable across sessions.
func skillKey(t string) uuid.UUID {
	return uuid.NewSHA1(uuid.NameSpaceURL, []byte("manglekit-skill:"+t))
}

// =========================================================================
// skillMemory: ooda.Memory persisted as JSON Lines — the skill library
// =========================================================================

type skillMemory struct {
	mu    sync.Mutex
	path  string
	atoms []ooda.Atom
}

func openSkillMemory(path string) (*skillMemory, error) {
	m := &skillMemory{path: path}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil // cold library
		}
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var a ooda.Atom
		if err := json.Unmarshal([]byte(line), &a); err != nil {
			return nil, fmt.Errorf("skill library line %q: %w", line, err)
		}
		m.atoms = append(m.atoms, a)
	}
	return m, nil
}

// Recall is called by the Orient phase; returns learned hints for this input.
func (m *skillMemory) Recall(_ context.Context, input string) ([]ooda.Atom, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ooda.Atom
	for _, a := range m.atoms {
		if a.Predicate == "skill_hint" && a.Subject == input {
			out = append(out, a)
		}
	}
	return out, nil
}

// Commit is the LEARNER. east.RunOODAEAST calls it automatically when the
// frame passes post-act validation. If the draft needed refinement to pass,
// we distill what fixed it into one persistent skill hint.
func (m *skillMemory) Commit(ctx context.Context, frame *ooda.CognitiveFrame) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Skill was only proven if a refinement iteration was needed.
	if frame.RetryCount < 2 {
		return nil
	}
	for _, a := range m.atoms {
		if a.Predicate == "skill_hint" && a.Subject == frame.Input {
			return nil // already learned
		}
	}
	return m.storeLocked(ctx, ooda.Atom{
		Predicate:    "skill_hint",
		Subject:      frame.Input,
		Object:       "produce an outline section before the prose body (learned after teacher-student refinement)",
		Weight:       0.9,
		OriginIntent: frame.Intent,
	})
}

// Store appends one atom and persists it.
func (m *skillMemory) Store(ctx context.Context, atom ooda.Atom) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.storeLocked(ctx, atom)
}

func (m *skillMemory) storeLocked(_ context.Context, atom ooda.Atom) error {
	line, err := json.Marshal(atom)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(m.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	m.atoms = append(m.atoms, atom)
	return nil
}

// Query returns all atoms with the given predicate.
func (m *skillMemory) Query(_ context.Context, predicate string) ([]ooda.Atom, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ooda.Atom
	for _, a := range m.atoms {
		if a.Predicate == predicate {
			out = append(out, a)
		}
	}
	return out, nil
}

// =========================================================================
// skillReasoner: ports.ReasoningPort answering SteerKB route queries
// from a persisted routes file
// =========================================================================

type skillReasoner struct {
	mu     sync.Mutex
	path   string
	routes map[string]bool // "fast_path|<uuid>" → learned
	// lastAnswer records what this session's run was told (demo/assert hook).
	lastAnswer string
}

func openSkillReasoner(path string) (*skillReasoner, error) {
	r := &skillReasoner{path: path, routes: map[string]bool{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return r, nil
		}
		return nil, err
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var rec struct{ Predicate, Subject string }
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			return nil, err
		}
		r.routes[rec.Predicate+"|"+rec.Subject] = true
	}
	return r, nil
}

var steerQueryRE = regexp.MustCompile(`^\s*(fast_path|slow_path)\(([^)\s]+)\)\s*$$`)

// VerifyWithDatalog answers the queries east.SteerKB issues:
// fast_path(<frame-id>) / slow_path(<frame-id>).
func (r *skillReasoner) VerifyWithDatalog(_ context.Context, datalogQuery string) ([]map[string]string, error) {
	m := steerQueryRE.FindStringSubmatch(strings.TrimSpace(datalogQuery))
	if m == nil {
		return nil, nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.routes[m[1]+"|"+m[2]] {
		r.lastAnswer = m[1]
		return []map[string]string{{"route": m[1]}}, nil
	}
	return nil, nil
}

// learnRoute crystallizes a routing decision for a task key and persists it.
func (r *skillReasoner) learnRoute(predicate string, key uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rk := predicate + "|" + key.String()
	if r.routes[rk] {
		return nil
	}
	line, err := json.Marshal(map[string]string{"predicate": predicate, "subject": key.String()})
	if err != nil {
		return err
	}
	f, err := os.OpenFile(r.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err := f.Write(append(line, '\n')); err != nil {
		return err
	}
	r.routes[rk] = true
	return nil
}

// =========================================================================
// skillBrain: fails quality validation until the learned hint is in context
// =========================================================================

type skillBrain struct {
	decision   *core.Decision
	violations int // mock "draft quality" debt: refined away over retries
}

func (b *skillBrain) Evaluate(_ context.Context, _ *ooda.CognitiveFrame) (*core.Decision, error) {
	return b.decision, nil
}

// Verify is invoked by eastPostAct (via ooda.ValidateAgainstRules).
// Without a learned skill hint the draft keeps violating the quality
// playbook (T2); with the hint it passes on the first attempt.
func (b *skillBrain) Verify(_ context.Context, frame *ooda.CognitiveFrame) (*core.AuditTrail, error) {
	for _, a := range frame.Context {
		if a.Predicate == "skill_hint" && a.Subject == frame.Input {
			return &core.AuditTrail{}, nil
		}
	}
	if b.violations > 0 {
		b.violations--
		return &core.AuditTrail{
			MatchedRules: []core.RuleInference{{
				RuleName:   "halt_quality_check",
				Predicate:  "halt",
				Tier:       "T2",
				SourceFile: "playbook.dl",
				Definition: `halt("Req", "outline missing", "T2")`,
			}},
		}, nil
	}
	return &core.AuditTrail{}, nil
}

func (b *skillBrain) LoadPolicy(_ context.Context, _ string) error { return nil }

// =========================================================================
// Sessions
// =========================================================================

type sessionReport struct {
	retries     int
	status      ooda.VerifyStatus
	hintsFed    int
	routeAnswer string
}

// runSession builds ALL state from scratch (simulating a fresh process)
// and runs one EAST loop over the shared task.
func runSession(ctx context.Context, dir string) (*ooda.CognitiveFrame, sessionReport, error) {
	mem, err := openSkillMemory(filepath.Join(dir, "skills.jsonl"))
	if err != nil {
		return nil, sessionReport{}, err
	}
	reasoner, err := openSkillReasoner(filepath.Join(dir, "routes.jsonl"))
	if err != nil {
		return nil, sessionReport{}, err
	}

	registry := ooda.NewRegistry()
	registry.MustRegister("generate_doc", func(context.Context, map[string]any) (string, error) {
		return "[mock] architecture overview (outline + body)", nil
	})

	brain := &skillBrain{
		decision: &core.Decision{
			Outcome: core.DecisionProceed,
			Action:  core.NewActionEnvelope("generate_doc", nil),
		},
		violations: 1, // the draft needs one refinement round unless the skill is known
	}

	frame := ooda.NewBuilder().
		WithInput(task).
		WithBrain(brain).
		WithRegistry(registry).
		WithMemory(mem).
		Build()
	frame.ID = skillKey(task) // stable skill identity across sessions
	frame.ReasoningPort = reasoner

	result, err := east.RunOODAEAST(ctx, frame)
	if err != nil {
		return result, sessionReport{}, err
	}

	// Learner side of routing: once a task has been proven, crystallize
	// the fast path for future sessions.
	if err := reasoner.learnRoute("fast_path", frame.ID); err != nil {
		return result, sessionReport{}, err
	}

	return result, sessionReport{
		retries:     result.RetryCount,
		status:      result.Status,
		hintsFed:    countHints(frame.Context),
		routeAnswer: reasoner.lastAnswer,
	}, nil
}

func countHints(atoms []ooda.Atom) int {
	n := 0
	for _, a := range atoms {
		if a.Predicate == "skill_hint" {
			n++
		}
	}
	return n
}

// =========================================================================
// Main
// =========================================================================

func main() {
	ctx := context.Background()
	dir, err := os.MkdirTemp("", "skill-learning-*")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	defer os.RemoveAll(dir)

	fmt.Println("OODA Cross-Session Skill Learning")
	fmt.Println("=================================")

	fmt.Println("\n--- Session 1 (cold skill library) ---")
	_, rep1, err := runSession(ctx, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session 1 failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  status:  %s (after %d attempt(s))\n", rep1.status, rep1.retries)
	fmt.Printf("  hints recalled into Orient: %d\n", rep1.hintsFed)
	fmt.Printf("  SteerKB route answer:       %q (no learned route yet)\n", rep1.routeAnswer)
	fmt.Printf("  → learner persisted skill_hint + fast_path to %s\n", dir)

	fmt.Println("\n--- Session 2 (restart: new frame, new memory, same files) ---")
	_, rep2, err := runSession(ctx, dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "session 2 failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("  status:  %s (after %d attempt(s))\n", rep2.status, rep2.retries)
	fmt.Printf("  hints recalled into Orient: %d\n", rep2.hintsFed)
	fmt.Printf("  SteerKB route answer:       %q (learned in session 1)\n", rep2.routeAnswer)

	fmt.Println("\nLearned: fewer refinement loops and a cheaper execution path,")
	fmt.Println("purely from persisted experience — across a full process restart.")
}
