# Dogfood: the kernel reviews itself with `learn_from_code`

This directory applies the UC-L8 learn-from-code loop to the **manglekit
kernel repository itself**. The output — `genes.yaml` — is a *reviewed
artifact*: the committed record of which code-hygiene lessons the kernel's
current sources induce. CI (`manglekit/.github/workflows/policy-hygiene.yml`)
fails any PR that changes the induced lessons without reviewing and
re-committing the pool — same discipline as golden files or `go.sum`.

## Loop

```bash
./dogfood/regenerate.sh              # scan ../../manglekit sources → genes.yaml
./dogfood/check-drift.sh             # CI check: pool == what code induces
go run . --status --out dogfood      # what the kernel currently teaches
go run . --eval --signals destructive_call --out dogfood   # how the gate would react
```

Determinism: the CLI itself walks the tree (`--learn <dir>`), and extraction
keys plus the candidate SHA depend **only on signal-bearing files** (sorted,
content-hashed), so unrelated edits never churn the pool. Walk exclusions:
`*_test.go`, `testdata/`, `vendor/`, dot-dirs — fixtures intentionally
contain fake secrets; the scanned set is the shipped code. `regenerate.sh`
is a thin wrapper over the CLI (no duplicate find logic; portable).

## Reviewed findings (2026-09-11, sha 6b499c8f)

One advisory gene: `lc-6b499c8f-signals` (T3) — "destructive bulk operation
— require an explicit approval path" — triggered by `func (receiver)
Delete/Drop/Purge…()` in 6 files:

| File | Surface | Verdict |
|---|---|---|
| `adapters/storage/session/store.go` | `DeleteSession` (TTL session hygiene) | known-good — data the store owns |
| `internal/statemanager/manager.go` | durable state delete path | known-good — scoped by session ID |
| `sdk/ports/session_store.go` | interface declaration only | false-positive-by-shape — keep the lesson (interfaces SHOULD be reviewed too) |
| `testutil/testutil.go` | in-memory test provider | known-good — no production data |
| `x/agents/architect.go`, `x/agents/tools/tools.go` | workflow/artifact deletion in the reference agent | known-good — extension, not gate path |

No `exec_import`, `direct_db_access`, or `hardcoded_secret` signals in
shipped kernel code.

## Promotion stance

T3 stays. Nothing here is a candidate for T1 promotion: the signal flags
*review-requiring shapes*, not violations — and this repo's own tier
discipline says a human decision, not a heuristic, turns advisory into
blocking (`x/genes.WithAllowHardTiers`, `--confirm`).

## History

- 2026-09-11: first dogfood run. Found and fixed two real extractor false
  negatives: (1) method-form `func (r *T) DeleteX()` was invisible to the v1
  regex (6 kernel files missed); (2) single-form `import "os/exec"` was
  invisible to the import regexes. Candidate identity was rescoped to
  signal-bearing files so unrelated PRs don't churn the pool — proven by the
  pool SHA surviving both regex fixes unchanged (no new verdicts).
- 2026-09-11: the first `check-drift.sh` design mutated the committed pool
  in place, and because LEARN merges by upsert, a failing check left
  residue that masked later runs. Rewritten to regenerate into a temp dir;
  the committed pool is now read-only for CI. (Caught by simulating a
  violating kernel — write a drift simulation for every gate before
  trusting a "pass".)
