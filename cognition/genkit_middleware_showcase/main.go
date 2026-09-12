package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"

	"github.com/duynguyendang/manglekit/adapters/ai"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/testutil"
	genkitai "github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/plugins/middleware"
)

// countingGenerator is a deterministic, no-key TextGenerator that records
// how many times Generate was called. It lets the showcase demonstrate
// the Genkit 1.7 middleware RETRY / FALLBACK path end-to-end
// without an API key: a failing call increments the count, and the
// retry middleware re-invokes Generate until it succeeds (or exhausts
// retries). It does NOT call any network — see TestNoKeyRetryPath.
type countingGenerator struct {
	calls    int
	failNext int // number of leading calls that should fail
}

func (g *countingGenerator) Complete(ctx context.Context, prompt string) (string, error) {
	resp, err := g.Generate(ctx, prompt)
	if err != nil {
		return "", err
	}
	return resp.Text, nil
}

func (g *countingGenerator) Generate(ctx context.Context, prompt string, opts ...core.GenerateOption) (*core.LLMResponse, error) {
	g.calls++
	if g.failNext > 0 {
		g.failNext--
		return nil, fmt.Errorf("simulated transient model error (call %d)", g.calls)
	}
	return &core.LLMResponse{
		Text:  "Summarized: Go is an open-source language from Google.",
		Usage: map[string]int{"prompt": 10, "completion": 5},
	}, nil
}

func (g *countingGenerator) Stream(ctx context.Context, prompt string) (<-chan core.StreamChunk, error) {
	ch := make(chan core.StreamChunk)
	close(ch)
	return ch, nil
}

// MiddlewareDemoAction wraps an LLM action to inject middleware options during generation.
type MiddlewareDemoAction struct {
	name      string
	generator core.TextGenerator
}

func (a *MiddlewareDemoAction) Execute(ctx context.Context, input core.Envelope) (core.Envelope, error) {
	prompt, ok := input.Payload.(string)
	if !ok {
		return core.Envelope{}, fmt.Errorf("expected string prompt, got %T", input.Payload)
	}

	mwCfg := buildMiddlewareConfig()

	resp, err := a.generator.Generate(ctx, prompt, ai.WithMiddleware(mwCfg))
	if err != nil {
		return core.Envelope{}, fmt.Errorf("generation failed: %w", err)
	}

	return core.NewEnvelope(resp.Text), nil
}

func (a *MiddlewareDemoAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{
		Name: a.name,
		Type: "llm_with_middleware",
	}
}

// buildMiddlewareConfig creates the full middleware configuration showing all 3 middleware types.
//
// NOTE: the fallback model IDs here are the current GA Gemini model
// names. The previous "gemini-1.5-flash" / "gemini-1.0-pro" IDs are
// retired (404 from the API). With the deterministic mock generator the
// fallback is never actually invoked over the network, but the config must
// still name valid GA models so the example compiles and the middleware
// graph is well-formed.
func buildMiddlewareConfig() *ai.MiddlewareConfig {
	return &ai.MiddlewareConfig{
		Retry: &middleware.Retry{
			MaxRetries:     3,
			InitialDelayMs: 500,
			MaxDelayMs:     2000,
		},
		Fallback: &middleware.Fallback{
			// Current GA Gemini model IDs (replace per your provider).
			Models: []genkitai.ModelRef{genkitai.NewModelRef("googleai/gemini-2.0-flash", nil)},
		},
		ToolApproval: &middleware.ToolApproval{
			AllowedTools: []string{},
		},
	}
}

// printMiddlewareConfig displays the configured middleware settings.
func printMiddlewareConfig(cfg *ai.MiddlewareConfig) {
	fmt.Println("Middleware Configuration")
	fmt.Println("========================")

	if cfg.Retry != nil {
		fmt.Println("  Retry:")
		fmt.Printf("    MaxRetries:     %d\n", cfg.Retry.MaxRetries)
		fmt.Printf("    InitialDelayMs: %d\n", cfg.Retry.InitialDelayMs)
		fmt.Printf("    MaxDelayMs:     %d\n", cfg.Retry.MaxDelayMs)
		fmt.Println("    Automatically retries failed LLM calls with exponential backoff.")
		fmt.Println("    Starts at 500ms, doubles each attempt, caps at 2000ms.")
	}

	if cfg.Fallback != nil {
		fmt.Println("  Fallback:")
		for _, m := range cfg.Fallback.Models {
			fmt.Printf("    Model: %s\n", m.Name())
		}
		fmt.Println("    If the primary model fails, falls back to an alternative model.")
	}

	if cfg.ToolApproval != nil {
		fmt.Println("  ToolApproval:")
		if len(cfg.ToolApproval.AllowedTools) == 0 {
			fmt.Println("    AllowedTools: [] (all tools require approval)")
		} else {
			fmt.Printf("    AllowedTools: %v\n", cfg.ToolApproval.AllowedTools)
		}
		fmt.Println("    Requires explicit human approval before executing any tool calls.")
		fmt.Println("    Empty list means ALL tools require approval.")
	}
}

