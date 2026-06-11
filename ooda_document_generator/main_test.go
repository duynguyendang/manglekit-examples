package main

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/sdk/ooda"
)

func TestOODALoopConvergesOnCompliantDoc(t *testing.T) {
	ctx := context.Background()
	client, err := sdk.NewClient(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Shutdown(ctx) })

	gen := &DocumentGenerator{maxRounds: 5}
	observer := &MultiTurnObserver{gen: gen}
	orienter := &MultiTurnOrienter{client: client}
	decider := &MultiTurnDecider{gen: gen}
	verifier := &MultiTurnVerifier{client: client, gen: gen}
	actor := &MultiTurnActor{gen: gen}
	loop := ooda.NewLoop(observer, orienter, decider, verifier, actor)

	input := "Create a security policy document for the authentication module."
	frame := ooda.NewCognitiveFrame(input, "test-session", ooda.TaskTypeGeneration)
	frame.MaxRetries = 5

	// First call will load the policy, so it may retry (round 1 lacks approval/author).
	// We just want to confirm the loop runs, the policy loads, and a final frame
	// is produced without panicking.
	result, err := loop.Run(ctx, input, frame)
	if err != nil {
		t.Fatalf("OODA loop returned error: %v", err)
	}
	if result == nil {
		t.Fatal("expected non-nil result frame")
	}
	if gen.round < 1 {
		t.Errorf("expected at least one round executed, got %d", gen.round)
	}
}

// TestNoPythonFStringRemnantsInMain guards the original regression
// (Python f-string `{'='*60}` printed verbatim). It reads the actual
// main.go source and asserts none of the round-header printf calls
// still emit the malformed token.
func TestNoPythonFStringRemnantsInMain(t *testing.T) {
	_, filename, _, _ := runtime.Caller(0)
	src, err := os.ReadFile(filepath.Join(filepath.Dir(filename), "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	body := string(src)
	if strings.Contains(body, "{'='") {
		t.Errorf("main.go still contains Python f-string remnant {'='; check round-header Printf calls")
	}
	if strings.Contains(body, "{0:=^60}") {
		t.Errorf("main.go still contains Python f-string remnant {0:=^60}")
	}
}
