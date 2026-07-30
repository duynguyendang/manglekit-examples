// production_resilience demonstrates production infrastructure patterns:
//
//  1. Circuit Breaker: wraps a flaky action, transitioning through
//     Closed → Open → HalfOpen → Closed states based on failure threshold.
//
//  2. OpenTelemetry Tracing: WithStdoutTracer prints spans to stdout,
//     showing the observability integration.
//
// No API key required (deterministic flaky action + stdout tracer).

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync/atomic"

	"github.com/duynguyendang/manglekit/adapters/resilience"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
)

// flakyAction fails the first N times, then succeeds.
type flakyAction struct {
	failCount int32
	maxFails  int32
}

func (a *flakyAction) Execute(_ context.Context, env core.Envelope) (core.Envelope, error) {
	n := atomic.AddInt32(&a.failCount, 1)
	if n <= a.maxFails {
		return core.Envelope{}, fmt.Errorf("transient error (call %d)", n)
	}
	return core.NewEnvelope("success after retries"), nil
}

func (a *flakyAction) Metadata() core.ActionMetadata {
	return core.ActionMetadata{Name: "flaky_service", Type: "mock"}
}

func main() {
	ctx := context.Background()

	// --- Circuit Breaker Demo ---
	fmt.Println("=== Circuit Breaker Demo ===")
	fmt.Println()
	fmt.Println("Config: FailureThreshold=3, ResetTimeout=0s (immediate probe)")
	fmt.Println("Action: fails 3 times, then succeeds")
	fmt.Println()

	flaky := &flakyAction{maxFails: 3}
	cb := resilience.NewCircuitBreaker(flaky, resilience.CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     0, // immediate probe for demo
	})

	states := []string{"1st call", "2nd call", "3rd call (trips)", "4th call (probe)", "5th call (retry)"}
	for i, label := range states {
		env := core.NewEnvelope(fmt.Sprintf("request-%d", i+1))
		result, err := cb.Execute(ctx, env)

		stateName := "Closed"
		switch {
		case errors.Is(err, resilience.ErrCircuitOpen):
			stateName = "Open"
		case err == nil && i >= 3:
			stateName = "HalfOpen → Closed"
		}

		if err != nil {
			fmt.Printf("  %s: state=%s err=%v\n", label, stateName, err)
		} else {
			fmt.Printf("  %s: state=%s result=%v\n", label, stateName, result.Payload)
		}
	}

	// --- Telemetry Demo ---
	fmt.Println()
	fmt.Println("=== OpenTelemetry Tracing Demo ===")
	fmt.Println()
	fmt.Println("Using sdk.WithStdoutTracer() — spans print to stderr.")
	fmt.Println()

	client, err := sdk.NewClient(ctx, sdk.WithStdoutTracer())
	if err != nil {
		log.Fatal(err)
	}
	defer client.Shutdown(ctx)

	action := &flakyAction{maxFails: 0}
	client.RegisterAction("telemetry_test", client.Supervise(action))

	env := core.NewEnvelope("telemetry payload")
	env.SetMeta("trace_demo", "true")

	_, err = client.ExecuteByName(ctx, "telemetry_test", env.Payload)
	if err != nil {
		fmt.Printf("  ExecuteByName error: %v\n", err)
	} else {
		fmt.Println("  ExecuteByName succeeded (check stderr for trace spans)")
	}

	fmt.Println()
	fmt.Println("Done.")
}
