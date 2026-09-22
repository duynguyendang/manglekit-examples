# fixed_choice_router

**Can a routing decision be made with provable thresholds?** (UC-G6)

This example re-creates the inference path of the "Jev-style decision
engine" (Avi Chawla's blog, 2026-09): a single forward pass scores the
probabilities of the option letters `A/B/C`, a **restricted softmax**
normalizes over exactly those labels, and confidence thresholds decide
**auto-route vs send-to-review vs escalate**.

Then it does the things the blog could not:

- **Thresholds are policy, not code.** The `0.80 / 0.20 / 0.70` bars live in
  `policy.dl`. `client.ReloadPolicy` swaps them in-process — no rebuild, no
  redeploy — and a **broken policy file is rejected fail-safe** while the old
  one keeps serving. The blog's version of this step is "edit Python and
  restart".
- **Every denial carries its own proof.** `client.Explain` renders the
  derivation tree, including the numbers the rule matched
  (`confidence("0.710")` under `threshold("auto","0.80")`). An `if p < 0.80`
  in Python can never show you its reasoning.
- **Nothing unproven dispatches.** The routing call is a supervised action:
  a T1 halt means the inner `route_ticket` never ran (the tests assert the
  execution counter).
- **It answers the blog's two top comments** — calibration drift and order
  sensitivity — with runnable code instead of opinion.

## Run it

```bash
cd manglekit-examples
go run ./governance/fixed_choice_router/            # the tour (mock, no keys)
go run ./governance/fixed_choice_router/ --mode=bench        # two-lane benchmark (Trục A)
go run ./governance/fixed_choice_router/ --mode=threshold    # threshold effect (Trục B)
go run ./governance/fixed_choice_router/ --mode=calibrate    # reliability + ECE + temperature
go run ./governance/fixed_choice_router/ --mode=permute      # permutation flip-rate
```

### Two modes — and what each one proves

| Mode | Backend | Proves |
|---|---|---|
| **default** | deterministic `MockScorer` replaying `datasets/fixtures.jsonl` | the **gate mechanics**: distribution → thresholds → halt/allow, with proof trees. Works with zero GPU/keys — this is what CI runs. |
| **live** (`MANGLE_SCORER_BACKEND=http://localhost:30000`, `MANGLE_SCORER_MODEL=...`) | OpenAI-compatible `/v1/score` (SGLang) + `/v1/chat/completions` | real model numbers: accuracy, **latency/throughput**, order sensitivity. |

> **Honesty rule baked into the output:** latency and throughput columns are
> only printed in live mode — mock timings measure the mock, not a model.
> Accuracy in mock mode validates the mechanism against synthetic labels;
> it is not a claim about any real model's quality. Published blog numbers
> belong to the blog's hardware; run `live` to get yours.

## The Jev inference path, mapped

```text
1. enumerate options A/B/C as single-token labels
2. one forward pass, read the probability of each label token at the answer
   position  ->  SGLang POST /v1/score
   {model, query, items, label_token_ids: [<id A>,<id B>,<id C>], apply_softmax: true}
   ->  {"scores": [[pA, pB, pC]]}          # server-side restricted softmax,
                                            # exactly the blog's step 4
3. normalize defensively in Go (restrictedSoftmax) -> Decision{Choice,
   Distribution, Margin, Entropy}
4. publish the numbers to the policy (Go computes flags, the engine gates —
   see policy.dl header for why threshold/2 is rule-defined, not a bare fact)
5. T1 halts: review floor / auto threshold / no-parse escape hatch
   T2 advisory: thin margin (visible in Explain, never blocks)
```

Tokenization is checked before scoring: a label that does not occupy exactly
one token (`" A"` vs `"A"` land on different token IDs) is **rejected** —
see `validateSingleTokens` in `scoring.go`.

## Parity matrix — blog ↔ this example

