package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// runThresholdDemo is Truc B — the threshold-effect demo, kept separate from
// the two-lane benchmark on purpose (different axes: one compares lanes,
// this one compares policies).
//
// Method: score the full fixture set once under the strict policy, once
// after client.ReloadPolicy swaps in the relaxed one — same client, same
// process, no rebuild. The blog's Python-version of this step is "edit the
// file and redeploy"; here the swap is the feature.
func runThresholdDemo(ctx context.Context, scorer Scorer, cases []datasets.Case) error {
	gate, err := NewClientWithPolicy(ctx, policyPath())
	if err != nil {
		return err
	}
	defer gate.Shutdown(ctx)

	strict, err := measureUnderPolicy(ctx, gate, scorer, cases, "policy.dl")
	if err != nil {
		return err
	}

	fmt.Println("\n=== Hot-swap: client.ReloadPolicy(policy_relaxed.dl) — same process ===")
	if err := gate.client.ReloadPolicy(ctx, relaxedPolicyPath()); err != nil {
		return fmt.Errorf("reload relaxed: %w", err)
	}
	relaxed, err := measureUnderPolicy(ctx, gate, scorer, cases, "policy_relaxed.dl")
	if err != nil {
		return err
	}

	// Fail-safe: a broken program must be rejected and the live policy must
	// keep serving — verified by re-measuring the threshold facts.
	fmt.Println("=== Fail-safe: ReloadPolicy(policy_broken.dl) must be REJECTED ===")
	if err := gate.client.ReloadPolicy(ctx, brokenPolicyPath()); err == nil {
		return fmt.Errorf("broken policy accepted — fail-safe reload is broken")
	} else {
		fmt.Printf("rejected as expected (%v)\n", err)
	}
	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		return err
	}
	if th.Auto != 0.70 {
		return fmt.Errorf("after failed reload the ACTIVE policy must be unchanged (relaxed auto=0.70), got auto=%.2f", th.Auto)
	}
	fmt.Println("old policy still active and serving: threshold(\"auto\") == 0.70")

	fmt.Printf("\n=== Threshold effect over %d scored cases ===\n", len(cases))
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "policy\tauto\treview\tescalate")
	for _, m := range []policyMeasure{strict, relaxed} {
		fmt.Fprintf(w, "%s\t%.1f%%\t%.1f%%\t%.1f%%\n", m.name,
			100*float64(m.auto)/float64(m.total),
			100*float64(m.review)/float64(m.total),
			100*float64(m.escalate)/float64(m.total))
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Println("accuracy never changes — the model's argmax is fixed; what moves is how much of it the policy will auto-route unreviewed.")
	return nil
}

type policyMeasure struct {
	name     string
	total    int
	auto     int
	review   int
	escalate int
}

func measureUnderPolicy(ctx context.Context, gate *Gate, scorer Scorer, cases []datasets.Case, name string) (policyMeasure, error) {
	th, err := ReadThresholds(ctx, gate.client)
	if err != nil {
		return policyMeasure{}, err
	}
	m := policyMeasure{name: name}
	for _, c := range cases {
		opts := optionsFor(c)
		dec, err := scorer.Score(ctx, BuildPrompt(c, opts), optionLetters, opts)
		if err != nil {
			return m, fmt.Errorf("score %s: %w", c.ID, err)
		}
		outcome, err := gate.Route(ctx, c.ID, dec, th)
		if err != nil {
			return m, fmt.Errorf("route %s: %w", c.ID, err)
		}
		m.total++
		switch outcome {
		case OutcomeAuto:
			m.auto++
		case OutcomeReview:
			m.review++
		case OutcomeEscalate:
			m.escalate++
		}
	}
	fmt.Printf("measured %s (auto>=%.2f, review>=%.2f)\n", name, th.Auto, th.Review)
	return m, nil
}
