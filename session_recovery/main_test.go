package main

import (
	"context"
	"sync"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/google/uuid"
)

func TestInMemoryStateProvider_CRUD(t *testing.T) {
	provider := NewInMemoryStateProvider()
	ctx := context.Background()

	t.Run("Get non-existent returns nil", func(t *testing.T) {
		raw, err := provider.Get(ctx, "missing")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if raw != nil {
			t.Errorf("expected nil, got %v", raw)
		}
	})

	t.Run("Set and Get round-trip", func(t *testing.T) {
		state := &core.SessionState{
			SessionID: "crud-test",
			ActiveEnvelope: core.Envelope{
				ID:       uuid.New(),
				Payload:  map[string]any{"key": "value"},
				Metadata: map[string]any{"meta": "data"},
			},
			ExecutionCtx: core.ExecutionContext{
				RetryCount:      2,
				FeedbackHistory: []string{"fb1", "fb2"},
			},
			LogicalFacts: []string{"fact(a).", "fact(b)."},
		}

		if err := provider.Set(ctx, "crud-test", state); err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		raw, err := provider.Get(ctx, "crud-test")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if raw == nil {
			t.Fatal("expected state, got nil")
		}

		// Verify data survives the JSON round-trip
		data, ok := raw.([]byte)
		if !ok {
			t.Fatalf("expected []byte, got %T", raw)
		}

		var restored core.SessionState
		if err := restored.UnmarshalJSON(data); err != nil {
			t.Fatalf("UnmarshalJSON failed: %v", err)
		}

		if restored.SessionID != "crud-test" {
			t.Errorf("SessionID: got %q, want %q", restored.SessionID, "crud-test")
		}
		if len(restored.LogicalFacts) != 2 {
			t.Errorf("LogicalFacts: got %d, want 2", len(restored.LogicalFacts))
		}
	})

	t.Run("Delete removes state", func(t *testing.T) {
		if err := provider.Delete(ctx, "crud-test"); err != nil {
			t.Fatalf("Delete failed: %v", err)
		}

		raw, err := provider.Get(ctx, "crud-test")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if raw != nil {
			t.Errorf("expected nil after delete, got %v", raw)
		}
	})

	t.Run("Close resets store", func(t *testing.T) {
		if err := provider.Set(ctx, "close-test", &core.SessionState{SessionID: "close-test"}); err != nil {
			t.Fatalf("Set failed: %v", err)
		}

		if err := provider.Close(ctx); err != nil {
			t.Fatalf("Close failed: %v", err)
		}

		raw, err := provider.Get(ctx, "close-test")
		if err != nil {
			t.Fatalf("Get failed: %v", err)
		}
		if raw != nil {
			t.Error("expected nil after Close")
		}
	})

	t.Run("Concurrent access safety", func(t *testing.T) {
		prov := NewInMemoryStateProvider()
		var wg sync.WaitGroup
		n := 50

		for i := 0; i < n; i++ {
			wg.Add(1)
			go func(idx int) {
				defer wg.Done()
				id := uuid.New().String()
				s := &core.SessionState{SessionID: id}
				if err := prov.Set(ctx, id, s); err != nil {
					t.Errorf("Set failed: %v", err)
				}
				if _, err := prov.Get(ctx, id); err != nil {
					t.Errorf("Get failed: %v", err)
				}
				if err := prov.Delete(ctx, id); err != nil {
					t.Errorf("Delete failed: %v", err)
				}
			}(i)
		}
		wg.Wait()
	})
}

