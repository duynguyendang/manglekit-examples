package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

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

	// 5. Discover MCP tools. If the real MCP server is unavailable (expected in
	// CI), fall back to simulated tools so the gating contract is still exercised.
	loader := mcp.NewLoader(mcpCfg).WithLogger(client.Logger())
	realActions, loadErr := loader.Load(ctx)
	if loadErr != nil {
		fmt.Printf("WARNING: MCP server connection failed (expected in CI): %v\n", loadErr)
		fmt.Println("Falling back to simulated MCP tool registration.")
		fmt.Println("WARNING: simulated tools are NOT exercised by the real MCP server.")
		fmt.Println("WARNING: any pass/fail signal from these scenarios is synthetic.")
		fmt.Println()
	} else {
		// Register the discovered real tools for completeness; the gating
		// scenarios below are driven by the canonical simulated tools instead,
		// so they never perform real filesystem I/O.
		for _, action := range realActions {
			safeAction := client.Supervise(action)
			client.RegisterAction(safeAction.Metadata().Name, safeAction)
			fmt.Printf("  Discovered: %s (type=%s)\n", safeAction.Metadata().Name, safeAction.Metadata().Type)
		}
	}

	// 6. Register the canonical supervised MCP tools used by the gating
	// scenarios. Governed actions now run via Supervise+ExecuteByName (the
	// zero-trust gate); Engine().Assess bypasses the supervisor, so it is no
	// longer used for the gate. These simulated actions are always registered
	// (real server up or down) so the gate contract is deterministic and the
	// inner-action execution can be recorded/asserted.
	//
	// NOTE: WithFailMode is a NO-OP for the gate (P0.3 regression), so it is
	// intentionally not relied upon to change block behavior here.
	simActions := map[string]*simulatedMCPAction{}
	for _, action := range simulatedMCPTools(mcpCfg.Name) {
		sa := action.(*simulatedMCPAction)
		safeAction := client.Supervise(sa)
		client.RegisterAction(safeAction.Metadata().Name, safeAction)
		simActions[safeAction.Metadata().Name] = sa
		fmt.Printf("  Registered: %s (type=%s)\n", safeAction.Metadata().Name, safeAction.Metadata().Type)
	}
	fmt.Println()

	// 7. Test Scenarios. Each scenario drives the SUPERVISED registered action
	// via ExecuteByName, so the gate actually executes (pre-check). The
	// buildMCPEnvelope classification is converted into metadata forwarded by
	// sdk.WithMetadata; policy facts must come from metadata on this path.
	fmt.Println("Testing MCP tool calls against policy...")
	fmt.Println()

	// Scenario 1: Allowed read from /tmp
	fmt.Println("--- Scenario 1: read_file on /tmp/data.txt (Should Allow) ---")
	runScenario(ctx, client, simActions, "mcp_filesystem_read_file", "read", "/tmp/data.txt", true)
	fmt.Println()

	// Scenario 2: Blocked write to /etc
	fmt.Println("--- Scenario 2: write_file on /etc/passwd (Should Block) ---")
	runScenario(ctx, client, simActions, "mcp_filesystem_write_file", "write", "/etc/passwd", false)
	fmt.Println()

	// Scenario 3: Blocked write to /sys
	fmt.Println("--- Scenario 3: write_file on /sys/kernel/debug (Should Block) ---")
	runScenario(ctx, client, simActions, "mcp_filesystem_write_file", "write", "/sys/kernel/debug", false)
	fmt.Println()

	// Scenario 4: Allowed write to /tmp
	fmt.Println("--- Scenario 4: write_file on /tmp/output.txt (Should Allow) ---")
	runScenario(ctx, client, simActions, "mcp_filesystem_write_file", "write", "/tmp/output.txt", true)
	fmt.Println()

	// Scenario 5: Blocked delete operation
	fmt.Println("--- Scenario 5: delete_file on /tmp/data.txt (Should Block) ---")
	runScenario(ctx, client, simActions, "mcp_filesystem_delete_file", "delete", "/tmp/data.txt", false)
	fmt.Println()

	fmt.Println("MCP tool integration demonstration complete.")
	fmt.Println()
	fmt.Println("Key Takeaway: MCP tools are discovered dynamically and each invocation")
	fmt.Println("passes through the Zero-Trust Gatekeeper before execution, enabling")
	fmt.Println("fine-grained access control over external tool capabilities.")
}

// runScenario drives a single supervised MCP tool call through ExecuteByName and
// reports whether the gate allowed/blocked it. When the action is a simulated
// MCP tool (the MCP-server-down fallback path), it also records whether the
// inner action actually executed, so we can assert blocked-vs-executed.
func runScenario(ctx context.Context, client *sdk.Client, simActions map[string]*simulatedMCPAction, name, operation, path string, expectAllowed bool) {
	payload := map[string]string{"path": path}
	opts := mcpMetaOpts(operation, path)

	var before int32
	if sa, ok := simActions[name]; ok {
		before = atomic.LoadInt32(&sa.execCount)
	}

	res, err := client.ExecuteByName(ctx, name, payload, opts...)

	var after int32
	if sa, ok := simActions[name]; ok {
		after = atomic.LoadInt32(&sa.execCount)
	}
	executed := after > before

	_ = res

	blocked := err != nil && core.IsPolicyViolationError(err)

	switch {
	case expectAllowed:
		if err == nil && (len(simActions) == 0 || executed) {
			fmt.Printf("Allowed: %s on %s permitted.\n", operation, path)
		} else {
			fmt.Printf("UNEXPECTEDLY BLOCKED (inner executed=%v): %v\n", executed, err)
		}
	default:
		if blocked && (len(simActions) == 0 || !executed) {
			fmt.Printf("Blocked: %v\n", err)
		} else if err == nil {
			fmt.Println("Unexpectedly allowed (should have blocked)")
		} else {
			fmt.Printf("Blocked but inner executed (gate ineffective): %v\n", err)
		}
	}
}

// mcpMetaOpts converts the buildMCPEnvelope classification into the metadata
// options forwarded by ExecuteByName. Policy facts must be metadata on the
// supervised path (ExecuteByName forwards only Metadata, not arbitrary facts).
func mcpMetaOpts(operation, path string) []sdk.ExecuteOption {
	_ = buildMCPEnvelope(operation, path, nil)
	opts := []sdk.ExecuteOption{
		sdk.WithMetadata("mcp_operation", operation),
		sdk.WithMetadata("mcp_path", path),
	}
	if isSensitivePath(path) {
		opts = append(opts,
			sdk.WithMetadata("mcp_path_sensitive", "true"),
			sdk.WithMetadata("mcp_path_restricted", "true"),
		)
	}
	return opts
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
	execCount  int32
}

func (a *simulatedMCPAction) Execute(ctx context.Context, input core.Envelope) (core.Envelope, error) {
	atomic.AddInt32(&a.execCount, 1)
	fmt.Printf("-> Executing simulated MCP tool: %s\n", a.name)
	return core.NewEnvelope(fmt.Sprintf("[simulated] %s executed", a.name)), nil
}

func (a *simulatedMCPAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{
		Name: fmt.Sprintf("mcp_%s_%s", a.serverName, a.name),
		Type: "mcp_tool",
	}
}