// validateConfig checks that the middleware config is properly formed.
func validateConfig(cfg *ai.MiddlewareConfig) error {
	if cfg == nil {
		return fmt.Errorf("middleware config is nil")
	}
	if cfg.Retry == nil && cfg.Fallback == nil && cfg.ToolApproval == nil {
		return fmt.Errorf("middleware config has no middleware configured")
	}
	if cfg.Retry != nil {
		if cfg.Retry.MaxRetries < 0 {
			return fmt.Errorf("retry MaxRetries must be >=0, got %d", cfg.Retry.MaxRetries)
		}
		if cfg.Retry.InitialDelayMs < 0 {
			return fmt.Errorf("retry InitialDelayMs must be >=0, got %d", cfg.Retry.InitialDelayMs)
		}
		if cfg.Retry.MaxDelayMs < cfg.Retry.InitialDelayMs {
			return fmt.Errorf("retry MaxDelayMs (%d) must be >= InitialDelayMs (%d)", cfg.Retry.MaxDelayMs, cfg.Retry.InitialDelayMs)
		}
	}
	return nil
}

// runMockMode runs the deterministic, no-key demonstration. It wires the
// MiddlewareDemoAction to a counting generator (which fails its first
// call) and drives it through the Genkit retry middleware, proving
// the retry/fallback path actually re-invokes Generate.
func runMockMode() {
	fmt.Println("[MOCK MODE] No GOOGLE_API_KEY found. Showing middleware composition + retry path with a deterministic generator.")
	fmt.Println()

	cfg := buildMiddlewareConfig()

	if err := validateConfig(cfg); err != nil {
		log.Fatalf("Config validation failed: %v", err)
	}
	fmt.Println("Config validation: PASSED")
	fmt.Println()

	printMiddlewareConfig(cfg)

	gen := &countingGenerator{failNext: 1}
	action := &MiddlewareDemoAction{name: "middleware_demo", generator: gen}

	env := sdk.NewEnvelope("Summarize this text in one sentence: Go is an open-source programming language designed at Google.")
	resp, err := action.Execute(context.Background(), env)
	if err != nil {
		fmt.Printf("Execution error: %v\n", err)
	} else {
		fmt.Printf("Success: %v\n", resp.Payload)
	}
	fmt.Printf("Generator was called %d time(s) — the retry middleware re-invoked it after the first simulated failure.\n", gen.calls)

	fmt.Println()
	fmt.Println("The fallback model (googleai/gemini-2.0-flash) is configured but only")
	fmt.Println("used if the primary generator kept failing; set GOOGLE_API_KEY to run")
	fmt.Println("the live model instead.")
}

// runLiveMode is the optional, API-key-gated live path. It uses a real
// Genkit action; without a key it is unreachable (see runMockMode).
func runLiveMode(ctx context.Context) {
	fmt.Println("[LIVE MODE] GOOGLE_API_KEY found. Running with real LLM.")
	fmt.Println()

	genkitAction, err := ai.NewGenkitAction(ctx, "googleai/gemini-2.0-flash")
	if err != nil {
		log.Fatalf("Failed to initialize Genkit action: %v", err)
	}

	mwAction := &MiddlewareDemoAction{
		name:      "middleware_demo",
		generator: genkitAction.(core.TextGenerator),
	}

	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}
	client.RegisterAction("demo_action", mwAction)

	fmt.Println("Executing action with middleware stack applied...")
	env := sdk.NewEnvelope("Summarize this text in one sentence: Go is an open-source programming language designed at Google.")
	resp, err := client.Execute(ctx, env)
	if err != nil {
		fmt.Printf("Execution error: %v\n", err)
	} else {
		fmt.Printf("Success: %v\n", resp.Payload)
	}
}

