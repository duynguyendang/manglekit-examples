package main

import (
	"testing"

	"github.com/duynguyendang/manglekit/adapters/ai"
	"github.com/firebase/genkit/go/plugins/middleware"
)

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
	} else if cfg.Fallback.Models[0].Name() != "google/gemini-1.0-pro" {
		t.Errorf("expected fallback model %q, got %q", "google/gemini-1.0-pro", cfg.Fallback.Models[0].Name())
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
