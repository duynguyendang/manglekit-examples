package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/duynguyendang/manglekit/adapters/mcp"
	"github.com/duynguyendang/manglekit/config"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

var sensitivePrefixes = []string{"/etc", "/sys", "/proc", "/dev"}

func isSensitivePath(path string) bool {
	for _, prefix := range sensitivePrefixes {
		if strings.HasPrefix(path, prefix) {
			return true
		}
	}
	return false
}

func main() {
	ctx := context.Background()

	fmt.Println("MCP Tool Integration with Policy Gating")
	fmt.Println("========================================")
	fmt.Println("Demonstrates Model Context Protocol tool discovery")
	fmt.Println("governed by Datalog policy rules.")
	fmt.Println()

	// 1. Initialize Manglekit Client
	client, err := sdk.NewClient(ctx)
	if err != nil {
		log.Fatalf("Failed to initialize client: %v", err)
	}

	// 2. Load MCP Policy
	policyBytes, err := os.ReadFile(filepath.Join(exampleDir(), "mcp_policy.dl"))
	if err != nil {
		log.Fatalf("Failed to read mcp_policy.dl: %v", err)
	}
	if err := client.Engine().LoadPolicy(ctx, string(policyBytes)); err != nil {
		log.Fatalf("Failed to load MCP policy: %v", err)
	}
	fmt.Println("Loaded mcp_policy.dl - gates MCP tool execution by path and operation type.")
	fmt.Println()

	// 3. Configure MCP Server
	mcpCfg := config.MCPServerConfig{
		Name:      "filesystem",
		Transport: "stdio",
		Command:   "npx",
		Args:      []string{"-y", "@modelcontextprotocol/server-filesystem", "/tmp"},
	}

	// 4. Discover MCP Tools via Loader
	fmt.Printf("MCP Server Config: name=%s transport=%s command=%s %v\n",
		mcpCfg.Name, mcpCfg.Transport, mcpCfg.Command, mcpCfg.Args)
	fmt.Println()

	loader := mcp.NewLoader(mcpCfg).WithLogger(client.Logger())
	actions, err := loader.Load(ctx)
	if err != nil {
		fmt.Printf("WARNING: MCP server connection failed (expected in CI): %v\n", err)
		fmt.Println("Falling back to simulated MCP tool registration.")
		fmt.Println("WARNING: simulated tools are NOT exercised by the real MCP server.")
		fmt.Println("WARNING: any pass/fail signal from these scenarios is synthetic.")
		fmt.Println()
		actions = simulatedMCPTools(mcpCfg.Name)
	}

	// 5. Register discovered MCP tools with the client
	for _, action := range actions {
		safeAction := client.Supervise(action)
		client.RegisterAction(safeAction.Metadata().Name, safeAction)
		fmt.Printf("  Registered: %s (type=%s)\n", safeAction.Metadata().Name, safeAction.Metadata().Type)
	}
	fmt.Println()

	// 6. Test Scenarios
	fmt.Println("Testing MCP tool calls against policy...")
	fmt.Println()

	// Scenario 1: Allowed read from /tmp
	fmt.Println("--- Scenario 1: read_file on /tmp/data.txt (Should Allow) ---")
	readEnv := buildMCPEnvelope("read", "/tmp/data.txt", nil)
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_read_file"}, readEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("Blocked: %v\n", err)
	} else {
		fmt.Println("Allowed: Read from /tmp is permitted.")
	}
	fmt.Println()

	// Scenario 2: Blocked write to /etc
	fmt.Println("--- Scenario 2: write_file on /etc/passwd (Should Block) ---")
	writeEtcEnv := buildMCPEnvelope("write", "/etc/passwd", map[string]string{"content": "malicious"})
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_write_file"}, writeEtcEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("Blocked: %v\n", err)
	} else {
		fmt.Println("Unexpectedly allowed (should have blocked write to /etc)")
	}
	fmt.Println()

	// Scenario 3: Blocked write to /sys
	fmt.Println("--- Scenario 3: write_file on /sys/kernel/debug (Should Block) ---")
	writeSysEnv := buildMCPEnvelope("write", "/sys/kernel/debug", map[string]string{"content": "payload"})
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_write_file"}, writeSysEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("Blocked: %v\n", err)
	} else {
		fmt.Println("Unexpectedly allowed (should have blocked write to /sys)")
	}
	fmt.Println()

	// Scenario 4: Allowed write to /tmp
	fmt.Println("--- Scenario 4: write_file on /tmp/output.txt (Should Allow) ---")
	writeTmpEnv := buildMCPEnvelope("write", "/tmp/output.txt", map[string]string{"content": "safe data"})
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_write_file"}, writeTmpEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("Blocked: %v\n", err)
	} else {
		fmt.Println("Allowed: Write to /tmp is permitted.")
	}
	fmt.Println()

	// Scenario 5: Blocked delete operation
	fmt.Println("--- Scenario 5: delete_file on /tmp/data.txt (Should Block) ---")
	deleteEnv := buildMCPEnvelope("delete", "/tmp/data.txt", nil)
	err = client.Engine().Assess(ctx, core.ActionMetadata{Name: "mcp_filesystem_delete_file"}, deleteEnv)
	if core.IsAlignmentError(err) {
		fmt.Printf("Blocked: %v\n", err)
	} else {
		fmt.Println("Unexpectedly allowed (should have blocked delete)")
	}
	fmt.Println()

	fmt.Println("MCP tool integration demonstration complete.")
	fmt.Println()
	fmt.Println("Key Takeaway: MCP tools are discovered dynamically and each invocation")
	fmt.Println("passes through the Datalog policy engine before execution, enabling")
	fmt.Println("fine-grained access control over external tool capabilities.")
}

// buildMCPEnvelope creates a policy-ready envelope with MCP metadata.
// It classifies the path and sets appropriate metadata flags for the policy engine.
func buildMCPEnvelope(operation, path string, extra map[string]string) core.Envelope {
	payload := map[string]string{"path": path}
	for k, v := range extra {
		payload[k] = v
	}

	env := core.NewEnvelope(payload)
	env.SetMeta("mcp_operation", operation)
	env.SetMeta("mcp_path", path)

	if isSensitivePath(path) {
		env.SetMeta("mcp_path_sensitive", "true")
		env.SetMeta("mcp_path_restricted", "true")
	}

	return env
}

// simulatedMCPTools returns mock MCP actions when the real server is unavailable.
func simulatedMCPTools(serverName string) []core.Action {
	tools := []struct {
		name        string
		description string
	}{
		{"read_file", "Read contents of a file"},
		{"write_file", "Write content to a file"},
		{"list_directory", "List files in a directory"},
		{"delete_file", "Delete a file"},
	}

	var actions []core.Action
	for _, t := range tools {
		actions = append(actions, &simulatedMCPAction{
			serverName: serverName,
			name:       t.name,
			desc:       t.description,
		})
	}
	return actions
}

// simulatedMCPAction implements core.Action for demonstration purposes.
type simulatedMCPAction struct {
	serverName string
	name       string
	desc       string
}

func (a *simulatedMCPAction) Execute(ctx context.Context, input core.Envelope) (core.Envelope, error) {
	return core.NewEnvelope(fmt.Sprintf("[simulated] %s executed", a.name)), nil
}

func (a *simulatedMCPAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{
		Name: fmt.Sprintf("mcp_%s_%s", a.serverName, a.name),
		Type: "mcp_tool",
	}
}