| Blog (Jev-style recipe) | Manglekit equivalent | Where |
|---|---|---|
| SGLang `/v1/score` + `label_token_ids` | `SGLangScorer` (1 request/case, server-side restricted softmax over the label IDs) | `scoring.go` |
| Restricted softmax over A/B/C | same, + defensive renorm + single-token label validation | `scoring.go` |
| Thresholds in Python code | `threshold(Name, Value)` in Datalog, hot-reloadable | `policy.dl` |
| `if p < 0.70: "send to review"` | T1 `halt` rule + `client.Explain` proof tree with P/T bindings | `policy.dl`, `gate.go`, `main.go` |
| OTHER/ESCALATE escape hatch | `no_valid_choice` T1 rule — refusing to guess is a governed decision | `policy.dl`, `gate.go` |
| 100-case two-lane benchmark (scoring vs generation, barrier + concurrent) | `--mode=bench`, 300 cases (3 × 100 the blog used) | `bench.go` |
| *(missing in blog)* calibration | reliability table, ECE, temperature scaling (1 scalar, grid search) | `calibration.go` |
| *(missing in blog)* order robustness | permutation flip-rate (options permuted **in the prompt text**) + average-over-permutations mitigation | `permute.go` |
| *(missing in blog)* audit | supervised action: a T1 halt proves `route_ticket` never ran | `gate.go`, `main_test.go` |
| *(missing in blog)* zero-key reproducibility | mock backend + committed fixtures; live smoke test is env-gated (`t.Skip`) | `datasets/`, `bench_test.go` |

Scope note on backends: **live mode implements SGLang `/v1/score`**. The
`Scorer` interface is portable by design (vLLM `prompt_logprobs` and
LightLLM expose the same quantity), but those adapters are **not
implemented here** — only what is listed in this table exists.

## Two axes, kept apart

- **Trục A (`--mode=bench`)** compares *lanes*: scoring vs generation, same
  policy, same server, behind a start barrier. This is the blog's parity
  experiment.
- **Trục B (`--mode=threshold`)** compares *policies*: strict `policy.dl`
  (auto ≥ 0.80) vs `policy_relaxed.dl` (auto ≥ 0.70), re-measured after
  `ReloadPolicy`, with the broken-file rejection at the end. This is the
  hot-reload demonstration and deliberately **not** a "lane".

## Files

```text
main.go              # tour: score -> gate -> Explain -> micro hot-reload
scoring.go           # Scorer/Tokenizer interfaces; MockScorer; SGLangScorer
generate.go          # Generator for lane G; MockGenerator (simulated primacy bias); SGLangGenerator
gate.go              # threshold reading, supervised route_ticket, outcome mapping
policy.dl            # strict thresholds (rule-defined, see header)
policy_relaxed.dl    # relaxed twin for the hot-swap demo
policy_broken.dl     # intentionally invalid — used by the fail-safe test
bench.go             # Trục A: barrier-synchronized two-lane harness
threshold_demo.go    # Trục B: ReloadPolicy + fail-safe + escalation-mix table
calibration.go       # reliability bins, ECE, temperature scaling (1 scalar)
permute.go           # flip-rate measurement + AverageOverPermutations
datasets/            # package datasets: deterministic generator + committed fixtures.jsonl
main_test.go         # contract tests (mock-only, CI-green without keys)
bench_test.go        # bench/threshold/calibration/permute tests + env-gated live smoke
```

## Permutation design note

The permutation shuffles the **option enumeration in the prompt text** —
that is where a model can actually be biased. Shuffling `label_token_ids`
would be meaningless: each label probability is computed independently at
the same position, so the scoring lane is order-invariant *by construction*
(the mock's 0% flip-rate demonstrates exactly this invariant; a live model's
number is the one that matters).

## Live-mode notes

- Tested contract: SGLang native `/v1/score` (`query`, `items`,
  `label_token_ids`, `apply_softmax`) and `/tokenize` for single-token
  validation + token-ID resolution. If your deployment doesn't expose
  `/tokenize`, resolve the label IDs once out-of-band and set
  `MANGLE_LABEL_TOKEN_IDS=A_ID,B_ID,C_ID` — the scorer accepts them directly.
- Chat-template space: if the server glues a space to the answer token
  (`" A"` ≠ `"A"`), set `MANGLE_SCORER_SPACE=1`.
- Cross-backend drift is the point of `--mode=calibrate`: re-fit the
  temperature per (model, quantization, template); never ship one T.
