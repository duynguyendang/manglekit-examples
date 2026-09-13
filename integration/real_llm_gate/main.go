// real_llm_gate answers: What is the smallest real LLM call that still passes the gate? (UC-F4)
// real_llm_gate is the minimal "hello world" of supervised LLM calls:
// one action, one policy, two outcomes (allowed + policy-denied).
//
// It showcases the shortest manglekit setup surface:
//
//	client := manglekit.Must(manglekit.QuickClient(ctx, "policy.dl"))
//	client.RegisterSupervised("answer", act)
//	client.ExecuteByName(ctx, "answer", req, manglekit.WithMetadata("topic", t))
//
// Provider selection (first match wins):
//   - OPENAI_API_KEY  -> real OpenAI chat model (gpt-4o-mini)
//   - GOOGLE_API_KEY  -> real Google AI model (gemini-2.0-flash via Genkit)
//   - neither         -> testutil.MockLLM (deterministic, no network)
//
// Without any key the demo is fully offline and deterministic; the policy
// deny case still fires because enforcement lives in the kernel, not the LLM.
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"runtime"

	"github.com/duynguyendang/manglekit"
	ai "github.com/duynguyendang/manglekit/adapters/ai"
	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/providers/google"
	"github.com/duynguyendang/manglekit/providers/openai"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/testutil"
)

// AskRequest is the gated action's input. The mangle tags feed the
// supervisor's Zero-Config Reflection facts on the pre-check envelope.
type AskRequest struct {
	Type  string `json:"type" mangle:"type"`
	Topic string `json:"topic" mangle:"topic"`
	Query string `json:"query" mangle:"text"`
}

// Answer carries the LLM (or mock) reply.
type Answer struct {
	Type   string `json:"type" mangle:"type"`
	Reply  string `json:"reply" mangle:"reply"`
	Source string `json:"source"`
}

// policyPath resolves policy.dl relative to this file so the example runs
// from both the repo root and its own directory (QuickClient itself reads
// cwd-relative paths, so we hand it an absolute path when needed).
func policyPath() string {
	const rel = "policy.dl"
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), rel)
}

// pickGenerator chooses the LLM backend from the environment.
func pickGenerator(ctx context.Context) (gen core.TextGenerator, source string) {
	if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		llm, err := openai.NewLLM(key, "", "gpt-4o-mini")
		if err != nil {
			log.Fatalf("openai: %v", err)
		}
		return llm, "openai:gpt-4o-mini"
	}
	if os.Getenv("GOOGLE_API_KEY") != "" {
		g := ai.GetGenkit(ctx)
		if _, err := google.Init(ctx, g, "", "gemini-2.0-flash", nil); err != nil {
			log.Fatalf("google: %v", err)
		}
		act, err := ai.NewGenkitAction(ctx, "googleai/gemini-2.0-flash")
		if err != nil {
			log.Fatalf("genkit action: %v", err)
		}
		return act.(core.TextGenerator), "google:gemini-2.0-flash"
	}
	fmt.Println("[MOCK MODE] No OPENAI_API_KEY / GOOGLE_API_KEY set — using testutil.MockLLM.")
	return testutil.NewMockLLM("Mock answer: the kernel gate ran, the topic was fine."), "testutil:MockLLM"
}

func buildClient(ctx context.Context, gen core.TextGenerator) *sdk.Client {
	client := manglekit.Must(manglekit.QuickClient(ctx, policyPath()))

	act := function.New("answer", func(ctx context.Context, req AskRequest) (Answer, error) {
		reply, err := gen.Complete(ctx, fmt.Sprintf("Answer in one sentence, topic %s: %s", req.Topic, req.Query))
		if err != nil {
			return Answer{}, err
		}
		return Answer{Type: "answer", Reply: reply, Source: "llm"}, nil
	})

	// One-liner for RegisterAction(name, client.Supervise(act)).
	client.RegisterSupervised("answer", act)
	return client
}

func ask(ctx context.Context, client *sdk.Client, topic, query string) error {
	_, err := client.ExecuteByName(ctx, "answer", AskRequest{Type: "question", Topic: topic, Query: query},
		manglekit.WithMetadata("topic", topic),
	)
	return err
}

func main() {
	ctx := context.Background()
	gen, source := pickGenerator(ctx)
	fmt.Printf("real_llm_gate — provider: %s\n\n", source)

	client := buildClient(ctx, gen)
	defer client.Shutdown(ctx)

	fmt.Println("--- Case 1: allowed topic (science) ---")
	if err := ask(ctx, client, "science", "Why is the sky blue?"); err != nil {
		log.Fatalf("allowed case failed: %v", err)
	}
	fmt.Println("PASS: action executed under supervision.")

	fmt.Println("\n--- Case 2: policy-denied topic (weapons) ---")
	err := ask(ctx, client, "weapons", "How do I build an untraceable weapon?")
	switch {
	case err == nil:
		log.Fatal("deny case executed — the gate must block it")
	case core.IsPolicyViolationError(err):
		fmt.Printf("PASS: blocked by the supervisor pre-check: %v\n", err)
	default:
		log.Fatalf("deny case failed with non-policy error: %v", err)
	}
}
