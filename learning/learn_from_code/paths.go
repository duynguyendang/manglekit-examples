package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// resolveLearnPaths expands --learn inputs into a deterministic file list
// (docs/plans/active/2026-09-11-full-tree-learn.md D1):
//
//   - no inputs            → the current working directory (LEARN = the tree
//     you are in; a pool built from pasted snippets is incomplete material)
//   - directory input      → recursive walk of *.go, ALWAYS excluding
//     *_test.go, any testdata/vendor/.git-or-other-dot path segment
//   - explicit .go file    → included as given: exclusions apply to tree
//     WALKS, not to paths the user named deliberately (surgical subset mode,
//     used by tests and re-runs — D2)
//   - result               → sorted + deduped (stable hashing), case where
//     the walk yields zero .go files fails closed: nothing gets written.
//   - each target carries a DISPLAY path: files found under a directory root
//     are shown root-relatively (adapters/x.go, not ../../manglekit/...), so
//     pool provenance + hashes are location-stable across clones, CI
//     checkouts, and repo reorganizations.
type learnTarget struct {
	Path    string // as-opened path
	Display string // provenance path (root-relative for walked files)
}

func resolveLearnPaths(inputs []string) ([]learnTarget, error) {
	if len(inputs) == 0 {
		inputs = []string{"."}
	}
	set := map[string]learnTarget{}
	for _, in := range inputs {
		fi, err := os.Stat(in)
		if err != nil {
			return nil, fmt.Errorf("learn path %q: %w", in, err)
		}
		if fi.IsDir() {
			walkErr := filepath.WalkDir(in, func(p string, d fs.DirEntry, err error) error {
				if err != nil {
					return err
				}
				if d.IsDir() {
					if excludedDirSegment(d.Name()) {
						return fs.SkipDir
					}
					return nil
				}
				if treeExcluded(p) {
					return nil
				}
				full := filepath.ToSlash(p)
				display := full
				if rel, rerr := filepath.Rel(in, p); rerr == nil && !strings.HasPrefix(rel, "..") {
					display = filepath.ToSlash(rel)
				}
				set[display] = learnTarget{Path: full, Display: display}
				return nil
			})
			if walkErr != nil {
				return nil, fmt.Errorf("walk %q: %w", in, walkErr)
			}
			continue
		}
		if !strings.HasSuffix(in, ".go") {
			return nil, fmt.Errorf("learn input %q is neither a directory nor a .go file", in)
		}
		full := filepath.ToSlash(in)
		set[full] = learnTarget{Path: full, Display: full}
	}
	if len(set) == 0 {
		return nil, fmt.Errorf("no .go files found under: %s", strings.Join(inputs, ", "))
	}
	// Dedup by REAL path (an explicit file inside a walked dir appears
	// twice); prefer the shorter root-relative Display.
	byPath := map[string]learnTarget{}
	for _, t := range set {
		key := filepath.Clean(t.Path)
		if prev, ok := byPath[key]; !ok || len(t.Display) < len(prev.Display) {
			byPath[key] = t
		}
	}
	out := make([]learnTarget, 0, len(byPath))
	for _, t := range byPath {
		out = append(out, t)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Display < out[j].Display })
	return out, nil
}

// excludedDirSegment names that are never part of a tree walk (dot-dirs
// cover .git and friends). "." and ".." are path-resolution artifacts
// (relative roots like ../../manglekit), not hidden directories.
func excludedDirSegment(name string) bool {
	switch name {
	case "testdata", "vendor":
		return true
	case ".", "..":
		return false
	}
	return strings.HasPrefix(name, ".")
}

// treeExcluded reports whether a walked file path is excluded.
func treeExcluded(path string) bool {
	if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
		return true
	}
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if excludedDirSegment(seg) {
			return true
		}
	}
	return false
}
