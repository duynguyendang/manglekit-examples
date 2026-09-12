package main

import (
	"fmt"
	"strings"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/x/genes"
)

// induceGenes turns extracted signals into signed CANDIDATE genes.
//
// Tier policy (learning-engine-design guardrails):
//   - one T3 gene holding per-signal advisory rules (user-tier: learned from
//     unaudited code, lowest trust);
//   - plus one T2 composite gene when ≥2 signals co-occur (playbook-tier:
//     "this combination deserves review").
//
// Nothing here is ever emitted at T0/T1 — that is PROMOTE's exclusive,
// human-confirmed job. Rules carry no Decl: the shared predicate is declared
// once by the app's base policy so genes compose without re-declaration.
func induceGenes(ex extracted) []genes.Gene {
	source := fmt.Sprintf("learn_from_code@%s %s", ex.SHAC8(), strings.Join(ex.Files, " "))
	names := bySeverity(ex.Keys)

	var t3 strings.Builder
	for _, k := range names {
		fmt.Fprintf(&t3, "halt(\"Req\", %q, \"T3\") :- code_signal(\"Req\", %q).\n",
			SignalMessage(k), k)
	}
	perSignal := genes.Gene{
		Name:    fmt.Sprintf("lc-%s-signals", ex.SHAC8()),
		Tier:    core.TierT3_User,
		Source:  source,
		Intents: []string{gateAction},
		Rules:   t3.String(),
	}
	perSignal.Sign()
	out := []genes.Gene{perSignal}

	if len(names) >= 2 {
		// Composite lesson: the two most severe co-occurring signals.
		a, b := names[0], names[1]
		composite := genes.Gene{
			Name:    fmt.Sprintf("lc-%s-review", ex.SHAC8()),
			Tier:    core.TierT2_Playbook,
			Source:  source,
			Intents: []string{gateAction},
			Rules: fmt.Sprintf("halt(\"Req\", %q, \"T2\") :- code_signal(\"Req\", %q), code_signal(\"Req\", %q).\n",
				fmt.Sprintf("risk combination %q + %q requires human approval before merge", a, b), a, b),
		}
		composite.Sign()
		out = append(out, composite)
	}
	return out
}
