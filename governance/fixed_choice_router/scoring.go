package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
	"github.com/duynguyendang/manglekit/adapters/scoring"
)

// The scoring machinery lives in the kernel now (manglekit/adapters/scoring,
// ADR-005): restricted softmax, single-token label validation, the two hosted
// backends, threshold reading and envelope building. This file keeps only what
// is example-specific — the fixture-replaying mock and the ref lookup both
// mock backends share.
//
// Aliases keep the tour/bench/calibration code reading the same as before.
type (
	Decision = scoring.Decision
	Scorer   = scoring.Scorer
)

// ErrLabelsUnobserved is the kernel sentinel: some option's probability was
// not observable, so the policy (not Go) refuses to auto-route.
var ErrLabelsUnobserved = scoring.ErrLabelsUnobserved

// MockScorer replays the probability vectors stored in the committed fixtures.
// It is found by the reference id embedded in the prompt text, which keeps the
// interface prompt-driven exactly like the live backends — the same prompt goes
// to a mock in CI and to a model in live mode.
type MockScorer struct {
	byRef map[string]datasets.Case
}

func NewMockScorer(cases []datasets.Case) *MockScorer {
	m := &MockScorer{byRef: map[string]datasets.Case{}}
	for _, c := range cases {
		m.byRef[c.ID] = c
	}
	return m
}

var _ Scorer = (*MockScorer)(nil)

func (m *MockScorer) Score(_ context.Context, prompt string, labels, options []string) (*Decision, error) {
	c, ok := LookupCase(m.byRef, prompt)
	if !ok {
		return nil, fmt.Errorf("mock scorer: unknown case in prompt %q", prompt)
	}
	probs := make([]float64, len(labels))
	for i := range labels {
		probs[i] = c.Probs[options[i]]
	}
	// Build is the kernel's restricted softmax: normalizes over exactly these
	// options, rejects duplicates, derives margin + entropy.
	d, err := scoring.Build(labels, options, probs)
	if err != nil {
		return nil, err
	}
	d.Source = "mock_fixture"
	return d, nil
}

// LookupCase finds the fixture case whose "ref <id>" marker appears in a
// prompt. Shared by the mock scorer and the mock generator.
func LookupCase(byRef map[string]datasets.Case, prompt string) (datasets.Case, bool) {
	start := strings.Index(prompt, "ref ")
	if start < 0 {
		return datasets.Case{}, false
	}
	fields := strings.Fields(prompt[start+len("ref "):])
	if len(fields) == 0 {
		return datasets.Case{}, false
	}
	id := strings.TrimRight(fields[0], `).,];:"'`)
	c, ok := byRef[id]
	return c, ok
}

// MockTokenizer is the keyless stand-in for a server tokenizer: the single
// ASCII letters used as option labels are one token, anything longer is not.
// It exists so the single-token rule can be demonstrated in CI.
type MockTokenizer struct{}

func (MockTokenizer) TokenLen(_ context.Context, text string) (int, error) {
	t := strings.TrimSpace(text)
	switch {
	case t == "":
		return 0, fmt.Errorf("empty label")
	case len(t) == 1 && t[0] >= 'A' && t[0] <= 'Z':
		return 1, nil
	default:
		return len(t), nil // multi-char labels fail validation, as intended
	}
}

var _ scoring.Tokenizer = MockTokenizer{}