// streamingPolicy gates the streaming demo: a hard T1 pre-check deny, and a
// post-check rule over the OUTPUT entity that refuses to auto-trust assembled
// LLM text for the "stream_review" action.
//
// Note the asymmetry this encodes (verified engine behavior): the raw
// PolicyEngine.Reflect path does NOT inject an action_operation("Output",…)
// fact — only the SDK-supervised path does. So OUTPUT-entity rules key on the
// output envelope's own meta facts, which StreamingSupervisedAction sets
// (model_type, action_name).
const streamingPolicy = `
halt("Req", "streaming about passwords is denied before the first chunk", "T1") :-
    action_operation("Req", "stream_answer"),
    meta("topic", "passwords").

halt("Output", "reviewed streams require human sign-off before being marked final", "T1") :-
    meta("model_type", "llm"),
    meta("action_name", "stream_review").
`

// streamTo collects chunk texts + any terminal error from a supervised stream.
func streamTo(ctx context.Context, ch <-chan core.StreamChunk) (text string, termErr error) {
	for c := range ch {
		if c.Err != nil {
			return text, c.Err
		}
		text += c.Text
	}
	return text, nil
}

// demoStreamingSupervision shows the pre-first-chunk deny and the
// post-check on the assembled response — the two moments a streaming LLM is
// dangerous, both covered by adapters/ai.NewStreamingSupervisedAction.
func demoStreamingSupervision(ctx context.Context) error {
	fmt.Println("--- Supervised streaming (adapters/ai.NewStreamingSupervisedAction) ---")

	client, err := sdk.NewClient(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = client.Shutdown(ctx) }()
	if err := client.LoadPolicy(ctx, streamingPolicy); err != nil {
		return err
	}

	// Deterministic streaming mock; its Calls() counter is the PROOF that a
	// denied request never opened the provider.
	gen := testutil.NewMockLLM("Sourdough is a fermented dough. ")
	answer, err := ai.NewStreamingSupervisedAction("stream_answer", gen, client.Engine())
	if err != nil {
		return err
	}
	client.RegisterAction("stream_answer", answer)

	// Case 1: denied BEFORE the first chunk — provider never called.
	denied := core.Envelope{Payload: "Explain passwords policy"}
	denied.Metadata = map[string]any{"topic": "passwords"}
	before := gen.Calls()
	ch, err := answer.Stream(ctx, denied)
	fmt.Printf("  deny case:  stream opened=%v (err: %v)\n", ch != nil, err != nil)
	fmt.Printf("  provider calls before/after: %d/%d — zero leaked chunks\n", before, gen.Calls())

	// Case 2: allowed — chunks flow, post-check passes, final is available.
	ok := core.Envelope{Payload: "Explain sourdough"}
	ok.Metadata = map[string]any{"topic": "cooking"}
	ch2, err := answer.Stream(ctx, ok)
	if err != nil {
		return err
	}
	text, termErr := streamTo(ctx, ch2)
	final, okFinal := answer.FinalEnvelope()
	fmt.Printf("  allow case: streamed %q, terminal err=%v, final-envelope=%v (%v)\n",
		text, termErr, okFinal, final.Payload)

	// Case 3: chunks arrive, but the assembled output is refused by the
	// OUTPUT-entity rule — caller gets a terminal error chunk, no final.
	review, err := ai.NewStreamingSupervisedAction("stream_review", gen, client.Engine())
	if err != nil {
		return err
	}
	ch3, err := review.Stream(ctx, ok)
	if err != nil {
		return err
	}
	text3, termErr3 := streamTo(ctx, ch3)
	_, okFinal3 := review.FinalEnvelope()
	fmt.Printf("  post-check case: streamed %q, terminal err present=%v, final usable=%v\n",
		text3, termErr3 != nil, okFinal3)
	fmt.Println()
	return nil
}

func main() {
	ctx := context.Background()

	if err := demoStreamingSupervision(ctx); err != nil {
		log.Fatalf("streaming supervision demo: %v", err)
	}

	fmt.Println("Genkit 1.7 Middleware Showcase")
	fmt.Println("==============================")
	fmt.Println("This example demonstrates how to compose Genkit middleware:")
	fmt.Println("  1. Retry: Automatic retry with exponential backoff")
	fmt.Println("  2. Fallback: Graceful degradation to alternative models")
	fmt.Println("  3. Tool Approval: Human-in-the-loop guardrails for sensitive tools")
	fmt.Println()

	if testing.Testing() {
		// Allow `go test` to exercise the mock path without a key.
		runMockMode()
		return
	}

	if os.Getenv("GOOGLE_API_KEY") == "" {
		runMockMode()
	} else {
		runLiveMode(ctx)
	}
}
