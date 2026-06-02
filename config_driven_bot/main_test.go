package main

import (
	"context"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/duynguyendang/manglekit"
)

func repoRoot() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filepath.Dir(filename))
}

func TestConfigDrivenBotPolicyLoads(t *testing.T) {
	ctx := context.Background()
	client, err := manglekit.NewClient(ctx, manglekit.WithBlueprintPath(filepath.Join(repoRoot(), "config_driven_bot/policy.dl")))
	if err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}
	if client == nil {
		t.Fatal("client should not be nil")
	}
}
