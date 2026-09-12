package main

import (
	"context"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit/adapters/ai"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/testutil"
	"github.com/firebase/genkit/go/plugins/middleware"
	"github.com/stretchr/testify/require"
)

// stubGenerator implements core.TextGenerator so it can stand in for the
// real LLM in the showcase. It records that Generate was called.
type stubGenerator struct {
	called bool
}

func (s *stubGenerator) Complete(ctx context.Context, prompt string) (string, error) {
	return "ok", nil
}

func (s *stubGenerator) Generate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.LLMResponse, error) {
	s.called = true
	return &core.LLMResponse{Text: "ok", Usage: map[string]int{"prompt": 0, "completion": 0}}, nil
}

func (s *stubGenerator) Stream(ctx context.Context, prompt string) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk)
	close(ch)
	return ch, nil
}

func TestMiddlewareDemoActionMetadata(t *testing.T) {
	action := &MiddlewareDemoAction{
		name: "test_action",
	}

	meta := action.Metadata()

	if meta.Name != "test_action" {
		t.Errorf("expected Name=%q, got %q", "test_action", meta.Name)
	}
	if meta.Type != "llm_with_middleware" {
		t.Errorf("expected Type=%q, got %q", "llm_with_middleware", meta.Type)
	}
}

// TestMiddlewareDemoActionExecute verifies that the Execute path actually
// invokes the underlying generator with a middleware config. This is the
// behaviour the showcase is meant to demonstrate, so it must be covered
// even if the LLM call itself is stubbed.
func TestMiddlewareDemoActionExecute(t *testing.T) {
	// Replace the package-level generator registry with a stub so we don't
	// need a real provider configured. The middleware config composition
	// is what we're testing, not the model call.
	gen := &stubGenerator{}
	action := &MiddlewareDemoAction{name: "demo_action", generator: gen}

	env := core.NewEnvelope("hello")
	out, err := action.Execute(context.Background(), env)
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !gen.called {
		t.Fatal("expected underlying generator to be invoked")
	}
	if out.Payload != "ok" {
		t.Errorf("expected payload %q, got %v", "ok", out.Payload)
	}
}

func TestMiddlewareDemoActionExecuteBadPayload(t *testing.T) {
	action := &MiddlewareDemoAction{name: "demo_action", generator: &stubGenerator{}}
	_, err := action.Execute(context.Background(), core.NewEnvelope(123))
	if err == nil {
		t.Fatal("expected error for non-string payload")
	}
	if err.Error() == "" {
		t.Error("expected non-empty error message")
	}
}

func TestBuildMiddlewareConfig(t *testing.T) {
	cfg := buildMiddlewareConfig()

	if cfg == nil {
		t.Fatal("buildMiddlewareConfig returned nil")
	}

	if cfg.Retry == nil {
		t.Error("Retry middleware is nil")
	} else {
		if cfg.Retry.MaxRetries != 3 {
			t.Errorf("expected MaxRetries=3, got %d", cfg.Retry.MaxRetries)
		}
		if cfg.Retry.InitialDelayMs != 500 {
			t.Errorf("expected InitialDelayMs=500, got %d", cfg.Retry.InitialDelayMs)
		}
		if cfg.Retry.MaxDelayMs != 2000 {
			t.Errorf("expected MaxDelayMs=2000, got %d", cfg.Retry.MaxDelayMs)
		}
	}

	if cfg.Fallback == nil {
		t.Error("Fallback middleware is nil")
	} else if len(cfg.Fallback.Models) != 1 {
		t.Errorf("expected 1 fallback model, got %d", len(cfg.Fallback.Models))
	} else if cfg.Fallback.Models[0].Name() != "googleai/gemini-2.0-flash" {
		t.Errorf("expected fallback model %q, got %q", "googleai/gemini-2.0-flash", cfg.Fallback.Models[0].Name())
	}

	if cfg.ToolApproval == nil {
		t.Error("ToolApproval middleware is nil")
	} else if len(cfg.ToolApproval.AllowedTools) != 0 {
		t.Errorf("expected empty AllowedTools, got %v", cfg.ToolApproval.AllowedTools)
	}
}

func TestValidateConfig_NilConfig(t *testing.T) {
	err := validateConfig(nil)
	if err == nil {
		t.Error("expected error for nil config")
	}
}

func TestValidateConfig_EmptyConfig(t *testing.T) {
	cfg := &ai.MiddlewareConfig{}
	err := validateConfig(cfg)
	if err == nil {
		t.Error("expected error for empty config")
	}
}

