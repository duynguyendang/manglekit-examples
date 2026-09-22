package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// Decision is one fixed-choice routing call: the winning business option,
// the full probability distribution over the options, the top-vs-second
// margin, and the entropy of the distribution.
type Decision struct {
	Choice       string
	Distribution map[string]float64
	Margin       float64
	Entropy      float64
}

// Scorer answers: for this prompt, which single-token label is most likely?
// Implementations must return a distribution normalized over exactly the
// provided labels (restricted softmax — all other vocabulary mass is
// discarded, per the Jev-style fixed-choice protocol).
type Scorer interface {
	// Decide scores labels against prompt. labels are the option letters in
	// canonical order (A, B, C); options are the business names the letters
	// stand for. The returned Decision speaks business names.
	Decide(ctx context.Context, prompt string, labels, options []string) (*Decision, error)
}

// Tokenizer resolves a label string to its token count on the serving
// backend. Fixed-choice scoring is only valid when each label occupies
// exactly one token — " A" and "A" are different token IDs, and a label that
// splits into two tokens makes the logit reading meaningless.
type Tokenizer interface {
	TokenLen(ctx context.Context, text string) (int, error)
}

// ---------------------------------------------------------------------------
// Shared math
// ---------------------------------------------------------------------------

// restrictedSoftmax normalizes scores over the given log-probability vector.
// probs may arrive pre-normalized (SGLang apply_softmax=true); renormalizing
// is a defensive no-op that keeps the invariant sum(Distribution)==1.
func restrictedSoftmax(probs []float64) []float64 {
	sum := 0.0
	for _, p := range probs {
		if p < 0 {
			p = 0 // defensive: clamp numerical dust
		}
		sum += p
	}
	if sum <= 0 {
		return probs
	}
	out := make([]float64, len(probs))
	for i, p := range probs {
		out[i] = p / sum
	}
	return out
}

func buildDecision(labels, options []string, probs []float64) (*Decision, error) {
	if len(probs) != len(labels) {
		return nil, fmt.Errorf("scorer: got %d probabilities for %d labels", len(probs), len(labels))
	}
	p := restrictedSoftmax(probs)
	dist := make(map[string]float64, len(labels))
	for i, lb := range labels {
		if i >= len(options) {
			return nil, fmt.Errorf("scorer: label %q has no business option", lb)
		}
		dist[options[i]] = p[i]
	}
	best := -1
	for i, v := range p {
		if best < 0 || v > p[best] {
			best = i
		}
	}
	margin := 0.0
	if len(p) > 1 {
		sorted := make([]float64, len(p))
		copy(sorted, p)
		// selection-sort the top two
		for i := 0; i < 2; i++ {
			mx := i
			for j := i + 1; j < len(sorted); j++ {
				if sorted[j] > sorted[mx] {
					mx = j
				}
			}
			sorted[i], sorted[mx] = sorted[mx], sorted[i]
		}
		margin = sorted[0] - sorted[1]
	}
	entropy := 0.0
	for _, v := range p {
		if v > 0 {
			entropy -= v * math.Log(v)
		}
	}
	return &Decision{
		Choice:       options[best],
		Distribution: dist,
		Margin:       margin,
		Entropy:      entropy,
	}, nil
}

