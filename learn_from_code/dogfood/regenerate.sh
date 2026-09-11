#!/usr/bin/env bash
# Regenerate the reviewed candidate pool from the manglekit kernel sources.
# The pool (dogfood/genes.yaml) is a REVIEWED ARTIFACT: it is the committed
# record of "which code-hygiene lessons the kernel's current signals induce".
# Deterministic: extraction keys + SHA identity depend only on signal-bearing
# files (sorted), so unrelated edits never churn it.
set -euo pipefail
cd "$(dirname "$0")/.."
KERNEL_DIR="${1:-../../manglekit}"
OUT_DIR="${2:-dogfood}"
mapfile -t FILES < <(find "$KERNEL_DIR" -name '*.go' \
    ! -name '*_test.go' ! -path '*/testdata/*' ! -path '*/.git/*' | sort)
if [ "${#FILES[@]}" -eq 0 ]; then
    echo "no sources found under $KERNEL_DIR" >&2; exit 1
fi
go run . --learn "${FILES[@]}" --out "$OUT_DIR"