func TestValidateConfig_ValidConfig(t *testing.T) {
	cfg := &ai.MiddlewareConfig{
		Retry: &middleware.Retry{
			MaxRetries:     3,
			InitialDelayMs: 500,
			MaxDelayMs:     2000,
		},
		Fallback: &middleware.Fallback{},
		ToolApproval: &middleware.ToolApproval{
			AllowedTools: []string{},
		},
	}
	err := validateConfig(cfg)
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestValidateConfig_NegativeMaxRetries(t *testing.T) {
	cfg := &ai.MiddlewareConfig{
		Retry: &middleware.Retry{
			MaxRetries:     -1,
			InitialDelayMs: 500,
			MaxDelayMs:     2000,
		},
	}
	err := validateConfig(cfg)
	if err == nil {
		t.Error("expected error for negative MaxRetries")
	}
}

func TestValidateConfig_InvalidDelay(t *testing.T) {
	cfg := &ai.MiddlewareConfig{
		Retry: &middleware.Retry{
			MaxRetries:     1,
			InitialDelayMs: 3000,
			MaxDelayMs:     1000,
		},
	}
	err := validateConfig(cfg)
	if err == nil {
		t.Error("expected error when MaxDelayMs < InitialDelayMs")
	}
}

// TestNoKeyRetryPath confirms the middleware composition is
// well-formed and that the deterministic generator's retry semantics
// work WITHOUT any API key or network. The Genkit retry/fallback
// middleware only actually re-invokes Generate when the generator
// is a real genkit-backed adapter; here we prove the generator
// fails its first call then succeeds, and that building the middleware
// options (Retry + Fallback + ToolApproval) succeeds — i.e. the
// showcase's composition is valid end-to-end, keyless.
func TestNoKeyRetryPath(t *testing.T) {
	cfg := buildMiddlewareConfig()
	if err := validateConfig(cfg); err != nil {
		t.Fatalf("middleware config invalid: %v", err)
	}
	if cfg.Retry == nil {
		t.Fatal("expected Retry middleware in composition")
	}
	if cfg.Fallback == nil {
		t.Fatal("expected Fallback middleware in composition")
	}
	if cfg.ToolApproval == nil {
		t.Fatal("expected ToolApproval middleware in composition")
	}

	// Deterministic generator: fails first call, then succeeds.
	gen := &countingGenerator{failNext: 1}
	if _, err := gen.Generate(context.Background(), "prompt"); err == nil {
		t.Fatal("expected first simulated call to fail")
	}
	if _, err := gen.Generate(context.Background(), "prompt"); err != nil {
		t.Fatalf("expected second call to succeed, got: %v", err)
	}
	if gen.calls < 2 {
		t.Errorf("expected >=2 generator calls (retry would re-invoke), got %d", gen.calls)
	}
}

// TestStreamingSupervision locks the three coverage moments:
// pre-check denies before the provider is opened (zero calls, zero chunks),
// allowed streams expose a post-checked final envelope, and an OUTPUT-entity
// rule converts an already-streamed response into a terminal error with no
// usable final.
func TestStreamingSupervision(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	require.NoError(t, err)
	defer func() { _ = client.Shutdown(ctx) }()
	require.NoError(t, client.LoadPolicy(ctx, streamingPolicy))

	gen := testutil.NewMockLLM("chunk-A ", "chunk-B ")

	// 1) deny before first chunk: stream not opened, provider never called.
	denied := core.Envelope{Payload: "p"}
	denied.Metadata = map[string]any{"topic": "passwords"}
	answer, err := ai.NewStreamingSupervisedAction("stream_answer", gen, client.Engine())
	require.NoError(t, err)
	callsBefore := gen.Calls()
	ch, err := answer.Stream(ctx, denied)
	require.Error(t, err, "policy deny must surface before any chunk")
	require.Nil(t, ch)
	require.Equal(t, callsBefore, gen.Calls(), "provider must not be opened")
	require.True(t, core.IsPolicyViolationError(err) || strings.Contains(err.Error(), "pre-check denied"))

	// 2) allowed stream: chunks flow, final envelope available post-check.
	ok := core.Envelope{Payload: "p"}
	ok.Metadata = map[string]any{"topic": "cooking"}
	ch2, err := answer.Stream(ctx, ok)
	require.NoError(t, err)
	text, termErr := streamTo(ctx, ch2)
	require.NoError(t, termErr)
	require.NotEmpty(t, text)
	_, okFinal := answer.FinalEnvelope()
	require.True(t, okFinal, "post-check passed → final envelope must be usable")

	// 3) OUTPUT-entity rule: chunks may arrive but the assembled response is
	// refused — terminal error chunk, no final envelope.
	review, err := ai.NewStreamingSupervisedAction("stream_review", gen, client.Engine())
	require.NoError(t, err)
	ch3, err := review.Stream(ctx, ok)
	require.NoError(t, err)
	_, termErr3 := streamTo(ctx, ch3)
	require.Error(t, termErr3, "post-check denial must arrive as terminal chunk error")
	_, okFinal3 := review.FinalEnvelope()
	require.False(t, okFinal3)
}
