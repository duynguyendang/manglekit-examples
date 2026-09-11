package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeGo(t *testing.T, root, rel string) string {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("package p\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func relSlash(t *testing.T, root string, files []string) []string {
	t.Helper()
	var out []string
	for _, f := range files {
		r, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(r))
	}
	return out
}

func TestResolveLearnPathsWalkAndExclusions(t *testing.T) {
	root := t.TempDir()
	writeGo(t, root, "a.go")
	writeGo(t, root, "pkg/sub/c.go")        // nested package must be found
	writeGo(t, root, "pkg/deep.go")         //
	writeGo(t, root, "pkg/a_test.go")       // excluded: *_test.go
	writeGo(t, root, "testdata/fixture.go") // excluded: testdata segment
	writeGo(t, root, "vendor/lib/x.go")     // excluded: vendor segment
	writeGo(t, root, ".git/hooks/h.go")     // excluded: dot dir
	writeGo(t, root, "notes.txt")           // excluded: not .go

	got, err := resolveLearnPaths([]string{root})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"a.go", "pkg/deep.go", "pkg/sub/c.go"}
	if strings.Join(relSlash(t, root, got), ",") != strings.Join(want, ",") {
		t.Errorf("walk = %v, want %v", relSlash(t, root, got), want)
	}
}

func TestResolveLearnPathsExplicitFileBypassesWalkExclusions(t *testing.T) {
	// Explicitly named files are a deliberate subset (surgical re-runs /
	// tests) — D2 — so walk exclusions do not filter them.
	p := writeGo(t, t.TempDir(), "x_test.go")
	got, err := resolveLearnPaths([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != filepath.ToSlash(p) {
		t.Fatalf("explicit file must pass through, got %v", got)
	}
}

func TestResolveLearnPathsUnionSortedDeduped(t *testing.T) {
	root := t.TempDir()
	file := writeGo(t, root, "z.go")
	writeGo(t, root, "a.go")
	// same path thrice (explicit + via its dir + explicit again) → one entry
	got, err := resolveLearnPaths([]string{file, root, file})
	if err != nil {
		t.Fatal(err)
	}
	rel := relSlash(t, root, got)
	if len(rel) != 2 || rel[0] != "a.go" || rel[1] != "z.go" {
		t.Fatalf("union = %v, want sorted [a.go z.go] deduped", rel)
	}
}

func TestResolveLearnPathsFailClosed(t *testing.T) {
	empty := t.TempDir()
	if _, err := resolveLearnPaths([]string{empty}); err == nil {
		t.Fatal("tree with zero .go files must error (nothing written)")
	}
	notGo := filepath.Join(empty, "readme.md")
	if err := os.WriteFile(notGo, []byte("hi"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveLearnPaths([]string{notGo}); err == nil {
		t.Fatal("non-.go explicit input must error")
	}
	if _, err := resolveLearnPaths([]string{filepath.Join(empty, "nope")}); err == nil {
		t.Fatal("missing path must error")
	}
}

func TestResolveLearnPathsDefaultCwd(t *testing.T) {
	// Tests run with cwd = the package dir: the default root must resolve to
	// this package's own .go files (walk rules apply; _test.go excluded).
	got, err := resolveLearnPaths(nil)
	if err != nil {
		t.Fatal(err)
	}
	var hasMain, hasTest bool
	for _, f := range got {
		base := filepath.Base(f)
		if base == "main.go" {
			hasMain = true
		}
		if strings.HasSuffix(base, "_test.go") {
			hasTest = true
		}
		if strings.Contains(f, "testdata/") {
			t.Errorf("testdata leaked into walk: %s", f)
		}
	}
	if !hasMain {
		t.Errorf("default cwd walk must find ./main.go, got %v", got)
	}
	if hasTest {
		t.Error("*_test.go must never enter a tree walk")
	}
}
