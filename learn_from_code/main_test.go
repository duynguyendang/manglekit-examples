package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/duynguyendang/manglekit/core"
	"github.com/duynguyendang/manglekit/x/genes"
)

const snippet = "./testdata/snippet.go"

func TestIntentRouter(t *testing.T) {
	cases := []struct {
		name   string
		cfg    config
		want   Mode
		shadow bool
	}{
		{"no args defaults to STATUS", config{}, ModeStatus, false},
		{"explicit status", config{status: true}, ModeStatus, false},
		{"code only learns", config{learn: []string{snippet}}, ModeLearn, false},
		{"eval intent only", config{input: snippet}, ModeEval, false},
		{"signals only", config{signals: []string{"a"}}, ModeEval, false},
		{"ambiguous learns then shadows eval", config{learn: []string{snippet}, eval: true}, ModeLearn, true},
		{"promote wins over code", config{learn: []string{snippet}, promote: true}, ModePromote, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := classify(&tc.cfg)
			if d.Primary != tc.want {
				t.Errorf("Primary = %s, want %s", d.Primary, tc.want)
			}
			if d.ShadowEval != tc.shadow {
				t.Errorf("ShadowEval = %v, want %v", d.ShadowEval, tc.shadow)
			}
		})
	}
}

func TestExtractSignalsDeterministic(t *testing.T) {
	ex, err := extractSignals([]string{snippet})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"destructive_call", "direct_db_access", "exec_import", "hardcoded_secret"}
	if strings.Join(ex.Keys, ",") != strings.Join(want, ",") {
		t.Errorf("keys = %v, want %v", ex.Keys, want)
	}
	if len(ex.SHAC8()) != 8 {
		t.Errorf("bad sha8 %q", ex.SHAC8())
	}
	clean, err := extractSignals([]string{"./testdata/clean.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(clean.Keys) != 0 {
		t.Errorf("clean fixture produced signals: %v", clean.Keys)
	}
}

func runCLI(t *testing.T, args ...string) string {
	t.Helper()
	cfg, err := parseFlags(args)
	if err != nil {
		t.Fatalf("parseFlags(%v): %v", args, err)
	}
	var buf bytes.Buffer
	if err := run(context.Background(), &buf, cfg); err != nil {
		t.Fatalf("run(%v): %v", args, err)
	}
	return buf.String()
}

func TestLearnStatusEvalAdvisory(t *testing.T) {
	dir := t.TempDir()
	out := runCLI(t, "--learn", snippet, "--out", dir)
	if !strings.Contains(out, "intent=LEARN") {
		t.Fatalf("missing intent:\n%s", out)
	}
	pool, err := genes.LoadPoolFile(filepath.Join(dir, poolFile))
	if err != nil {
		t.Fatal(err)
	}
	advisory, hard := splitByTier(pool.Genes())
	if len(hard) != 0 {
		t.Fatalf("LEARN produced hard-tier genes: %v", hard)
	}
	if len(advisory) != 2 {
		t.Fatalf("want T3 signals + T2 composite, got %d", len(advisory))
	}
	var composite, perSig genes.Gene
	for _, g := range advisory {
		if g.Tier == core.TierT2_Playbook {
			composite = g
		} else {
			perSig = g
		}
	}
	if composite.Name == "" || perSig.Name == "" {
		t.Fatal("missing expected gene pair")
	}
	if !strings.Contains(perSig.Source, "learn_from_code@") {
		t.Errorf("no provenance in source: %s", perSig.Source)
	}
	if err := perSig.Verify(); err != nil {
		t.Errorf("gene not signed: %v", err)
	}

	out = runCLI(t, "--status", "--out", dir)
	for _, want := range []string{perSig.Name, composite.Name, "tier=T3", "tier=T2"} {
		if !strings.Contains(out, want) {
			t.Errorf("STATUS missing %q:\n%s", want, out)
		}
	}

	// Advisory-only application: the supervised action still runs.
	out = runCLI(t, "--eval", "--input", snippet, "--out", dir)
	if !strings.Contains(out, "verdict: ALLOW") {
		t.Fatalf("advisory genes must not block:\n%s", out)
	}
}

