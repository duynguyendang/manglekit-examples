// config_driven_app demonstrates declarative YAML configuration,
// the generics API (Define[In,Out]), and provider registry.
//
// Features shown:
//   - config.Load() parses YAML with env var expansion
//   - sdk.WithConfig() hydrates client from config
//   - sdk.Define[In,Out]() registers type-safe supervised actions
//   - sdk.RegisterProvider() registers a custom provider factory
//
// No API key required (mock provider).

package main

import (
	"context"
	"fmt"
	"log"
	"strings"

	"github.com/duynguyendang/manglekit/config"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// ClassifyRequest is the typed input for our action.
type ClassifyRequest struct {
	Text string `mangle:"text"`
}

// ClassifyResponse is the typed output.
type ClassifyResponse struct {
	Category   string  `mangle:"category"`
	Confidence float64 `mangle:"confidence"`
}

func main() {
	ctx := context.Background()

	// =====================================================================
	// 1. Load YAML config
	// =====================================================================
	fmt.Println("=== Config-Driven App ===")
	fmt.Println()

	cfg, err := config.Load("config_driven_app/app.yaml")
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("  Observability.Enabled: %v\n", cfg.Observability.Enabled)
	fmt.Printf("  Actions: %d defined\n", len(cfg.Actions))
	fmt.Println()

	// =====================================================================
	// 2. Register a mock provider factory
	// =====================================================================
	sdk.RegisterProvider("mock", func(opts map[string]any) (sdk.ClientOption, error) {
		name := "mock_action"
		if n, ok := opts["_action_name"].(string); ok {
			name = n
		}
		fmt.Printf("  [provider] Mock factory called for action: %s\n", name)

		// Return a ClientOption that registers a mock action
		return func(c *sdk.Client) error {
			mockAction := &mockClassifyAction{}
			c.RegisterAction(name, c.Supervise(mockAction))
			return nil
		}, nil
	})

	// =====================================================================
	// 3. Create client with config
	// =====================================================================
	client, err := sdk.NewClient(ctx, sdk.WithConfig(cfg))
	if err != nil {
		log.Fatal(err)
	}
	defer client.Shutdown(ctx)

	// =====================================================================
	// 4. Use generics API: Define[In, Out]
	// =====================================================================
	fmt.Println("--- Generics API: Define[In, Out] ---")
	fmt.Println()

	runnable := sdk.Define[ClassifyRequest, ClassifyResponse](
		client,
		"classify_manual",
		func(ctx context.Context, req ClassifyRequest) (ClassifyResponse, error) {
			category := "general"
			confidence := 0.5
			lower := strings.ToLower(req.Text)
			switch {
			case strings.Contains(lower, "error") || strings.Contains(lower, "fail") || strings.Contains(lower, "crash"):
				category = "incident"
				confidence = 0.9
			case strings.Contains(lower, "deploy") || strings.Contains(lower, "release"):
				category = "deployment"
				confidence = 0.8
			case strings.Contains(lower, "security") || strings.Contains(lower, "vulnerability"):
				category = "security"
				confidence = 0.85
			}
			return ClassifyResponse{Category: category, Confidence: confidence}, nil
		},
	)

	inputs := []string{
		"The server crashed with an error",
		"Deploy v2.1 to production",
		"Security vulnerability found in dependency",
		"Regular maintenance scheduled",
	}

	for _, text := range inputs {
		resp, err := runnable.Run(ctx, ClassifyRequest{Text: text})
		if err != nil {
			fmt.Printf("  %q → error: %v\n", text, err)
			continue
		}
		fmt.Printf("  %q → category=%s confidence=%.1f\n", text, resp.Category, resp.Confidence)
	}

	fmt.Println()
	fmt.Println("--- Config-Driven Actions ---")
	fmt.Println()

	// The config hydrated "classify_text" action via the mock provider
	env := core.NewEnvelope("deploy the hotfix now")
	env.Facts = append(env.Facts, `action_operation("Req", "classify_text").`)

	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "classify_text"}, env)
	if err != nil {
		fmt.Printf("  classify_text Assess: blocked (%v)\n", err)
	} else {
		fmt.Println("  classify_text Assess: allowed (no policy blocking)")
	}

	fmt.Println()
	fmt.Println("Done.")
}

// mockClassifyAction is a simple mock implementing core.Action.
type mockClassifyAction struct{}

func (a *mockClassifyAction) Execute(_ context.Context, env core.Envelope) (core.Envelope, error) {
	return core.NewEnvelope("mock classified"), nil
}

func (a *mockClassifyAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "classify_text", Type: "mock"}
}
