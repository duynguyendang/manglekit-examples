package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/duynguyendang/manglekit"
	function "github.com/duynguyendang/manglekit/adapters/func"
	"github.com/duynguyendang/manglekit/adapters/scoring"
	"github.com/duynguyendang/manglekit/core"
)

func exampleDir() string {
	_, filename, _, _ := runtime.Caller(0)
	return filepath.Dir(filename)
}

// policyPath / relaxedPolicyPath / brokenPolicyPath point at the three
// Datalog programs shipped with this example.
func policyPath() string        { return filepath.Join(exampleDir(), "policy.dl") }
func relaxedPolicyPath() string { return filepath.Join(exampleDir(), "policy_relaxed.dl") }
func brokenPolicyPath() string  { return filepath.Join(exampleDir(), "policy_broken.dl") }

// Outcomes of one gated routing call.
const (
	OutcomeAuto     = "auto_route"     // policy PROCEEDed: the ticket was dispatched
	OutcomeReview   = "send_to_review" // T1 halt: low confidence -> human review queue
	OutcomeEscalate = "escalate_human" // T1 halt: below the review floor / no valid choice
)

// Thresholds come from the kernel adapter; the alias keeps the tour code short.
type Thresholds = scoring.Thresholds

// ReadThresholds reads the bars from the ACTIVE policy via the kernel adapter.
// The policy holds per-mille integers (Mangle's comparisons are integer-only);
// this returns floats for reporting. After a ReloadPolicy these are the new
// numbers, and a missing/inverted/ambiguous bar is a hard error rather than a
// permissive default — see ADR-005.
//
// Note the bars are now only used for REPORTING and for the deprecated
// Go-side flags: the gate decision itself is made by the policy's own :lt/:ge
// comparisons over score_pm/2 and class_bar/2.
func ReadThresholds(ctx context.Context, client *manglekit.Client) (Thresholds, error) {
	return scoring.ThresholdsFromPolicyScaled(ctx, client.Engine(), "threshold_pm", scoring.PerMille)
}

// Gate carries the client and the supervised action counter so callers can
// prove whether the inner dispatch actually ran.
type Gate struct {
	client   *manglekit.Client
	executed int32
}

// RouteTicketInput is the supervised action payload: a strong type instead
// of a bare map so the action body is ordinary Go.
type RouteTicketInput struct {
	CaseID string
	Choice string
}

// NewClientWithPolicy builds the client and registers the supervised
// route_ticket action: a no-op dispatch recorder. The Zero-Trust pre-check
// is what decides; on halt the dispatch never happens.
func NewClientWithPolicy(ctx context.Context, policy string) (*Gate, error) {
	client, err := manglekit.QuickClient(ctx, policy)
	if err != nil {
		return nil, fmt.Errorf("load policy %s: %w", policy, err)
	}
	g := &Gate{client: client}
	client.RegisterSupervised("route_ticket", function.New("route_ticket",
		func(_ context.Context, in RouteTicketInput) (string, error) {
			atomic.AddInt32(&g.executed, 1)
			return "dispatched:" + in.Choice, nil
		}))
	return g, nil
}

// Shutdown releases the kernel client.
func (g *Gate) Shutdown(ctx context.Context) error { return g.client.Shutdown(ctx) }

// Executed returns how many route_ticket dispatches actually ran.
func (g *Gate) Executed() int32 { return atomic.LoadInt32(&g.executed) }

// Route applies the policy to one scoring decision and returns the outcome.
// Envelope construction — the meta flags plus the confidence/margin/entropy
// facts a proof tree grounds — is the kernel adapter's job (ADR-005: Go
// computes, the policy gates).
func (g *Gate) Route(ctx context.Context, caseID string, dec *Decision, th Thresholds) (string, error) {
	before := atomic.LoadInt32(&g.executed)

	env := scoring.Request(RouteTicketInput{CaseID: caseID, Choice: dec.Choice}, dec, th)

	_, err := g.client.ExecuteByName(ctx, "route_ticket", env)
	ran := atomic.LoadInt32(&g.executed) > before

	if err == nil {
		if !ran {
			return "", errors.New("gate: policy allowed but the action did not run")
		}
		return OutcomeAuto, nil
	}
	var pve *core.PolicyViolationError
	if !core.IsPolicyViolationError(err) || !errors.As(err, &pve) {
		return "", fmt.Errorf("route_ticket: %w", err)
	}
	if ran {
		return "", errors.New("gate: T1 halt fired but the action still ran")
	}
	// Classify the deny by the matched rule's reason.
	switch {
	case containsRule(pve, "escalate"):
		return OutcomeEscalate, nil
	case containsRule(pve, "review"):
		return OutcomeReview, nil
	default:
		return "", fmt.Errorf("unexpected deny: %v", err)
	}
}

