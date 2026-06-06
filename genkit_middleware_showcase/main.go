package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/duynguyendang/manglekit/adapters/ai"
	"github.com/duynguyendang/manglekit/core"
	_ "github.com/duynguyendang/manglekit/providers/google"
	"github.com/duynguyendang/manglekit/sdk"
	genkitai "github.com/firebase/genkit/go/ai"
	"github.com/firebase/genkit/go/plugins/middleware"
)

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
func buildMiddlewareConfig() *ai.MiddlewareConfig {
	return &ai.MiddlewareConfig{
		Retry: &middleware.Retry{
			MaxRetries:     3,
			InitialDelayMs: 500,
			MaxDelayMs:     2000,
		},
		Fallback: &middleware.Fallback{
			Models: []genkitai.ModelRef{genkitai.NewModelRef("google/gemini-1.0-pro", nil)},
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
			return fmt.Errorf("retry MaxRetries must be >= 0, got %d", cfg.Retry.MaxRetries)
		}
		if cfg.Retry.InitialDelayMs < 0 {
			return fmt.Errorf("retry InitialDelayMs must be >= 0, got %d", cfg.Retry.InitialDelayMs)
		}
		if cfg.Retry.MaxDelayMs < cfg.Retry.InitialDelayMs {
			return fmt.Errorf("retry MaxDelayMs (%d) must be >= InitialDelayMs (%d)", cfg.Retry.MaxDelayMs, cfg.Retry.InitialDelayMs)
		}
	}
	return nil
}

func runMockMode() {
	fmt.Println("[MOCK MODE] No GOOGLE_API_KEY found. Showing middleware configuration only.")
	fmt.Println()

	cfg := buildMiddlewareConfig()

	if err := validateConfig(cfg); err != nil {
		log.Fatalf("Config validation failed: %v", err)
	}
	fmt.Println("Config validation: PASSED")
	fmt.Println()

	printMiddlewareConfig(cfg)

	fmt.Println()
	fmt.Println("To see this example run with a real LLM, set GOOGLE_API_KEY and re-run.")
}

func runRealMode(ctx context.Context) {
	fmt.Println("[LIVE MODE] GOOGLE_API_KEY found. Running with real LLM.")
	fmt.Println()

	genkitAction, err := ai.NewGenkitAction(ctx, "google/gemini-1.5-flash")
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

func main() {
	ctx := context.Background()

	fmt.Println("Genkit 1.7 Middleware Showcase")
	fmt.Println("==============================")
	fmt.Println("This example demonstrates how to compose Genkit middleware:")
	fmt.Println("  1. Retry: Automatic retry with exponential backoff")
	fmt.Println("  2. Fallback: Graceful degradation to alternative models")
	fmt.Println("  3. Tool Approval: Human-in-the-loop guardrails for sensitive tools")
	fmt.Println()

	if os.Getenv("GOOGLE_API_KEY") == "" {
		runMockMode()
	} else {
		runRealMode(ctx)
	}
}
