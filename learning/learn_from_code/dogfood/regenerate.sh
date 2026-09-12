#!/usr/bin/env bash
# Regenerate the reviewed candidate pool from the manglekit kernel sources.
# The pool (dogfood/genes.yaml) is a REVIEWED ARTIFACT: it is the committed
# record of "which code-hygiene lessons the kernel's current sources induce".
# Since the full-tree-learn plan (2026-09-11) the CLI itself walks the tree
# deterministically (exclusions: *_test.go, testdata/, vendor/, dot-dirs),
# so this script is a thin wrapper — no duplicate find logic, portable.
#
# Usage: regenerate.sh [KERNEL_DIR] [OUT_DIR]
#   defaults: ../../../manglekit  ./dogfood   (sibling mangle-project layout)
set -euo pipefail
cd "$(dirname "$0")/.."
go run . --learn "${1:-../../../manglekit}" --out "${2:-dogfood}"