// RouteUnobserved handles the case where the scorer could not observe every
// option's probability (a hosted API's top_logprobs was truncated). The
// decision to refuse auto-routing is the POLICY's, via the
// unobserved_distribution rule — not a Go fallback that invents a number.
func (g *Gate) RouteUnobserved(ctx context.Context, caseID string) (string, error) {
	before := atomic.LoadInt32(&g.executed)
	// A nil Decision is how the adapter says "no distribution was observable":
	// Request publishes unobserved_distribution and no numbers at all.
	env := scoring.Request(RouteTicketInput{CaseID: caseID, Choice: "?"}, nil, Thresholds{})
	_, err := g.client.ExecuteByName(ctx, "route_ticket", env)
	ran := atomic.LoadInt32(&g.executed) > before
	if err == nil {
		return "", errors.New("unobserved-distribution route unexpectedly allowed")
	}
	if ran || !core.IsPolicyViolationError(err) {
		return "", fmt.Errorf("unobserved-distribution route: %w", err)
	}
	return OutcomeEscalate, nil
}

// RouteChoiceOnly is the generation lane's honest path: the model wrote a
// letter and that is ALL the evidence. It publishes the choice and no number —
// the earlier version of this example fabricated a 1.0 distribution here, which
// cleared every bar and produced an authoritative-looking proof about a value
// nobody measured. The policy gates on the ABSENCE of a score instead
// (no_score_present_review).
func (g *Gate) RouteChoiceOnly(ctx context.Context, caseID, choice string) (string, error) {
	before := atomic.LoadInt32(&g.executed)
	env := scoring.RequestNoScore(RouteTicketInput{CaseID: caseID, Choice: choice}, choice)
	_, err := g.client.ExecuteByName(ctx, "route_ticket", env)
	ran := atomic.LoadInt32(&g.executed) > before
	if err == nil {
		if !ran {
			return "", errors.New("gate: policy allowed but the action did not run")
		}
		return OutcomeAuto, nil
	}
	var pve *core.PolicyViolationError
	if !core.IsPolicyViolationError(err) || !errors.As(err, &pve) {
		return "", fmt.Errorf("route_ticket: %w", err)
	}
	if ran {
		return "", errors.New("gate: T1 halt fired but the action still ran")
	}
	switch {
	case containsRule(pve, "no_score_present"):
		return OutcomeReview, nil
	case containsRule(pve, "escalate"):
		return OutcomeEscalate, nil
	case containsRule(pve, "review"):
		return OutcomeReview, nil
	default:
		return "", fmt.Errorf("unexpected deny: %v", err)
	}
}

// RouteNoChoice models the generation lane's OTHER/ESCALATE escape hatch:
// no parsable choice, so the policy must halt — refusing to guess is a
// governed decision, not Go code.
func (g *Gate) RouteNoChoice(ctx context.Context, caseID string) (string, error) {
	before := atomic.LoadInt32(&g.executed)
	env := core.NewEnvelope(RouteTicketInput{CaseID: caseID, Choice: "?"})
	env.Metadata = map[string]any{"no_valid_choice": "true"}
	_, err := g.client.ExecuteByName(ctx, "route_ticket", env)
	ran := atomic.LoadInt32(&g.executed) > before
	if err == nil {
		return "", errors.New("no-choice route unexpectedly allowed")
	}
	if ran || !core.IsPolicyViolationError(err) {
		return "", fmt.Errorf("no-choice route: %w", err)
	}
	return OutcomeEscalate, nil
}

func containsRule(err *core.PolicyViolationError, needle string) bool {
	needle = strings.ToLower(needle)
	for _, hay := range []string{err.MatchedRule, err.Violation, err.RuleID} {
		if strings.Contains(strings.ToLower(hay), needle) {
			return true
		}
	}
	return false
}
