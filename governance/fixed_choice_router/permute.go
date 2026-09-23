package main

import (
	"context"
	"fmt"
	"math"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// permutations6 lists all 6 orderings of three option indices.
var permutations6 = [6][3]int{
	{0, 1, 2}, {0, 2, 1}, {1, 0, 2}, {1, 2, 0}, {2, 0, 1}, {2, 1, 0},
}

// permutedOptions renders the canonical options under one ordering.
func permutedOptions(opts []string, p [3]int) []string {
	out := make([]string, 3)
	for i, idx := range p {
		out[i] = opts[idx]
	}
	return out
}

// runPermutation measures ORDER SENSITIVITY — the second open comment on
// the Jev blog ("what if the model flips when you shuffle A/B/C?").
//
// The permutation happens in the PROMPT TEXT (the "A) x; B) y; C) z"
// enumeration), because that is where a model can actually be biased.
// Reordering label_token_ids on the server would be meaningless: each label
// probability is computed independently at the same position, so the
// scoring lane is order-invariant BY CONSTRUCTION — a property worth
// demonstrating, not just claiming.
//
// Lane S (scoring):  flip-rate is expected to be 0 for any order-invariant
//
//	scorer; on a live model tiny numeric effects may appear.
//
// Lane G (generation): the mock carries a documented simulated primacy
//
//	bias, so ambiguous cases flip — exactly what permutation tests exist to
//	catch.
func runPermutation(ctx context.Context, scorer Scorer, live bool, cases []datasets.Case) error {
	subset := cases
	if len(subset) > 20 {
		subset = subset[:20] // measurement over a subset; 300x6 in mock buys nothing
	}
	gen, _ := newGenerator(cases)

	fmt.Printf("\n=== Permutation flip-rate over %d cases x 6 option orderings ===\n", len(subset))

	var scoreFlips, genFlips int
	for _, c := range subset {
		opts := optionsFor(c)

		scoreChoices := map[string]bool{}
		for _, p := range permutations6 {
			perm := permutedOptions(opts, p)
			dec, err := scorer.Score(ctx, BuildPrompt(c, perm), optionLetters, perm)
			if err != nil {
				return fmt.Errorf("score %s: %w", c.ID, err)
			}
			scoreChoices[dec.Choice] = true
		}
		if len(scoreChoices) > 1 {
			scoreFlips++
		}

		genChoices := map[string]bool{}
		for _, p := range permutations6 {
			perm := permutedOptions(opts, p)
			completion, _, err := gen.Generate(ctx, BuildPrompt(c, perm))
			if err != nil {
				return fmt.Errorf("generate %s: %w", c.ID, err)
			}
			letter := ParseChoice(completion)
			idx := -1
			for i, l := range optionLetters {
				if l == letter {
					idx = i
				}
			}
			if idx < 0 {
				genChoices["<no-parse>"] = true
				continue
			}
			genChoices[perm[idx]] = true
		}
		if len(genChoices) > 1 {
			genFlips++
		}
	}

	fmt.Printf("scoring lane    flip-rate: %.1f%%\n", 100*float64(scoreFlips)/float64(len(subset)))
	fmt.Printf("generation lane flip-rate: %.1f%%\n", 100*float64(genFlips)/float64(len(subset)))
	if !live {
		fmt.Println("interpretation: the mock scorer replays one probability vector per case, so 0% is order-invariance by construction; the mock generator carries a simulated primacy bias, so its flips show what a real order-sensitive model looks like.")
	} else {
		fmt.Println("interpretation: live numbers — compare against the blog's reported order-sensitivity discussion.")
	}
	fmt.Println("mitigation shipped: average-over-permutations (AverageOverPermutations below) — call it on any scorer whose flip-rate is non-zero, then gate the averaged distribution.")
	return nil
}

// AverageOverPermutations scores every option ordering and returns the mean
// distribution as a Decision — the standard mitigation for a model that is
// NOT order-invariant. Feed its output into Gate.Route like any decision.
func AverageOverPermutations(ctx context.Context, scorer Scorer, c datasets.Case) (*Decision, error) {
	opts := optionsFor(c)
	acc := map[string]float64{}
	for _, p := range permutations6 {
		perm := permutedOptions(opts, p)
		dec, err := scorer.Score(ctx, BuildPrompt(c, perm), optionLetters, perm)
		if err != nil {
			return nil, err
		}
		for k, v := range dec.Distribution {
			acc[k] += v / float64(len(permutations6))
		}
	}
	top, topP := "", -1.0
	for k, v := range acc {
		if v > topP {
			topP, top = v, k
		}
	}
	return &Decision{Choice: top, Distribution: acc, Margin: topP - second(acc), Entropy: entropy(acc)}, nil
}

func second(dist map[string]float64) float64 {
	best, scnd := -1.0, -1.0
	for _, v := range dist {
		if v >= best {
			scnd = best
			best = v
		} else if v > scnd {
			scnd = v
		}
	}
	if scnd < 0 {
		return 0
	}
	return scnd
}

func entropy(dist map[string]float64) float64 {
	var h float64
	for _, v := range dist {
		if v > 0 {
			h -= v * math.Log(v)
		}
	}
	return h
}
