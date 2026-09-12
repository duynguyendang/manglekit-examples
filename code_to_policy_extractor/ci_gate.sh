#!/usr/bin/env bash
# CI policy gate for architecture violations — the exit-code contract demo.
#
# Usage: ./ci_gate.sh [path/to/pr-graph.nt]
#   pr-graph.nt: N-Triples facts describing the PR files, e.g.
#     <file1> <file_path> "controllers/" .
#     <file1> <file_imports> "domain/" .
#
# Exit codes (manglekit v0.9 contract): 0 = PR clean, 1 = policy-deny,
# 2 = usage error, 3 = runtime error. Wire this directly into CI:
#
#   - run: manglekit-examples/code_to_policy_extractor/ci_gate.sh pr.nt
#     continue-on-error: true   # then read steps.outputs / exit status
set -euo pipefail
cd "$(dirname "$0")"
FACTS="${1:-testdata_clean.pr-graph.nt}"

POLICY="$(mktemp).dl"
go run . --emit-policy "$POLICY"

# Build mkit from inside the kernel module (sibling layout; override with
# MANGLEKIT_DIR). The examples module does not carry the CLI's transitive deps.
KERNEL_DIR="${MANGLEKIT_DIR:-../../manglekit}"
MKIT="$(mktemp -d)/mkit"
go build -C "$KERNEL_DIR" -o "$MKIT" ./cmd/mkit

set +e
"$MKIT" eval --policy "$POLICY" --facts "$FACTS" --query 'halt(Req, Reason)' --quiet
CODE=$?
set -e
case $CODE in
  0) echo "CI gate: PASS — no architecture violation" ;;
  1) echo "CI gate: DENY — policy violation found (see results above)" ;;
  *) echo "CI gate: error (exit $CODE)" ;;
esac
exit $CODE
