#!/usr/bin/env bash
# CI gate: the pool is a golden REVIEWED artifact (like go.sum / golden
# files). If kernel sources newly trip or change a code signal, the pool
# must be regenerated, reviewed, and committed in the SAME PR. The committed
# pool is never mutated by this check; the diff printed IS the pending review.
set -euo pipefail
cd "$(dirname "$0")/.."
if [ ! -f dogfood/genes.yaml ]; then
    echo "missing dogfood/genes.yaml — run ./dogfood/regenerate.sh" >&2; exit 1
fi
FRESH="$(mktemp -d)"
trap 'rm -rf "$FRESH"' EXIT
bash dogfood/regenerate.sh "${1:-../../manglekit}" "$FRESH" >/dev/null
if ! diff -u dogfood/genes.yaml "$FRESH/genes.yaml"; then
    echo "POLICY POOL DRIFT: kernel code signals changed without a pool review."
    echo "Fix: ./dogfood/regenerate.sh, review the diff, commit the updated pool."
    exit 1
fi
echo "policy pool up to date"
