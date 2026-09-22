package main

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"

	"github.com/duynguyendang/manglekit"
	function "github.com/duynguyendang/manglekit/adapters/func"
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

// Thresholds read FROM the policy at runtime (threshold/2 rules — see the
// policy.dl header for why rule-defined beats bare facts). The policy file
// is the single source of truth; Go only does the arithmetic the engine's
// analyzer deliberately refuses to do (cross-fact :lt on floats).
type Thresholds struct {
	Auto   float64
	Margin float64
	Review float64
}

// ReadThresholds queries the loaded policy for its threshold facts. After a
// ReloadPolicy these are the NEW numbers — that is the whole point.
func ReadThresholds(ctx context.Context, client *manglekit.Client) (Thresholds, error) {
	get := func(name string) (float64, error) {
		sols, err := client.Engine().Query(ctx, nil, fmt.Sprintf(`threshold("%s", V)`, name))
		if err != nil {
			return 0, fmt.Errorf(`query threshold("%s"): %w`, name, err)
		}
		if len(sols) == 0 {
			return 0, fmt.Errorf(`policy has no threshold("%s") fact`, name)
		}
		v, err := strconv.ParseFloat(sols[0]["V"], 64)
		if err != nil {
			return 0, fmt.Errorf(`threshold("%s")=%q not a float: %w`, name, sols[0]["V"], err)
		}
		return v, nil
	}
	var th Thresholds
	var err error
	if th.Auto, err = get("auto"); err != nil {
		return th, err
	}
	if th.Margin, err = get("margin"); err != nil {
		return th, err
	}
	if th.Review, err = get("review"); err != nil {
		return th, err
	}
	return th, nil
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

// Route applies the policy to one scoring decision and returns the
// outcome. It publishes the decision's numbers as facts and the
// threshold comparisons as meta flags (Go computes, policy gates — the
// same boundary devops_policy_gate documents).
func (g *Gate) Route(ctx context.Context, caseID string, dec *Decision, th Thresholds) (string, error) {
	before := atomic.LoadInt32(&g.executed)

	top, ok := dec.Distribution[dec.Choice]
	if !ok {
		top = 0
	}
	env := core.NewEnvelope(RouteTicketInput{CaseID: caseID, Choice: dec.Choice})
	env.Metadata = map[string]any{}
	if top < th.Review {
		env.Metadata["below_review"] = "true"
	}
	if top < th.Auto {
		env.Metadata["below_auto"] = "true"
	}
	if th.Auto <= top && top-decSecond(dec) < th.Margin {
		env.Metadata["thin_margin"] = "true" // advisory only: T2 never blocks
	}
	env.Facts = append(env.Facts,
		fmt.Sprintf("confidence(%q).", strconv.FormatFloat(top, 'f', 3, 64)),
		fmt.Sprintf("margin_value(%q).", strconv.FormatFloat(top-decSecond(dec), 'f', 3, 64)),
		fmt.Sprintf("entropy_value(%q).", strconv.FormatFloat(dec.Entropy, 'f', 3, 64)),
	)

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

func decSecond(dec *Decision) float64 {
	best, second := -1.0, -1.0
	for _, v := range dec.Distribution {
		if v >= best {
			second = best
			best = v
		} else if v > second {
			second = v
		}
	}
	if second < 0 {
		return 0
	}
	return second
}
