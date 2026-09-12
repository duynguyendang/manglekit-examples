package main

import (
	"fmt"
	"strings"
)

// Mode is the skill intent (UC-L8 router table).
type Mode string

const (
	ModeLearn   Mode = "LEARN"
	ModeEval    Mode = "EVAL"
	ModePromote Mode = "PROMOTE"
	ModeStatus  Mode = "STATUS"
)

// config is the fully-parsed command line.
type config struct {
	learn     []string // code paths for LEARN (files and/or dirs)
	learnSeen bool     // --learn passed, even with no paths (= cwd tree)
	eval      bool     // --eval
	input     string   // --input <file.go> (re-extract signals at EVAL)
	signals   []string // --signals a,b,c
	promote   bool     // --promote
	gene      string   // --gene <name>
	confirm   bool     // --confirm (human review acknowledgment)
	status    bool     // --status
	pool      string   // --pool <file.yaml> override
	poolDir   string   // --out <dir>, default ./learned-pool
}

// classify is the deterministic intent router. Rules (docs/use-cases/learning
// .md UC-L8 "Intent router"):
//
//	--status / no args            → STATUS
//	--promote                     → PROMOTE
//	code paths AND eval intent    → LEARN first, then a SHADOW EVAL
//	eval intent only              → EVAL
//	code paths only               → LEARN
//
// Shadow EVAL never applies anything; ambiguity always prints its choice.
func classify(cfg *config) struct {
	Primary    Mode
	ShadowEval bool
	Reason     string
} {
	type d = struct {
		Primary    Mode
		ShadowEval bool
		Reason     string
	}
	hasCode := cfg.learnSeen || len(cfg.learn) > 0
	hasEvalIntent := cfg.eval || cfg.input != "" || len(cfg.signals) > 0

	switch {
	case cfg.status:
		return d{ModeStatus, false, "explicit --status"}
	case !hasCode && !hasEvalIntent && !cfg.promote:
		return d{ModeStatus, false, "no input — default to STATUS"}
	case cfg.promote:
		return d{ModePromote, false, "explicit --promote (human-gated)"}
	case hasCode && hasEvalIntent:
		return d{ModeLearn, true, "ambiguous (code + eval intent): LEARN the tree first, shadow EVAL on the candidate, nothing auto-applied"}
	case hasCode:
		return d{ModeLearn, false, "code tree/file input present"}
	default:
		return d{ModeEval, false, "eval intent without code input"}
	}
}

// parseFlags is a tiny hand-rolled parser (flag package can't mix repeated
// free args this cleanly); unknown flags fail closed.
func parseFlags(args []string) (*config, error) {
	cfg := &config{poolDir: defaultPoolDir}
	consumePaths := func() []string {
		var out []string
		for len(args) > 0 && !strings.HasPrefix(args[0], "-") {
			out = append(out, args[0])
			args = args[1:]
		}
		return out
	}
	for len(args) > 0 {
		a := args[0]
		args = args[1:]
		switch a {
		case "--learn", "-l":
			// No paths = the tree you are in (default whole-source LEARN).
			cfg.learnSeen = true
			cfg.learn = append(cfg.learn, consumePaths()...)
		case "--eval", "-e":
			cfg.eval = true
		case "--input":
			if len(args) == 0 {
				return nil, fmt.Errorf("--input needs a path")
			}
			cfg.input, args = args[0], args[1:]
		case "--signals":
			if len(args) == 0 {
				return nil, fmt.Errorf("--signals needs a comma list")
			}
			for _, s := range strings.Split(args[0], ",") {
				if s = strings.TrimSpace(s); s != "" {
					cfg.signals = append(cfg.signals, s)
				}
			}
			args = args[1:]
		case "--promote":
			cfg.promote = true
		case "--gene":
			if len(args) == 0 {
				return nil, fmt.Errorf("--gene needs a name")
			}
			cfg.gene, args = args[0], args[1:]
		case "--confirm":
			cfg.confirm = true
		case "--status":
			cfg.status = true
		case "--pool":
			if len(args) == 0 {
				return nil, fmt.Errorf("--pool needs a file path")
			}
			cfg.pool, args = args[0], args[1:]
		case "--out":
			if len(args) == 0 {
				return nil, fmt.Errorf("--out needs a directory")
			}
			cfg.poolDir, args = args[0], args[1:]
		default:
			return nil, fmt.Errorf("unknown flag %q", a)
		}
	}
	return cfg, nil
}