// validateSingleTokens rejects any label that does not occupy exactly one
// token on the backend tokenizer (the " A" vs "A" trap the Jev blog warns
// about).
func validateSingleTokens(ctx context.Context, tk Tokenizer, labels []string) error {
	for _, lb := range labels {
		n, err := tk.TokenLen(ctx, lb)
		if err != nil {
			return fmt.Errorf("tokenize label %q: %w", lb, err)
		}
		if n != 1 {
			return fmt.Errorf("label %q occupies %d tokens, fixed-choice scoring requires exactly 1", lb, n)
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// MockScorer — deterministic, keyless default (repo contract: tests run
// without GPU/API keys)
// ---------------------------------------------------------------------------

// MockScorer replays the probability vectors stored in the committed
// fixtures. It is found by the reference id embedded in the prompt text,
// which keeps the interface prompt-driven exactly like the live backend.
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

func (m *MockScorer) Decide(_ context.Context, prompt string, labels, options []string) (*Decision, error) {
	c, ok := LookupCase(m.byRef, prompt)
	if !ok {
		return nil, fmt.Errorf("mock scorer: unknown case in prompt %q", prompt)
	}
	if len(options) != len(labels) {
		return nil, fmt.Errorf("mock scorer: %d labels but %d options", len(labels), len(options))
	}
	probs := make([]float64, len(labels))
	for i := range labels {
		probs[i] = c.Probs[options[i]]
	}
	return buildDecision(labels, options, probs)
}

// LookupCase finds the fixture case whose "ref <id>" marker appears in the
// prompt. The ref token runs to the first whitespace or punctuation.
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

// MockTokenizer treats the single ASCII letters used as option labels as
// one-token labels — mirroring the server-side tokenizer without any HTTP.
type MockTokenizer struct{}

func (MockTokenizer) TokenLen(_ context.Context, text string) (int, error) {
	t := strings.TrimSpace(text)
	if len(t) == 1 && t[0] >= 'A' && t[0] <= 'Z' {
		return 1, nil
	}
	if t == "" {
		return 0, errors.New("empty label")
	}
	return len(t), nil // multi-char labels fail validation, as intended
}

var _ Tokenizer = MockTokenizer{}

// ---------------------------------------------------------------------------
// SGLangScorer — live backend, opt-in via MANGLE_SCORER_BACKEND
// ---------------------------------------------------------------------------

// SGLangScorer talks to an OpenAI-compatible /v1/score endpoint
// (SGLang native API). One request scores every label token in a single
// forward pass: the server reads the vocabulary distribution at the position
// after the prompt and returns the normalized probabilities of exactly
// label_token_ids — the restricted softmax, done where the logits live
// instead of shipping a 150k-dimension vector over HTTP.
type SGLangScorer struct {
	BaseURL       string
	Model         string
	Tok           Tokenizer
	Client        *http.Client
	SpacePrefixed bool // score " A" instead of "A" when the chat template glues a space

	// idCache memoizes label-set -> token-id resolution (constant per
	// server+label pair; safe for concurrent lanes).
	idCache sync.Map
}

// NewSGLangScorerFromEnv builds the live scorer from
// MANGLE_SCORER_BACKEND (server base URL) and MANGLE_SCORER_MODEL.
// Optional: MANGLE_SCORER_SPACE=1 for space-prefixed answer tokens;
// MANGLE_LABEL_TOKEN_IDS="id,id,..." to bypass /tokenize on servers that
// do not expose it.
func NewSGLangScorerFromEnv() (*SGLangScorer, error) {
	base := strings.TrimRight(os.Getenv("MANGLE_SCORER_BACKEND"), "/")
	if base == "" {
		return nil, errors.New("MANGLE_SCORER_BACKEND not set")
	}
	model := os.Getenv("MANGLE_SCORER_MODEL")
	if model == "" {
		return nil, errors.New("MANGLE_SCORER_MODEL not set")
	}
	return &SGLangScorer{
		BaseURL:       base,
		Model:         model,
		Tok:           &HTTPTokenizer{BaseURL: base, Client: &http.Client{Timeout: 15 * time.Second}},
		Client:        &http.Client{Timeout: 120 * time.Second},
		SpacePrefixed: os.Getenv("MANGLE_SCORER_SPACE") == "1",
	}, nil
}

var _ Scorer = (*SGLangScorer)(nil)

type scoreRequest struct {
	Model         string   `json:"model"`
	Query         string   `json:"query"`
	Items         []string `json:"items"`
	LabelTokenIDs []int    `json:"label_token_ids"`
	ApplySoftmax  bool     `json:"apply_softmax"`
}

type scoreResponse struct {
	Scores [][]float64 `json:"scores"`
}

// resolveLabelIDs maps the option letters to single token IDs, in label
// order. Resolution order:
//  1. MANGLE_LABEL_TOKEN_IDS ("32,33,34") — for deployments without a
//     /tokenize endpoint; the IDs are assumed validated out-of-band.
//  2. the backend /tokenize endpoint, which ALSO enforces the blog's
//     single-token rule (" A" and "A" are different token IDs).
//
// Results are cached: the mapping is constant for a (model, labels) pair.
func (s *SGLangScorer) resolveLabelIDs(ctx context.Context, labels []string) ([]int, error) {
	key := strings.Join(labels, "\x00")
	if cached, ok := s.idCache.Load(key); ok {
		return cached.([]int), nil
	}
	if env := strings.TrimSpace(os.Getenv("MANGLE_LABEL_TOKEN_IDS")); env != "" {
		parts := strings.Split(env, ",")
		ids := make([]int, 0, len(parts))
		for _, p := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(p))
			if err != nil {
				return nil, fmt.Errorf("MANGLE_LABEL_TOKEN_IDS: %q is not an int: %w", p, err)
			}
			ids = append(ids, n)
		}
		if len(ids) != len(labels) {
			return nil, fmt.Errorf("MANGLE_LABEL_TOKEN_IDS carries %d ids for %d labels", len(ids), len(labels))
		}
		s.idCache.Store(key, ids)
		return ids, nil
	}
	if err := validateSingleTokens(ctx, s.Tok, labels); err != nil {
		return nil, err
	}
	res, ok := s.Tok.(tokenIDResolver)
	if !ok {
		return nil, fmt.Errorf("tokenizer %T cannot resolve token ids", s.Tok)
	}
	ids := make([]int, len(labels))
	for i, lb := range labels {
		text := lb
		if s.SpacePrefixed {
			text = " " + lb
		}
		id, err := res.SingleTokenID(ctx, text)
		if err != nil {
			return nil, fmt.Errorf("resolve token id for %q: %w", text, err)
		}
		ids[i] = id
	}
	s.idCache.Store(key, ids)
	return ids, nil
}

func (s *SGLangScorer) Decide(ctx context.Context, prompt string, labels, options []string) (*Decision, error) {
	if len(labels) != len(options) {
		return nil, fmt.Errorf("sglang scorer: %d labels but %d options", len(labels), len(options))
	}
	ids, err := s.resolveLabelIDs(ctx, labels)
	if err != nil {
		return nil, err
	}
	body, _ := json.Marshal(scoreRequest{
		Model:         s.Model,
		Query:         prompt,
		Items:         []string{""},
		LabelTokenIDs: ids,
		ApplySoftmax:  true,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.BaseURL+"/v1/score", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := s.Client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("/v1/score: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("/v1/score: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var sr scoreResponse
	if err := json.Unmarshal(raw, &sr); err != nil {
		return nil, fmt.Errorf("/v1/score decode: %w", err)
	}
	if len(sr.Scores) == 0 || len(sr.Scores[0]) != len(ids) {
		return nil, fmt.Errorf("/v1/score: expected 1 x %d probabilities, got %v", len(ids), sr.Scores)
	}
	return buildDecision(labels, options, sr.Scores[0])
}

// ---------------------------------------------------------------------------
// Tokenizers
// ---------------------------------------------------------------------------

// HTTPTokenizer POSTs {base}/tokenize (OpenAI-compatible tokenization
// endpoint; SGLang and vLLM both expose it) and reports token count plus,
// for one-token text, the token id.
type HTTPTokenizer struct {
	BaseURL string
	Client  *http.Client
}

type tokenizeRequest struct {
	Text             string `json:"text"`
	AddSpecialTokens bool   `json:"add_special_tokens"`
}

type tokenizeResponse struct {
	Tokens  []int    `json:"tokens"`
	Count   int      `json:"count"`
	Content []string `json:"content"` // some servers return pieces instead of ids
}

func (t *HTTPTokenizer) TokenLen(ctx context.Context, text string) (int, error) {
	tr := tokenizeRequest{Text: text}
	body, _ := json.Marshal(tr)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/tokenize", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("/tokenize: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("/tokenize: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var tk tokenizeResponse
	if err := json.Unmarshal(raw, &tk); err != nil {
		return 0, fmt.Errorf("/tokenize decode: %w", err)
	}
	if len(tk.Tokens) > 0 {
		return len(tk.Tokens), nil
	}
	if tk.Count > 0 {
		return tk.Count, nil
	}
	if len(tk.Content) > 0 {
		return len(tk.Content), nil
	}
	return 0, errors.New("/tokenize: empty response")
}

// singleTokenID returns the token id when text is exactly one token, else an
// error. Used by the scorer after TokenLen validation.
func (t *HTTPTokenizer) singleTokenID(ctx context.Context, text string) (int, error) {
	body, _ := json.Marshal(tokenizeRequest{Text: text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.BaseURL+"/tokenize", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := t.Client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("/tokenize: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("/tokenize: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var tk tokenizeResponse
	if err := json.Unmarshal(raw, &tk); err != nil {
		return 0, err
	}
	if len(tk.Tokens) != 1 {
		return 0, fmt.Errorf("text %q is %d tokens, need exactly 1", text, len(tk.Tokens))
	}
	return tk.Tokens[0], nil
}

// tokenIDResolver is the scorer-side view of a tokenizer that can also map a
// single-token string to its id. The exported interface stays Tokenizer.
type tokenIDResolver interface {
	SingleTokenID(ctx context.Context, text string) (int, error)
}

// SingleTokenID adapts *HTTPTokenizer to the unexported resolver interface.
func (t *HTTPTokenizer) SingleTokenID(ctx context.Context, text string) (int, error) {
	return t.singleTokenID(ctx, text)
}

// MockTokenizer has no HTTP; it exposes a fixed mapping for the A/B/C labels.
func (MockTokenizer) SingleTokenID(_ context.Context, text string) (int, error) {
	t := strings.TrimSpace(text)
	if len(t) == 1 && t[0] >= 'A' && t[0] <= 'Z' {
		return int(rune('A') + rune(t[0]) - 'A'), nil
	}
	return 0, fmt.Errorf("mock tokenizer: multi-char label %q is not a single token", text)
}

var _ tokenIDResolver = (*HTTPTokenizer)(nil)
var _ tokenIDResolver = MockTokenizer{}
