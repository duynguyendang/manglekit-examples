package main

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/duynguyendang/manglekit/adapters/resilience"
	"github.com/duynguyendang/manglekit/core"
)

func TestCircuitBreaker_TripAfterThreshold(t *testing.T) {
	action := &resilFlakyAction{maxFails: 3}
	cb := resilience.NewCircuitBreaker(action, resilience.CircuitBreakerConfig{
		FailureThreshold: 3,
		ResetTimeout:     1 * time.Hour, // long enough that the circuit stays open
	})
	ctx := context.Background()

	// First 3 calls fail but don't trip (below threshold)
	for i := 0; i < 3; i++ {
		_, err := cb.Execute(ctx, core.NewEnvelope("test"))
		if err == nil {
			t.Fatalf("call %d: expected error", i+1)
		}
	}

	// 4th call trips the circuit (threshold reached)
	_, err := cb.Execute(ctx, core.NewEnvelope("test"))
	if !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}
}

func TestCircuitBreaker_RecoveryAfterReset(t *testing.T) {
	action := &resilFlakyAction{maxFails: 2}
	cb := resilience.NewCircuitBreaker(action, resilience.CircuitBreakerConfig{
		FailureThreshold: 2,
		ResetTimeout:     10 * time.Millisecond, // short timeout for test
	})
	ctx := context.Background()

	// Trip the circuit
	for i := 0; i < 2; i++ {
		cb.Execute(ctx, core.NewEnvelope("test"))
	}

	// Circuit should be open
	_, err := cb.Execute(ctx, core.NewEnvelope("test"))
	if !errors.Is(err, resilience.ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen, got %v", err)
	}

	// Wait for reset timeout to elapse
	time.Sleep(15 * time.Millisecond)

	// Next call should transition to HalfOpen and probe (action now succeeds)
	result, err := cb.Execute(ctx, core.NewEnvelope("test"))
	if err != nil {
		t.Fatalf("probe should succeed after reset, got %v", err)
	}
	if result.Payload != "success after retries" {
		t.Errorf("unexpected payload: %v", result.Payload)
	}
}
