package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync/atomic"
	"testing"

	"github.com/duynguyendang/manglekit/adapters/mcp"
	"github.com/duynguyendang/manglekit/config"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func testClient(t *testing.T) *sdk.Client {
	t.Helper()
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatalf("Failed to create client: %v", err)
	}
	_, filename, _, _ := runtime.Caller(0)
	policyPath := filepath.Join(filepath.Dir(filename), "mcp_policy.dl")
	policyBytes, err := os.ReadFile(policyPath)
	if err != nil {
		t.Fatalf("Failed to read policy: %v", err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyBytes)); err != nil {
		t.Fatalf("Failed to load policy: %v", err)
	}
	return client
}

func TestMCPLoaderConfig(t *testing.T) {
	cfg := config.MCPServerConfig{
		Name:      "filesystem",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
	}

	if cfg.Name != "filesystem" {
		t.Fatalf("Expected name 'filesystem', got %q", cfg.Name)
	}
	if cfg.Transport != "stdio" {
		t.Fatalf("Expected transport 'stdio', got %q", cfg.Transport)
	}
	if cfg.Command != "npx" {
		t.Fatalf("Expected command 'npx', got %q", cfg.Command)
	}
	if len(cfg.Args) != 3 {
		t.Fatalf("Expected 3 args, got %d", len(cfg.Args))
	}

	loader := mcp.NewLoader(cfg)
	if loader == nil {
		t.Fatal("NewLoader returned nil")
	}
}

func TestMCPLoaderConfig_SSE(t *testing.T) {
	cfg := config.MCPServerConfig{
		Name:      "remote-tools",
		Transport: "sse",
		Command:   "https://mcp.example.com/sse",
	}

	if cfg.Transport != "sse" {
		t.Fatalf("Expected transport 'sse', got %q", cfg.Transport)
	}

	loader := mcp.NewLoader(cfg)
	if loader == nil {
		t.Fatal("NewLoader returned nil")
	}
}

func TestMCPLoaderConfig_FailOnStartup(t *testing.T) {
	cfg := config.MCPServerConfig{
		Name:          "critical-server",
		Transport:     "stdio",
		Command:       "missing-binary",
		FailOnStartup: true,
		Tools:         []string{"tool_a", "tool_b"},
	}

	if !cfg.FailOnStartup {
		t.Fatal("Expected FailOnStartup to be true")
	}
	if len(cfg.Tools) != 2 {
		t.Fatalf("Expected 2 expected tools, got %d", len(cfg.Tools))
	}
}

// TestPolicyGating_AllowRead exercises the policy directly via
// Engine().Assess. This is a pure-policy check that BYPASSES the supervisor
// (Zero-Trust Gatekeeper), so it never exercises the real supervised contract.
func TestPolicyGating_AllowRead(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	env := buildMCPEnvelope("read", "/tmp/data.txt", nil)
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_read_file"}, env)
	if core.IsAlignmentError(err) {
		t.Fatalf("Expected read to /tmp to be allowed, got blocked: %v", err)
	}
}

// TestPolicyGating_BlockWriteToEtc exercises the policy directly via
// Engine().Assess. This is a pure-policy check that BYPASSES the supervisor.
func TestPolicyGating_BlockWriteToEtc(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	env := buildMCPEnvelope("write", "/etc/passwd", map[string]string{"content": "bad"})
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_write_file"}, env)
	if !core.IsAlignmentError(err) {
		t.Fatal("Expected write to /etc to be blocked, but it was allowed")
	}
}

// TestPolicyGating_BlockWriteToSys exercises the policy directly via
// Engine().Assess. This is a pure-policy check that BYPASSES the supervisor.
func TestPolicyGating_BlockWriteToSys(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	env := buildMCPEnvelope("write", "/sys/kernel/debug", map[string]string{"content": "payload"})
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_write_file"}, env)
	if !core.IsAlignmentError(err) {
		t.Fatal("Expected write to /sys to be blocked, but it was allowed")
	}
}

// TestPolicyGating_AllowWriteToTmp exercises the policy directly via
// Engine().Assess. This is a pure-policy check that BYPASSES the supervisor.
func TestPolicyGating_AllowWriteToTmp(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	env := buildMCPEnvelope("write", "/tmp/output.txt", map[string]string{"content": "safe"})
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_write_file"}, env)
	if core.IsAlignmentError(err) {
		t.Fatalf("Expected write to /tmp to be allowed, got blocked: %v", err)
	}
}

