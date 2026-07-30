package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/config"
	"github.com/duynguyendang/manglekit/sdk"
)

func TestConfigLoad(t *testing.T) {
	cfg, err := config.Load("app.yaml")
	if err != nil {
		t.Fatalf("config.Load error: %v", err)
	}
	if cfg.FailureMode != "closed" {
		t.Errorf("expected failure_mode=closed, got %s", cfg.FailureMode)
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
