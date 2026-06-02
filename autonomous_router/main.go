package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/duynguyendang/manglekit"
	"github.com/duynguyendang/manglekit/core"
)

// Input struct for routing
type Input struct {
	Tier string `mangle:"payload.tier"`
	User string `mangle:"payload.user"`
}

// SQLOutput struct for generation
type SQLOutput struct {
	SQL string `mangle:"payload.sql"`
}

func main() {
	ctx := context.Background()

	// 1. Initialize Client
	client := manglekit.Must(manglekit.NewClient(ctx, manglekit.WithBlueprintPath("autonomous_router/blueprint.dl")))

	// 2. Register Actions
	client.RegisterAction("generate_sql", client.Supervise(&SQLGenerator{}))
	client.RegisterAction("classify", client.Supervise(&RouterAction{}))
	client.RegisterAction("vip_agent", client.Supervise(&VIPAction{}))

	// 3. Run Scenarios

	// Scenario 1: SQL Generator
	// The blueprint defines a correction(Req, Msg) rule for bad SQL.
	// The SQLGenerator action checks env.Metadata for feedback from a prior retry
	// and adjusts its output accordingly.
	fmt.Println("--- Scenario 1: Generate SQL ---")
	res, err := client.Action("generate_sql").Execute(ctx, core.NewEnvelope(nil))
	if err != nil {
		fmt.Printf("Execute failed: %v\n", err)
	} else {
		if out, ok := res.Payload.(SQLOutput); ok {
			fmt.Printf("Result: %s\n", out.SQL)
		} else {
			fmt.Printf("Result: %+v\n", res.Payload)
		}
	}

	// Scenario 2: Gold Tier → route to VIP Agent
	// The blueprint has: route("vip_agent") :- payload.tier(Req, "gold").
	// The classify action passes through; post-execution steering detects
	// the route decision and forwards to vip_agent which returns the VIP message.
	fmt.Println("\n--- Scenario 2: Gold Tier → VIP Agent ---")
	res, err = client.Action("classify").Execute(ctx, core.NewEnvelope(Input{Tier: "gold"}))
	if err != nil {
		fmt.Printf("Execute failed: %v\n", err)
	} else {
		if val, ok := res.Payload.(string); ok {
			fmt.Printf("VIP Agent delivered: %s\n", val)
		} else if val, ok := res.Payload.(Input); ok {
			fmt.Printf("Routing did not fire; passed through as: tier=%s\n", val.Tier)
		} else {
			fmt.Printf("Result: %+v\n", res.Payload)
		}
	}

	// Scenario 3: Silver tier (no VIP routing rule)
	fmt.Println("\n--- Scenario 3: Silver Tier (no routing) ---")
	res, err = client.Action("classify").Execute(ctx, core.NewEnvelope(Input{Tier: "silver"}))
	if err != nil {
		fmt.Printf("Execute failed: %v\n", err)
	} else {
		if val, ok := res.Payload.(Input); ok {
			fmt.Printf("Passed through (no routing): tier=%s\n", val.Tier)
		} else {
			fmt.Printf("Result: %+v\n", res.Payload)
		}
	}
}

// SQLGenerator implementation
type SQLGenerator struct{}

func (a *SQLGenerator) Execute(ctx context.Context, env core.Envelope) (core.Envelope, error) {
	// Check previous feedback
	feedback := ""
	if v, ok := env.Metadata[core.KeyPrevFeedback]; ok {
		if s, ok := v.(string); ok {
			feedback = s
		} else {
			feedback = fmt.Sprintf("%v", v)
		}
	}

	sql := "SELECT * FROM users; DROP TABLE users;" // Default bad
	if strings.Contains(feedback, "Do not use DROP") {
		sql = "SELECT * FROM users; DELETE FROM users;" // Fixed
	}

	// Create output envelope with SQLOutput payload
	return core.NewEnvelope(SQLOutput{SQL: sql}), nil
}

func (a *SQLGenerator) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "generate_sql", Type: "generator"}
}

// RouterAction implementation
type RouterAction struct{}

func (a *RouterAction) Execute(ctx context.Context, env core.Envelope) (core.Envelope, error) {
	// Pass through the input as output
	return env, nil
}
func (a *RouterAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "classify", Type: "router"}
}

// VIPAction implementation
type VIPAction struct{}

func (a *VIPAction) Execute(ctx context.Context, env core.Envelope) (core.Envelope, error) {
	return core.NewEnvelope("VIP Service Executed"), nil
}
func (a *VIPAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "vip_agent", Type: "agent"}
}
