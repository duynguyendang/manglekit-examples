package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/config"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func TestConfigLoad(t *testing.T) {
	_, err := config.Load("app.yaml")
	if err != nil {
		t.Fatalf("config.Load error: %v", err)
	}
}

func TestGenericsDefine(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)

	runnable := sdk.Define[ClassifyRequest, ClassifyResponse](
		client,
		"test_classify",
		func(ctx context.Context, req ClassifyRequest) (ClassifyResponse, error) {
			return ClassifyResponse{Category: "test", Confidence: 1.0}, nil
		},
	)

	resp, err := runnable.Run(ctx, ClassifyRequest{Text: "hello"})
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}
	if resp.Category != "test" {
		t.Errorf("expected category=test, got %s", resp.Category)
	}
}

func TestProviderRegistry(t *testing.T) {
	ctx := context.Background()

	sdk.RegisterProvider("test_mock", func(opts map[string]any) (sdk.ClientOption, error) {
		return func(c *sdk.Client) error {
			c.RegisterAction("test_action", c.Supervise(&mockClassifyAction{}))
			return nil
		}, nil
	})

	provider, err := sdk.Provider("test_mock")
	if err != nil {
		t.Fatalf("Provider error: %v", err)
	}
	if provider == nil {
		t.Fatal("expected non-nil provider")
	}

	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)
}

// TestGateAllowAndDeny is the biz proof for "ship skills from YAML":
// the SAME config-registered action passes the happy path and is denied
// when its metadata trips the policy — allow AND deny in one test.
func TestGateAllowAndDeny(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Shutdown(ctx)

	policy := `
halt("Req", "secrets topics require human review", "T1") :-
    action_operation("Req", "test_classify"),
    meta("topic", "secrets").
`
	if err := client.LoadPolicy(ctx, policy); err != nil {
		t.Fatal(err)
	}

	runnable := sdk.Define[ClassifyRequest, ClassifyResponse](
		client, "test_classify",
		func(ctx context.Context, req ClassifyRequest) (ClassifyResponse, error) {
			return ClassifyResponse{Category: "general", Confidence: 0.9}, nil
		})

	// allow: ordinary typed call
	resp, err := runnable.Run(ctx, ClassifyRequest{Text: "hello world"})
	if err != nil || resp.Category != "general" {
		t.Fatalf("allow path: resp=%+v err=%v", resp, err)
	}

	// deny: same action, topic trips the gate BEFORE the handler runs
	_, err = client.ExecuteByName(ctx, "test_classify",
		ClassifyRequest{Text: "vault rotation"}, sdk.WithMetadata("topic", "secrets"))
	if err == nil {
		t.Fatal("expected policy deny for topic=secrets")
	}
	if !core.IsPolicyViolationError(err) {
		t.Fatalf("expected PolicyViolationError, got %v", err)
	}
}