func TestSessionCheckpointAndRecover(t *testing.T) {
	provider := NewInMemoryStateProvider()
	sm := NewSessionManager(provider)
	ctx := context.Background()

	sessionID := "recover-test"

	t.Run("Hydrate returns nil for non-existent session", func(t *testing.T) {
		state, err := sm.Hydrate(ctx, sessionID)
		if err != nil {
			t.Fatalf("Hydrate failed: %v", err)
		}
		if state != nil {
			t.Error("expected nil for non-existent session")
		}
	})

	t.Run("Checkpoint and Hydrate preserves full state", func(t *testing.T) {
		original := &core.SessionState{
			SessionID: sessionID,
			ActiveEnvelope: core.Envelope{
				ID:             uuid.New(),
				Payload:        map[string]any{"workflow": "test", "step": 3},
				ContentType:    core.TypeJSON,
				Metadata:       map[string]any{"env": "test", "attempt": 1},
				SecurityLabels: []string{"internal"},
				Facts:          []string{"step(init)", "step(load)", "step(validate)"},
			},
			ExecutionCtx: core.ExecutionContext{
				RetryCount:      0,
				FeedbackHistory: []string{},
				CurrentHistory: []core.Message{
					{Role: "system", Content: "Executed: Initialize"},
					{Role: "system", Content: "Executed: Load Config"},
					{Role: "system", Content: "Executed: Validate"},
				},
			},
			LogicalFacts: []string{"step(init)", "step(load)", "step(validate)"},
		}

		if err := sm.Checkpoint(ctx, original); err != nil {
			t.Fatalf("Checkpoint failed: %v", err)
		}

		recovered, err := sm.Hydrate(ctx, sessionID)
		if err != nil {
			t.Fatalf("Hydrate failed: %v", err)
		}
		if recovered == nil {
			t.Fatal("expected recovered state, got nil")
		}

		if recovered.SessionID != original.SessionID {
			t.Errorf("SessionID: got %q, want %q", recovered.SessionID, original.SessionID)
		}
		if len(recovered.LogicalFacts) != len(original.LogicalFacts) {
			t.Errorf("LogicalFacts: got %d, want %d", len(recovered.LogicalFacts), len(original.LogicalFacts))
		}
		if len(recovered.ExecutionCtx.CurrentHistory) != len(original.ExecutionCtx.CurrentHistory) {
			t.Errorf("CurrentHistory: got %d, want %d", len(recovered.ExecutionCtx.CurrentHistory), len(original.ExecutionCtx.CurrentHistory))
		}
	})

	t.Run("Simulated crash recovery preserves state", func(t *testing.T) {
		steps := []string{"step1(init)", "step2(config)", "step3(validate)", "step4(execute)", "step5(finalize)"}

		// Phase 1: execute steps 1-3 and checkpoint
		state := &core.SessionState{
			SessionID: "crash-recovery",
			ActiveEnvelope: core.Envelope{
				ID:          uuid.New(),
				Payload:     map[string]any{"workflow": "order"},
				ContentType: core.TypeJSON,
				Metadata:    make(map[string]any),
			},
			ExecutionCtx: core.ExecutionContext{},
		}

		for i := 0; i < 3; i++ {
			state.LogicalFacts = append(state.LogicalFacts, steps[i])
			state.ExecutionCtx.CurrentHistory = append(state.ExecutionCtx.CurrentHistory, core.Message{
				Role:    "system",
				Content: steps[i],
			})
		}

		if err := sm.Checkpoint(ctx, state); err != nil {
			t.Fatalf("Checkpoint failed: %v", err)
		}

		// Phase 2: simulate crash — create new manager with same provider
		sm2 := NewSessionManager(provider)

		recovered, err := sm2.Hydrate(ctx, "crash-recovery")
		if err != nil {
			t.Fatalf("Hydrate failed: %v", err)
		}
		if recovered == nil {
			t.Fatal("expected recovered state after crash, got nil")
		}

		// Verify steps 1-3 are preserved
		if len(recovered.LogicalFacts) != 3 {
			t.Errorf("facts after recovery: got %d, want 3", len(recovered.LogicalFacts))
		}
		if len(recovered.ExecutionCtx.CurrentHistory) != 3 {
			t.Errorf("history after recovery: got %d, want 3", len(recovered.ExecutionCtx.CurrentHistory))
		}

		// Continue from step 4
		for i := 3; i < len(steps); i++ {
			recovered.LogicalFacts = append(recovered.LogicalFacts, steps[i])
			recovered.ExecutionCtx.CurrentHistory = append(recovered.ExecutionCtx.CurrentHistory, core.Message{
				Role:    "system",
				Content: steps[i],
			})
		}

		if err := sm2.Checkpoint(ctx, recovered); err != nil {
			t.Fatalf("Final checkpoint failed: %v", err)
		}

		// Final verification
		final, err := sm2.Hydrate(ctx, "crash-recovery")
		if err != nil {
			t.Fatalf("Final hydrate failed: %v", err)
		}
		if len(final.LogicalFacts) != 5 {
			t.Errorf("final facts: got %d, want 5", len(final.LogicalFacts))
		}
		if len(final.ExecutionCtx.CurrentHistory) != 5 {
			t.Errorf("final history: got %d, want 5", len(final.ExecutionCtx.CurrentHistory))
		}
	})

	t.Run("Multiple sessions are independent", func(t *testing.T) {
		s1 := &core.SessionState{
			SessionID:      "session-a",
			ActiveEnvelope: core.Envelope{ID: uuid.New(), ContentType: core.TypeJSON},
			LogicalFacts:   []string{"fact_a"},
		}
		s2 := &core.SessionState{
			SessionID:      "session-b",
			ActiveEnvelope: core.Envelope{ID: uuid.New(), ContentType: core.TypeJSON},
			LogicalFacts:   []string{"fact_b1", "fact_b2"},
		}

		if err := sm.Checkpoint(ctx, s1); err != nil {
			t.Fatalf("Checkpoint s1 failed: %v", err)
		}
		if err := sm.Checkpoint(ctx, s2); err != nil {
			t.Fatalf("Checkpoint s2 failed: %v", err)
		}

		r1, _ := sm.Hydrate(ctx, "session-a")
		r2, _ := sm.Hydrate(ctx, "session-b")

		if len(r1.LogicalFacts) != 1 {
			t.Errorf("session-a facts: got %d, want 1", len(r1.LogicalFacts))
		}
		if len(r2.LogicalFacts) != 2 {
			t.Errorf("session-b facts: got %d, want 2", len(r2.LogicalFacts))
		}
	})
}
