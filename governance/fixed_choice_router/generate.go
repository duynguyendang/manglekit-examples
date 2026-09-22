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
	"strings"
	"time"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// Generator is the generation lane (Lane G of the blog): ask the model to
// answer with a letter, parse the first 100 characters. It reports the
// completion text and the completion-token count actually billed.
type Generator interface {
	Generate(ctx context.Context, prompt string) (string, int64, error)
}

// ParseChoice extracts the first option letter from a free-form completion,
// mirroring the blog's parser: only the first maxChars characters count, and
// anything unrecognizable becomes an empty string (the escape hatch input).
func ParseChoice(completion string) string {
	head := completion
	if len(head) > 100 {
		head = head[:100]
	}
	for i := 0; i < len(head); i++ {
		ch := head[i]
		if ch >= 'A' && ch <= 'C' {
			return string(ch)
		}
		if ch >= 'a' && ch <= 'c' {
			return strings.ToUpper(string(ch))
		}
	}
	return ""
}

// MockGenerator simulates the generation lane on fixtures. It answers with
// the letter of the highest-probability option under a documented, clearly
// labeled SIMULATED primacy bias (+PrimacyBias added to the option listed
// first in the prompt), which reproduces the order-sensitivity the blog's
// commenters asked about. It never pretends to measure real model latency:
// latency columns are only rendered in live mode.
type MockGenerator struct {
	cases       map[string]datasets.Case
	PrimacyBias float64
}

func NewMockGenerator(cases []datasets.Case) *MockGenerator {
	m := &MockGenerator{cases: map[string]datasets.Case{}}
	for _, c := range cases {
		m.cases[c.ID] = c
	}
	return m
}

// Generate returns the letter the mock model would emit for this prompt.
// Mock completions bill 1 completion token (a bare letter).
func (m *MockGenerator) Generate(_ context.Context, prompt string) (string, int64, error) {
	c, ok := LookupCase(m.cases, prompt)
	if !ok {
		return "", 0, fmt.Errorf("mock generator: unknown case in prompt %q", prompt)
	}
	// Recover the enumerated option order from the prompt itself.
	byPos, ok := optionsByLetter(prompt, optionsFor(c))
	if !ok {
		return "", 0, fmt.Errorf("mock generator: could not recover option enumeration from %q", prompt)
	}
	bestLetter, bestP := "", math.Inf(-1)
	for i, pos := range optionLetters {
		p := c.Probs[byPos[pos]]
		if i == 0 { // the FIRST-LISTED option carries the simulated bias
			p += m.PrimacyBias
		}
		if p > bestP {
			bestP, bestLetter = p, pos
		}
	}
	return bestLetter, 1, nil
}

// optionsByLetter parses the "A) name; B) name; C) name" enumeration of a
// prompt built by BuildPrompt, returning letter -> business option.
func optionsByLetter(prompt string, opts []string) (map[string]string, bool) {
	out := map[string]string{}
	for _, letter := range optionLetters {
		matched := false
		for _, o := range opts {
			if strings.Contains(prompt, fmt.Sprintf("%s) %s", letter, o)) {
				out[letter] = o
				matched = true
				break
			}
		}
		if !matched {
			return nil, false
		}
	}
	return out, true
}

// SGLangGenerator is the live generation lane: POST {base}/v1/chat/completions
// with temperature 0 and max_tokens 32 (the blog's exact budget).
type SGLangGenerator struct {
	BaseURL string
	Model   string
	Client  *http.Client
}

// NewSGLangGeneratorFromEnv builds the live generator from the same env
// contract as the scorer.
func NewSGLangGeneratorFromEnv() (*SGLangGenerator, bool) {
	base := strings.TrimRight(os.Getenv("MANGLE_SCORER_BACKEND"), "/")
	model := os.Getenv("MANGLE_SCORER_MODEL")
	if base == "" || model == "" {
		return nil, false
	}
	return &SGLangGenerator{BaseURL: base, Model: model, Client: &http.Client{Timeout: 120 * time.Second}}, true
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatResponse struct {
	Choices []struct {
		Message chatMessage `json:"message"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

func (g *SGLangGenerator) Generate(ctx context.Context, prompt string) (string, int64, error) {
	reqBody := map[string]any{
		"model": g.Model,
		"messages": []chatMessage{
			{Role: "user", Content: prompt},
		},
		"max_tokens":  32,
		"temperature": 0,
	}
	body, _ := json.Marshal(reqBody)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.BaseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := g.Client.Do(req)
	if err != nil {
		return "", 0, fmt.Errorf("/v1/chat/completions: %w", err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return "", 0, fmt.Errorf("/v1/chat/completions: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))
	}
	var cr chatResponse
	if err := json.Unmarshal(raw, &cr); err != nil {
		return "", 0, fmt.Errorf("/v1/chat/completions decode: %w", err)
	}
	if len(cr.Choices) == 0 {
		return "", 0, errors.New("/v1/chat/completions: no choices")
	}
	return cr.Choices[0].Message.Content, int64(cr.Usage.CompletionTokens), nil
}

var _ Generator = (*SGLangGenerator)(nil)
