// http_service embeds manglekit in a plain net/http Go service: a /ask
// endpoint whose handler executes a supervised LLM action via
// ExecuteByName — plus the production wrapper patterns (resilience.go,
// formerly the production_resilience example: circuit breaker + OTel
// tracer around the same supervised stack). Enforcement lives in the kernel — the handler just maps
// kernel outcomes onto HTTP statuses:
//
//	PROCEED                    -> 200 {"answer": ...}
//	PolicyViolationError       -> 403 {"error": ...}
//	any other error            -> 500 {"error": ...}
//
// It also demonstrates HOT POLICY RELOAD in-process: POST /admin/reload
// swaps the active policy atomically (parse + evaluate against a copy of
// state BEFORE the swap). A failed reload keeps the old policy serving —
// the same guarantee `mkit serve` exposes via SIGHUP, minus the signal.
//
// Without OPENAI_API_KEY/GOOGLE_API_KEY the LLM is a testutil.MockLLM, so
// the service is fully deterministic and needs no key.
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"runtime"

	"github.com/duynguyendang/manglekit"
	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/sdk"
	"github.com/duynguyendang/manglekit/testutil"
)

// AskRequest is the JSON body POSTed to /ask.
type AskRequest struct {
	Topic string `json:"topic"`
	Query string `json:"query"`
}

// AskResponse is the 200 body.
type AskResponse struct {
	Reply string `json:"reply"`
}

// ErrorResponse is the 4xx/5xx body.
type ErrorResponse struct {
	Error string `json:"error"`
}

// Server wires an HTTP mux to a supervised manglekit client.
type Server struct {
	client *sdk.Client
}

// ReloadRequest asks the server to atomically swap its active policy.
type ReloadRequest struct {
	PolicyPath string `json:"policy_path"`
}

// policyPath resolves policy.dl relative to this file (cwd-safe).
func policyPath() string {
	const rel = "policy.dl"
	if _, err := os.Stat(rel); err == nil {
		return rel
	}
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), rel)
}

// NewServer builds the supervised client (mock LLM unless a key is set) and
// registers routes on a new mux.
func NewServer(ctx context.Context) (*Server, error) {
	// Deterministic mock LLM: the interesting behavior here is the gate, not
	// the model. Swap in providers/openai.NewLLM or a Genkit action for a
	// live model — the HTTP + policy wiring is unchanged.
	gen := core.TextGenerator(testutil.NewMockLLM("Mock answer: your request passed the policy gate."))

	client, err := manglekit.QuickClient(ctx, policyPath())
	if err != nil {
		return nil, err
	}

	act := function.New("answer", func(ctx context.Context, req AskRequest) (string, error) {
		reply, err := gen.Complete(ctx, fmt.Sprintf("Answer in one sentence, topic %s: %s", req.Topic, req.Query))
		if err != nil {
			return "", err
		}
		return reply, nil
	})
	client.RegisterSupervised("answer", act)

	return &Server{client: client}, nil
}

// Handler returns the routed http.Handler.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /ask", s.handleAsk)
	mux.HandleFunc("POST /admin/reload", s.handleReload)
	return mux
}

// Shutdown releases the kernel client.
func (s *Server) Shutdown(ctx context.Context) { s.client.Shutdown(ctx) }

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// handleAsk executes the supervised "answer" action. The supervisor
// pre-check runs BEFORE the LLM: a denied topic never reaches the model.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	var req AskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "invalid JSON body: " + err.Error()})
		return
	}

	out, err := s.client.ExecuteByName(r.Context(), "answer", req,
		sdk.WithMetadata("topic", req.Topic),
	)
	if err != nil {
		switch {
		case core.IsPolicyViolationError(err):
			// Policy deny: the caller asked for something forbidden.
			writeJSON(w, http.StatusForbidden, ErrorResponse{Error: err.Error()})
		default:
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		}
		return
	}

	writeJSON(w, http.StatusOK, AskResponse{Reply: fmt.Sprintf("%v", out.Payload)})
}

// handleReload hot-swaps the active policy from a new file. On success the
// very next request is evaluated against the new program; on failure the
// old policy is still active and the error is reported (never partial).
// The swap also proves engine builtins survive reloads: the std.dl deny/halt
// vocabulary and planner rules keep working after the swap.
func (s *Server) handleReload(w http.ResponseWriter, r *http.Request) {
	var req ReloadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.PolicyPath == "" {
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: `invalid body: {"policy_path": "..."} required`})
		return
	}
	if err := s.client.ReloadPolicy(r.Context(), req.PolicyPath); err != nil {
		// Fail-safe: the reload was rejected, the OLD policy stays active.
		writeJSON(w, http.StatusBadRequest, ErrorResponse{Error: "reload failed, previous policy still active: " + err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "reloaded", "policy": req.PolicyPath})
}

func main() {
	ctx := context.Background()
	srv, err := NewServer(ctx)
	if err != nil {
		log.Fatalf("build server: %v", err)
	}
	defer srv.Shutdown(ctx)

	// Production patterns first (circuit breaker + tracing), then serve.
	if err := demoResilience(ctx); err != nil {
		log.Fatalf("resilience section: %v", err)
	}

	addr := ":8080"
	fmt.Printf("http_service listening on %s\n", addr)
	fmt.Println("  POST /ask           {\"topic\":..., \"query\":...}  — supervised question")
	fmt.Println("  POST /admin/reload  {\"policy_path\":...}          — hot policy swap (fail-safe)")
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatal(err)
	}
}
