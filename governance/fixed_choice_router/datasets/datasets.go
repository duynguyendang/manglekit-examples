// Package datasets provides the synthetic labeled routing fixtures for the
// fixed_choice_router example.
//
// IMPORTANT — what these numbers do and do not prove: the cases are generated
// by deterministic keyword rules plus a fixed profile rotation, and the mock
// scorer replays the stored probability vector. Accuracy measured on them
// proves the GATE MECHANISM routes/halts correctly; it says nothing about the
// quality of a real model. Real model numbers require a live scoring server
// (MANGLE_SCORER_BACKEND) — see README.md.
package datasets

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

// Domain identifiers for the three fixed-choice routing tasks (the same
// three the Jev-style blog benchmarks).
const (
	DomainSupport   = "support_routing"
	DomainScreening = "candidate_screening"
	DomainExpense   = "expense_review"
)

// Case is one labeled fixed-choice example: prompt text, the gold business
// option, and the probability vector the mock scorer replays.
type Case struct {
	ID     string             `json:"id"`
	Domain string             `json:"domain"`
	Text   string             `json:"text"`
	Gold   string             `json:"gold"`
	Probs  map[string]float64 `json:"probs"`
}

// Options returns the three business labels for a domain, in canonical
// option order (A, B, C).
func Options(domain string) []string {
	switch domain {
	case DomainSupport:
		return []string{"billing", "technical_support", "account_access"}
	case DomainScreening:
		return []string{"advance", "interview", "reject"}
	case DomainExpense:
		return []string{"approve", "flag_audit", "reject_expense"}
	default:
		panic("unknown domain: " + domain)
	}
}

// profile describes one case's difficulty shape. P is a canonical descending
// triple over the three options; Decoy means the argmax is NOT the gold
// option — a deliberately overconfident wrong read, which gives the
// calibration section real miscalibration to measure.
type profile struct {
	name  string
	p     [3]float64
	decoy bool
}

var profiles = [10]profile{
	{name: "confident", p: [3]float64{0.93, 0.05, 0.02}},
	{name: "confident", p: [3]float64{0.91, 0.07, 0.02}},
	{name: "confident", p: [3]float64{0.95, 0.03, 0.02}},
	{name: "confident", p: [3]float64{0.88, 0.09, 0.03}},
	{name: "review", p: [3]float64{0.74, 0.18, 0.08}},
	{name: "review", p: [3]float64{0.71, 0.20, 0.09}},
	{name: "ambiguous", p: [3]float64{0.55, 0.37, 0.08}},
	{name: "decoy", p: [3]float64{0.91, 0.06, 0.03}, decoy: true},
	{name: "escalate", p: [3]float64{0.44, 0.41, 0.15}},
	{name: "auto_margin", p: [3]float64{0.83, 0.14, 0.03}},
}

// ProfileAt exposes the difficulty profile of fixture index i (0..9 within
// the rotation) so tests and the tour can speak about case shapes.
func ProfileAt(i int) string { return profiles[i%len(profiles)].name }

// texts per domain per option. The keyword rule is the labeling law: gold ==
// the option whose phrasing the text is built from.
var texts = map[string]map[string][]string{
	DomainSupport: {
		"billing": {
			"I was charged twice for my subscription this month.",
			"My invoice shows tax I should not be paying.",
			"The refund for my cancelled plan never appeared.",
			"Can you prorate my plan after the downgrade?",
		},
		"technical_support": {
			"The mobile app crashes whenever I upload a photo.",
			"API returns 500 on the export endpoint since yesterday.",
			"Sync is stuck at 99 percent on all my devices.",
			"Webhook deliveries stopped after we rotated keys.",
		},
		"account_access": {
			"I cannot log in and the password reset email never arrives.",
			"Please delete my account under GDPR article 17.",
			"My account got locked after a suspicious sign-in abroad.",
			"I need to transfer account ownership to my co-founder.",
		},
	},
	DomainScreening: {
		"advance": {
			"Ten years in Go, shipped three payments SDKs, talks about tradeoffs.",
			"Led the migration to our exact stack, references check out deeply.",
			"Portfolio matches the role one-for-one with production scale proof.",
		},
		"interview": {
			"Four years backend, first time with our stack, strong fundamentals.",
			"Close enough profile, self-taught, needs a system design conversation.",
			"Good signals but one gap in required experience; worth a call.",
		},
		"reject": {
			"No relevant engineering experience; cover letter is generic spam.",
			"Five applications this week for five unrelated roles, no portfolio.",
			"Requirements missed on every axis listed in the posting.",
		},
	},
	DomainExpense: {
		"approve": {
			"Team lunch receipt, 84 dollars, four attendees, no alcohol.",
			"Flight booked two weeks ahead at the negotiated corporate rate.",
			"Conference registration paid directly to the organizer.",
		},
		"flag_audit": {
			"Client dinner of 610 dollars for two, purpose line left blank.",
			"Repeated same-vendor purchases just under the approval limit.",
			"Lodging over policy ceiling with a handwritten note only.",
		},
		"reject_expense": {
			"Personal gym membership submitted as an office supply.",
			"Weekend vacation night booked adjacent to the trip dates.",
			"Alcohol receipts from a personal birthday party.",
		},
	},
}

// Generate produces 3 domains x 100 cases deterministically: the gold option
// rotates with the case index, the difficulty profile rotates over the
// ten-entry cycle — no PRNG, so the committed fixtures.jsonl is exactly
// reproducible by `go run . --mode=gen-fixtures` (and a golden test pins it).
func Generate() []Case {
	domains := []string{DomainSupport, DomainScreening, DomainExpense}
	var out []Case
	for _, domain := range domains {
		opts := Options(domain)
		for i := 0; i < 100; i++ {
			gold := opts[i%len(opts)]
			prof := profiles[i%len(profiles)]
			pool := texts[domain][gold]
			base := pool[(i/len(opts))%len(pool)]

			// Assign the canonical descending triple: the peak sits on gold,
			// except for decoy profiles where it sits one option later (a
			// confident WRONG read whose gold keeps the second value).
			probs := map[string]float64{}
			peak := indexOf(opts, gold)
			if prof.decoy {
				peak = (peak + 1) % 3
			}
			order := make([]string, 3)
			for k := 0; k < 3; k++ {
				order[k] = opts[(peak+k)%3]
			}
			for k, name := range order {
				probs[name] = prof.p[k]
			}

			out = append(out, Case{
				ID:     fmt.Sprintf("%s-%03d", short(domain), i),
				Domain: domain,
				Text:   fmt.Sprintf("%s (ref %s-%03d)", base, short(domain), i),
				Gold:   gold,
				Probs:  probs,
			})
		}
	}
	return out
}

func short(domain string) string {
	parts := strings.Split(domain, "_")
	return string(parts[0][0]) + strings.ToUpper(string(parts[1][0]))
}

func indexOf(hay []string, needle string) int {
	for i, v := range hay {
		if v == needle {
			return i
		}
	}
	return -1
}

// WriteJSONL persists the generated cases.
func WriteJSONL(path string, cases []Case) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	for _, c := range cases {
		if err := enc.Encode(c); err != nil {
			return err
		}
	}
	return nil
}

// ReadJSONL loads committed fixtures from path.
func ReadJSONL(path string) ([]Case, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []Case
	dec := json.NewDecoder(f)
	for dec.More() {
		var c Case
		if err := dec.Decode(&c); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, nil
}
