package main

import (
	"context"
	"fmt"
	"math"
	"os"
	"text/tabwriter"

	"github.com/duynguyendang/manglekit-examples/governance/fixed_choice_router/datasets"
)

// calObservation is one scored case in canonical option order: the
// restricted-softmax probability vector and the index of the gold option.
type calObservation struct {
	probs   []float64
	goldIdx int
}

// runCalibration measures how well the reported confidences track reality:
//
//   - reliability table + Expected Calibration Error (ECE) over 10 bins
//   - temperature scaling: a SINGLE scalar T fitted by grid search over
//     [0.5, 3.0] to minimize mean NLL on the labeled set — deliberately the
//     simple version: one scalar, nothing that could overfit a 300-case demo.
//
// Why this earns its place: policy.dl compares thresholds against these
// probabilities, and a gate on an uncalibrated number is a gate on vibes.
// This is the direct answer to the calibration-drift comment under the Jev
// blog. The report names the scorer: T is a property of (model,
// quantization, template), so it must be re-fit per backend — never shipped
// from one to another.
func runCalibration(ctx context.Context, scorer Scorer, live bool, cases []datasets.Case) error {
	obs := make([]calObservation, 0, len(cases))
	for _, c := range cases {
		opts := optionsFor(c)
		dec, err := scorer.Score(ctx, BuildPrompt(c, opts), optionLetters, opts)
		if err != nil {
			return fmt.Errorf("score %s: %w", c.ID, err)
		}
		p := make([]float64, len(opts))
		goldIdx := -1
		for i, o := range opts {
			p[i] = dec.Distribution[o]
			if o == c.Gold {
				goldIdx = i
			}
		}
		if goldIdx < 0 {
			return fmt.Errorf("case %s: gold %q not among options", c.ID, c.Gold)
		}
		obs = append(obs, calObservation{probs: p, goldIdx: goldIdx})
	}

	rawECE := ece(rawEntries(obs))
	bestT, bestNLL := fitTemperature(obs)

	fmt.Printf("\n=== Calibration of the %s scorer over %d cases ===\n", calModeWord(live), len(cases))
	w := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(w, "bin\tcount\tavg conf\taccuracy")
	for _, row := range reliabilityTable(rawEntries(obs)) {
		fmt.Fprintf(w, "%.1f-%.1f\t%d\t%.3f\t%.3f\n", row.lo, row.hi, row.count, row.conf, row.acc)
	}
	if err := w.Flush(); err != nil {
		return err
	}
	fmt.Printf("ECE raw        = %.4f\n", rawECE)
	fmt.Printf("fitted T       = %.2f (grid [0.5, 3.0], min mean NLL)\n", bestT)
	fmt.Printf("ECE at T       = %.4f\n", ece(scaledEntriesAt(obs, bestT)))
	fmt.Printf("mean NLL at T  = %.4f (raw: %.4f)\n", bestNLL, meanNLL(obs, 1.0))
	fmt.Println("cross-backend warning: T is a property of (model, quantization, template). Re-fit it per backend — never ship one T from Qwen to DeepSeek, and never auto-route on an uncalibrated number.")
	if !live {
		fmt.Println("note: the mock replays fixture constants — these numbers validate the calibration CODE, not any model. Live mode is where real drift shows.")
	}
	return nil
}

// calEntry: confidence of the top choice and whether the top choice is gold.
type calEntry struct {
	conf    float64
	correct bool
}

func rawEntries(obs []calObservation) []calEntry {
	return scaledEntriesAt(obs, 1.0)
}

func scaledEntriesAt(obs []calObservation, t float64) []calEntry {
	out := make([]calEntry, 0, len(obs))
	for _, o := range obs {
		s := softmaxScaled(o.probs, t)
		top := argmax(o.probs)
		out = append(out, calEntry{conf: s[top], correct: top == o.goldIdx})
	}
	return out
}

func fitTemperature(obs []calObservation) (float64, float64) {
	bestT, bestNLL := 1.0, math.Inf(1)
	for x := 0.50; x <= 3.001; x += 0.05 {
		v := meanNLL(obs, x)
		if v < bestNLL-1e-12 {
			bestNLL, bestT = v, x
		}
	}
	return bestT, bestNLL
}

// meanNLL scores the gold class under the temperature-scaled distribution —
// decoy cases (confidently wrong) are penalized by how much mass went to
// the wrong peak.
func meanNLL(obs []calObservation, t float64) float64 {
	if len(obs) == 0 {
		return math.Inf(1)
	}
	var sum float64
	for _, o := range obs {
		s := softmaxScaled(o.probs, t)
		p := s[o.goldIdx]
		if p <= 0 {
			p = 1e-12
		}
		sum += -math.Log(p)
	}
	return sum / float64(len(obs))
}

func softmaxScaled(p []float64, t float64) []float64 {
	logs := make([]float64, len(p))
	for i, v := range p {
		if v <= 0 {
			v = 1e-12
		}
		logs[i] = math.Log(v) / t
	}
	mx := logs[0]
	for _, v := range logs {
		if v > mx {
			mx = v
		}
	}
	out := make([]float64, len(p))
	sum := 0.0
	for i, v := range logs {
		out[i] = math.Exp(v - mx)
		sum += out[i]
	}
	for i := range out {
		out[i] /= sum
	}
	return out
}

func argmax(p []float64) int {
	best := 0
	for i, v := range p {
		if v > p[best] {
			best = i
		}
	}
	return best
}

type binRow struct {
	lo, hi    float64
	count     int
	conf, acc float64
}

func reliabilityTable(entries []calEntry) []binRow {
	const bins = 10
	rows := make([]binRow, bins)
	sums := make([][2]float64, bins)
	for i := range rows {
		rows[i].lo = float64(i) / bins
		rows[i].hi = float64(i+1) / bins
	}
	for _, e := range entries {
		b := int(e.conf * bins)
		if b >= bins {
			b = bins - 1
		}
		rows[b].count++
		sums[b][0] += e.conf
		if e.correct {
			sums[b][1]++
		}
	}
	for i := range rows {
		if rows[i].count > 0 {
			rows[i].conf = sums[i][0] / float64(rows[i].count)
			rows[i].acc = sums[i][1] / float64(rows[i].count)
		}
	}
	return rows
}

func ece(entries []calEntry) float64 {
	if len(entries) == 0 {
		return 0
	}
	const bins = 10
	counts := make([]int, bins)
	confSum := make([]float64, bins)
	accSum := make([]float64, bins)
	for _, e := range entries {
		b := int(e.conf * bins)
		if b >= bins {
			b = bins - 1
		}
		counts[b]++
		confSum[b] += e.conf
		if e.correct {
			accSum[b]++
		}
	}
	var err float64
	for b := 0; b < bins; b++ {
		if counts[b] == 0 {
			continue
		}
		avgConf := confSum[b] / float64(counts[b])
		avgAcc := accSum[b] / float64(counts[b])
		err += float64(counts[b]) / float64(len(entries)) * math.Abs(avgConf-avgAcc)
	}
	return err
}

func calModeWord(live bool) string {
	if live {
		return "live"
	}
	return "mock (fixture-replay)"
}
