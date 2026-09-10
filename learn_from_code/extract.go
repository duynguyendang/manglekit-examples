package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// extracted is the deterministic result of scanning code files.
type extracted struct {
	Files []string
	Keys  []string // sorted, deduplicated signal keys
	hash  string   // full sha256 over canonical form
}

// SHAC8 is the provenance fingerprint embedded in gene names/sources.
func (e extracted) SHAC8() string {
	if len(e.hash) >= 8 {
		return e.hash[:8]
	}
	return e.hash
}

type signalDef struct {
	key   string
	regex *regexp.Regexp
}

// v1 extraction is deliberately deterministic regex/scan — no LLM, no AST
// dependency. LLM-assisted extraction (adapters/extractor) is the documented
// upgrade path behind the same keys (docs/use-cases/learning.md UC-L8).
var signalDefs = []signalDef{
	{"exec_import", regexp.MustCompile(`(?m)^\s*_?\s*"os/exec"`)},
	{"direct_db_access", regexp.MustCompile(`(?m)^\s*_?\s*"database/sql"`)},
	{"hardcoded_secret", regexp.MustCompile(`(?i)(password|secret|token|api_?key)\s*(:=|:|=)\s*"[^"\s]{8,}"`)},
	{"destructive_call", regexp.MustCompile(`(?i)\bfunc\s+\w*(delete|drop|truncate|purge|erase)\w*\s*\(`)},
}

// SignalMessage maps an extracted signal to its advisory halt text.
func SignalMessage(key string) string {
	switch key {
	case "exec_import":
		return "code shells out via os/exec — review command construction for injection"
	case "direct_db_access":
		return "code talks to the database directly — review for missing guards/migrations"
	case "hardcoded_secret":
		return "hardcoded credential detected — move to a secret store"
	case "destructive_call":
		return "destructive bulk operation — require an explicit approval path"
	default:
		return "unclassified code signal"
	}
}

// signalSeverity orders keys deterministically for composite-rule picking
// (most dangerous first). Unknown keys sort last, then alphabetically.
var signalSeverity = map[string]int{
	"exec_import": 0, "destructive_call": 1,
	"hardcoded_secret": 2, "direct_db_access": 3,
}

func extractSignals(paths []string) (extracted, error) {
	seen := map[string]bool{}
	var keys []string
	var h = sha256.New()

	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			return extracted{}, fmt.Errorf("read %s: %w", p, err)
		}
		fmt.Fprintf(h, "file:%s\n", p)
		text := string(src)
		fmt.Fprintf(h, "content:%x\n", sha256.Sum256(src))
		for _, def := range signalDefs {
			if def.regex.MatchString(text) && !seen[def.key] {
				seen[def.key] = true
				keys = append(keys, def.key)
			}
		}
	}
	sort.Strings(keys)
	return extracted{Files: append([]string(nil), paths...), Keys: keys, hash: hex.EncodeToString(h.Sum(nil))}, nil
}

// bySeverity sorts keys most-severity-first, deterministic tie-break.
func bySeverity(keys []string) []string {
	out := append([]string(nil), keys...)
	sort.SliceStable(out, func(i, j int) bool {
		si, sj := signalSeverity[out[i]], signalSeverity[out[j]]
		if si != sj {
			return si < sj
		}
		return strings.Compare(out[i], out[j]) < 0
	})
	return out
}