// TestPolicyGating_BlockDelete exercises the policy directly via
// Engine().Assess. This is a pure-policy check that BYPASSES the supervisor.
func TestPolicyGating_BlockDelete(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	env := buildMCPEnvelope("delete", "/tmp/data.txt", nil)
	err := client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_delete_file"}, env)
	if !core.IsAlignmentError(err) {
		t.Fatal("Expected delete to be blocked, but it was allowed")
	}
}

func TestPolicyGating_SimulatedAction(t *testing.T) {
	action := &simulatedMCPAction{
		serverName: "filesystem",
		name:       "read_file",
		desc:       "Read a file",
	}

	meta := action.Metadata()
	if meta.Name != "mcp_filesystem_read_file" {
		t.Fatalf("Expected name 'mcp_filesystem_read_file', got %q", meta.Name)
	}
	if meta.Type != "mcp_tool" {
		t.Fatalf("Expected type 'mcp_tool', got %q", meta.Type)
	}

	ctx := context.Background()
	env := core.NewEnvelope(map[string]string{"path": "/tmp/test.txt"})
	result, err := action.Execute(ctx, env)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}
	if result.Payload == nil {
		t.Fatal("Expected non-nil payload from simulated action")
	}
}

func TestPolicyGating_WithRegisteredAction(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	action := &simulatedMCPAction{
		serverName: "filesystem",
		name:       "read_file",
	}
	safeAction := client.Supervise(action)
	client.RegisterAction(safeAction.Metadata().Name, safeAction)

	res, err := client.ExecuteByName(ctx, "mcp_filesystem_read_file", map[string]string{"path": "/tmp/hello.txt"})
	if err != nil {
		t.Fatalf("ExecuteByName failed: %v", err)
	}
	if res.Payload == nil {
		t.Fatal("Expected non-nil result from registered MCP action")
	}
	fmt.Printf("Registered action result: %v\n", res.Payload)
}

// TestSupervisedGating_BlockWriteToEtc pins the REAL supervised contract:
// the action is wrapped by Supervise (Zero-Trust Gatekeeper) and driven via
// ExecuteByName, so the gate actually executes. A write to a sensitive path
// must be blocked by the pre-check and the inner action must NOT run.
func TestSupervisedGating_BlockWriteToEtc(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	action := &simulatedMCPAction{
		serverName: "filesystem",
		name:       "write_file",
	}
	safeAction := client.Supervise(action)
	client.RegisterAction(safeAction.Metadata().Name, safeAction)

	res, err := client.ExecuteByName(ctx, "mcp_filesystem_write_file",
		map[string]string{"path": "/etc/passwd"},
		sdk.WithMetadata("mcp_operation", "write"),
		sdk.WithMetadata("mcp_path", "/etc/passwd"),
		sdk.WithMetadata("mcp_path_sensitive", "true"),
		sdk.WithMetadata("mcp_path_restricted", "true"),
	)
	executed := atomic.LoadInt32(&action.execCount) > 0

	if !core.IsPolicyViolationError(err) {
		t.Fatalf("Expected policy violation on write to /etc, got err=%v", err)
	}
	if executed {
		t.Fatal("Inner action executed despite a pre-check halt (gate is ineffective)")
	}
	_ = res
}

// TestSupervisedGating_AllowRead pins the REAL supervised contract: a read from
// /tmp is permitted by the gate and the supervised inner action must run.
func TestSupervisedGating_AllowRead(t *testing.T) {
	client := testClient(t)
	ctx := context.Background()

	action := &simulatedMCPAction{
		serverName: "filesystem",
		name:       "read_file",
	}
	safeAction := client.Supervise(action)
	client.RegisterAction(safeAction.Metadata().Name, safeAction)

	res, err := client.ExecuteByName(ctx, "mcp_filesystem_read_file",
		map[string]string{"path": "/tmp/data.txt"},
		sdk.WithMetadata("mcp_operation", "read"),
		sdk.WithMetadata("mcp_path", "/tmp/data.txt"),
	)
	if err != nil {
		t.Fatalf("Expected read from /tmp to be allowed, got err=%v", err)
	}
	if atomic.LoadInt32(&action.execCount) == 0 {
		t.Fatal("Expected inner action to execute on an allowed supervised call")
	}
	if res.Payload == nil {
		t.Fatal("Expected non-nil result from registered MCP action")
	}
}
