package main

import (
	"context"
	"testing"

	"github.com/duynguyendang/manglekit/x/ooda"
)

func TestSkillLearningAcrossSessions(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()

	// Session 1: cold library — the draft needs one refinement round and
	// no route is known yet.
	frame1, rep1, err := runSession(ctx, dir)
	if err != nil {
		t.Fatalf("session 1: %v", err)
	}
	if rep1.retries != 2 {
		t.Errorf("session 1 retries = %d, want 2 (one refinement)", rep1.retries)
	}
	if rep1.status != ooda.VerifyStatusPassed {
		t.Errorf("session 1 status = %s, want passed", rep1.status)
	}
	if rep1.hintsFed != 0 {
		t.Errorf("session 1 should start with an empty skill library, got %d hints", rep1.hintsFed)
	}
	if frame1 == nil {
		t.Fatal("session 1 returned nil frame")
	}

	// Session 2: everything rebuilt from scratch (new memory, new reasoner,
	// new brain) — only the files carry over. The learned hint must hydrate
	// Orient so validation passes on the first attempt, and SteerKB must get
	// the route learned in session 1.
	_, rep2, err := runSession(ctx, dir)
	if err != nil {
		t.Fatalf("session 2: %v", err)
	}
	if rep2.retries != 1 {
		t.Errorf("session 2 retries = %d, want 1 (learned skill skips refinement)", rep2.retries)
	}
	if rep2.hintsFed != 1 {
		t.Errorf("session 2 hints recalled = %d, want 1", rep2.hintsFed)
	}
	if rep2.routeAnswer != "fast_path" {
		t.Errorf("session 2 SteerKB answer = %q, want fast_path", rep2.routeAnswer)
	}
}

func TestSkillMemoryRoundTripsThroughFile(t *testing.T) {
	path := t.TempDir() + "/skills.jsonl"
	mem, err := openSkillMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	err = mem.Store(context.Background(), ooda.Atom{
		Predicate: "skill_hint", Subject: "task-x", Object: "do-it-in-order", Weight: 0.5,
	})
	if err != nil {
		t.Fatal(err)
	}

	// Reopen: simulate a process restart.
	reopened, err := openSkillMemory(path)
	if err != nil {
		t.Fatal(err)
	}
	hints, err := reopened.Recall(context.Background(), "task-x")
	if err != nil {
		t.Fatal(err)
	}
	if len(hints) != 1 || hints[0].Object != "do-it-in-order" {
		t.Fatalf("recall after reopen = %+v", hints)
	}
	other, err := reopened.Recall(context.Background(), "task-y")
	if err != nil {
		t.Fatal(err)
	}
	if len(other) != 0 {
		t.Fatalf("expected no hints for unrelated input, got %+v", other)
	}
}

func TestSkillReasonerOnlyAnswersSteerQueries(t *testing.T) {
	r, err := openSkillReasoner(t.TempDir() + "/routes.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	got, err := r.VerifyWithDatalog(ctx, `fast_path(6ba7b810-9dad-11d1-80b4-00c04fd430c8)`)
	if err != nil || got != nil {
		t.Fatalf("unknown key: got=%v err=%v, want nil", got, err)
	}
	if err := r.learnRoute("fast_path", skillKey(task)); err != nil {
		t.Fatal(err)
	}
	got, err = r.VerifyWithDatalog(ctx, `fast_path(`+skillKey(task).String()+`)`)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0]["route"] != "fast_path" {
		t.Fatalf("learned key: got %v", got)
	}
	if got, err = r.VerifyWithDatalog(ctx, `halt("Req", Reason, Tier)`); err != nil || got != nil {
		t.Fatalf("non-steer query leaked: got=%v err=%v", got, err)
	}
}