func TestPromoteGuardrailAndEffect(t *testing.T) {
	dir := t.TempDir()
	runCLI(t, "--learn", snippet, "--out", dir)
	pool, err := genes.LoadPoolFile(filepath.Join(dir, poolFile))
	if err != nil {
		t.Fatal(err)
	}
	composite := ""
	for _, g := range pool.Genes() {
		if g.Tier == core.TierT2_Playbook {
			composite = g.Name
		}
	}

	// Without --confirm: refused, pool untouched.
	out := runCLI(t, "--promote", "--gene", composite, "--out", dir)
	if !strings.Contains(out, "REFUSED") {
		t.Fatalf("expected refusal:\n%s", out)
	}
	pool2, _ := genes.LoadPoolFile(filepath.Join(dir, poolFile))
	_, hard := splitByTier(pool2.Genes())
	if len(hard) != 0 {
		t.Fatal("unconfirmed promote changed the pool")
	}

	// With --confirm: T1 gene appears and the same input now DENIES.
	out = runCLI(t, "--promote", "--gene", composite, "--confirm", "--out", dir)
	if !strings.Contains(out, "promoted:") || !strings.Contains(out, "verdict: DENY") {
		t.Fatalf("promote+re-eval failed:\n%s", out)
	}

	// Safety on the plain path: EVAL without --confirm skips the hard gene
	// and the input is allowed again — the human opt-in is mandatory.
	out = runCLI(t, "--eval", "--input", snippet, "--out", dir)
	if !strings.Contains(out, "skipping hard-tier gene") || !strings.Contains(out, "verdict: ALLOW") {
		t.Fatalf("EVAL must skip hard genes without --confirm:\n%s", out)
	}
}

func TestAmbiguousInputShadowEval(t *testing.T) {
	dir := t.TempDir()
	out := runCLI(t, "--learn", snippet, "--eval", "--signals", "exec_import,destructive_call", "--out", dir)
	if !strings.Contains(out, "intent=LEARN") || !strings.Contains(out, "shadow EVAL") {
		t.Fatalf("ambiguous routing wrong:\n%s", out)
	}
	if !strings.Contains(out, "verdict: ALLOW") {
		t.Fatalf("shadow eval must run advisory:\n%s", out)
	}
	// Shadow must not apply anything twice — pool still has 2 advisory genes.
	pool, err := genes.LoadPoolFile(filepath.Join(dir, poolFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(pool.Genes()) != 2 {
		t.Fatalf("pool should hold exactly the 2 candidates, got %d", len(pool.Genes()))
	}
}

func TestLearnFailClosed(t *testing.T) {
	cfg, err := parseFlags([]string{"--learn", "./testdata/clean.go", "--out", t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	err = run(context.Background(), &bytes.Buffer{}, cfg)
	if err == nil || !strings.Contains(err.Error(), "nothing written") {
		t.Fatalf("expected fail-closed error, got %v", err)
	}
	if _, statErr := os.Stat(filepath.Join(cfg.poolDir, poolFile)); !os.IsNotExist(statErr) {
		t.Fatal("pool file must not exist after failed learn")
	}
}

func TestUnknownFlagRejected(t *testing.T) {
	if _, err := parseFlags([]string{"--frobnicate"}); err == nil {
		t.Fatal("unknown flag must fail closed")
	}
}

func TestExtractMethodFormDestructive(t *testing.T) {
	ex, err := extractSignals([]string{"./testdata/destructive_methods.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Keys) != 1 || ex.Keys[0] != "destructive_call" {
		t.Fatalf("method-form receivers must be detected, got %v", ex.Keys)
	}
}

func TestExtractIdentityScopedToSignalFiles(t *testing.T) {
	withClean, err := extractSignals([]string{"./testdata/clean.go", snippet})
	if err != nil {
		t.Fatal(err)
	}
	only, err := extractSignals([]string{snippet})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(withClean.Files, ",") != "./testdata/snippet.go" {
		t.Errorf("Files must list only signal-bearing files, got %v", withClean.Files)
	}
	if withClean.SHAC8() != only.SHAC8() {
		t.Error("candidate identity must not churn on unrelated clean files")
	}
}

func TestExtractSingleImportForm(t *testing.T) {
	ex, err := extractSignals([]string{"./testdata/single_import.go"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ex.Keys) != 1 || ex.Keys[0] != "exec_import" {
		t.Fatalf("single-form import must be detected, got %v", ex.Keys)
	}
}
