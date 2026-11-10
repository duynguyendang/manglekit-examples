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
go run ./governance/fixed_choice_router/                     # tour: score -> gate -> Explain (mock)
go run ./governance/fixed_choice_router/ --mode=bench        # two-lane benchmark (Trục A)
go run ./governance/fixed_choice_router/ --mode=threshold    # threshold effect (Trục B)
go run ./governance/fixed_choice_router/ --mode=calibrate    # reliability + ECE + temperature
go run ./governance/fixed_choice_router/ --mode=permute      # permutation flip-rate

# same five modes, real model, API key only (no server):
MANGLE_SCORER=openai OPENAI_API_KEY=sk-... OPENAI_MODEL=gpt-4o-mini \
  go run ./governance/fixed_choice_router/ --mode=bench

# ...or the exact blog path, with a scoring server:
MANGLE_SCORER_BACKEND=http://localhost:30000 MANGLE_SCORER_MODEL=qwen/qwen3-4b-instruct \
  go run ./governance/fixed_choice_router/ --mode=bench
```

### Three modes — and what each one proves

| Mode | Backend | Proves |
|---|---|---|
| **default** (mock) | deterministic `MockScorer` replaying `datasets/fixtures.jsonl` | the **gate mechanics**: distribution → thresholds → halt/allow, with proof trees. Zero GPU, zero keys — this is what CI runs. |
| **key-only** — `MANGLE_SCORER=openai` + `OPENAI_API_KEY` | hosted `/chat/completions` with `logprobs: true, top_logprobs: K` | the **real Jev distribution without standing up a server**: request one token, read the option labels out of the returned log-probabilities, renormalize client-side over exactly those labels. API key, no GPU. |
| **score server** — `MANGLE_SCORER_BACKEND=http://localhost:30000` + `MANGLE_SCORER_MODEL=...` | SGLang native `/v1/score` (`label_token_ids`) | the exact blog path: the server restricts the softmax to the label IDs you name, so **no candidate can ever be missing**. Needs a scoring server. |

> **Honesty rule baked into the output:** latency and throughput columns are
> only printed in live mode — mock timings measure the mock, not a model.
> Accuracy in mock mode validates the mechanism against synthetic labels;
> it is not a claim about any real model's quality. Published blog numbers
> belong to the blog's hardware; run live to get yours.

### Why the key-only path can escalate — and why that is the right design

`top_logprobs` is a **truncated** view of the vocabulary: the provider returns
only the K highest-probability tokens at the answer position. If an option
letter is not among them, its probability is *unobservable* — and normalizing
over the survivors would silently overstate confidence, which is exactly the
failure the blog's critics warned about.

So the kernel's `scoring.ChatScorer` returns `scoring.ErrLabelsUnobserved`, and
this example routes that case through the policy instead of inventing a number
in Go:

```datalog
halt("Req", "unobserved_distribution_escalate", "T1") :-
    action_operation("Req", "route_ticket"),
    meta("unobserved_distribution", "true").
```

The refusal to auto-route is a governed decision with its own proof tree.
Raise `MANGLE_LLM_TOP_LOGPROBS` where the provider allows, or use the
`/v1/score` server, to make it never happen.

## The Jev inference path, mapped

```text
1. enumerate options A/B/C as single-token labels
2. one forward pass, read the probability of each label token at the answer
   position. Two ways to get it (kernel adapters/scoring):
     a) /v1/score  {model, query, items, label_token_ids, apply_softmax:true}
        -> {"scores": [[pA, pB, pC]]}       # server-side restricted softmax
     b) /chat/completions {max_tokens:1, logprobs:true, top_logprobs:K}
        -> read the labels out of top_logprobs, exp + renormalize client-side
3. scoring.Build(...) -> Decision{Choice, Distribution, Margin, Entropy}
   (renormalizes over exactly the options; discards every other token)
4. publish the numbers to the policy (Go computes flags, the engine gates —
   see policy.dl header for why threshold/2 is rule-defined, not a bare fact)
5. T1 halts: review floor / auto threshold / no-parse escape hatch
   T2 advisory: thin margin (visible in Explain, never blocks)
```

Tokenization is checked before scoring on the `/v1/score` path: a label that
does not occupy exactly one token (`" A"` vs `"A"` land on different token IDs)
is **rejected** — `scoring.ValidateSingleTokens`, checked against the surface
actually sent (so `SpacePrefix` cannot bypass it).

## Parity matrix — blog ↔ this example

| Blog (Jev-style recipe) | Manglekit equivalent | Where |
|---|---|---|
| SGLang `/v1/score` + `label_token_ids` | `scoring.ScoreServer` (1 request/case, server-side restricted softmax over the label IDs) | kernel `adapters/scoring` |
| Restricted softmax over A/B/C | `scoring.Build` — renormalizes over exactly the options, + single-token label validation | kernel `adapters/scoring` |
| *(blog needs a scoring server)* | `scoring.ChatScorer`: `/chat/completions` + `logprobs`/`top_logprobs`, labels read client-side — **API key only, no server** | kernel `adapters/scoring` |
| Thresholds in Python code | `threshold(Name, Value)` in Datalog, hot-reloadable | `policy.dl` |
| `if p < 0.70: "send to review"` | T1 `halt` rule + `client.Explain` proof tree with P/T bindings | `policy.dl`, `gate.go`, `main.go` |
| OTHER/ESCALATE escape hatch | `no_valid_choice` + `unobserved_distribution` T1 rules — refusing to guess is a governed decision | `policy.dl`, `gate.go` |
| 100-case two-lane benchmark (scoring vs generation, barrier + concurrent) | `--mode=bench`, 300 cases (3 × 100 the blog used) | `bench.go` |
| *(missing in blog)* calibration | reliability table, ECE, temperature scaling (1 scalar, grid search) | `calibration.go` |
| *(missing in blog)* order robustness | permutation flip-rate (options permuted **in the prompt text**) + average-over-permutations mitigation | `permute.go` |
| *(missing in blog)* audit | supervised action: a T1 halt proves `route_ticket` never ran | `gate.go`, `main_test.go` |
| *(missing in blog)* zero-key reproducibility | mock backend + committed fixtures; live smoke tests are env-gated (`t.Skip`) | `datasets/`, `bench_test.go`, `hosted_test.go` |
| *(missing in blog)* errors cannot hide | each lane owns its gate; errored calls are counted, printed and fail the run | `bench.go` |

Scope note on backends: three scorers exist — this example's `MockScorer` (CI),
and the kernel's `scoring.ChatScorer` (hosted chat logprobs, key-only) and
`scoring.ScoreServer` (native `/v1/score`). The `scoring.Scorer` interface is
portable further (vLLM `prompt_logprobs`, LightLLM expose the same quantity),
but those adapters are **not implemented** — only what is listed here exists.

## The policy does the arithmetic

Since the per-class redesign the gate is not "Go computes a boolean, Datalog
pattern-matches it". The kernel publishes per-mille integers and **the policy
compares them**:

```prolog
class_bar("billing", T)            :- threshold_pm("auto", T).   % inherits the global bar
class_bar("account_access", 950)   :- policy_anchor("ok").       % riskier class, its own bar

halt("Req", "low_confidence_send_to_review", "T1") :-
    action_operation("Req", "route_ticket"),
    choice(C), score_pm(C, P), class_bar(C, B), :lt(P, B),
    threshold_pm("review", T), :ge(P, T).

% mandatory catch-all: an option with no bar must DENY, not auto-route
halt("Req", "no_class_bar_defined_refuse", "T1") :-
    action_operation("Req", "route_ticket"),
    choice(C), !has_class_bar(C).
```

So the same score can be allowed for one class and held back for another, and
the proof tree contains the comparison itself:

```text
halt("Req","low_confidence_send_to_review","T1")
  choice("technical_support")            [base fact]
  score_pm("technical_support",740)      [base fact]
  class_bar("technical_support",800)     via threshold_pm("auto",800) :- policy_anchor("ok")
  740 < 800                              [builtin]
  740 >= 700                             [builtin]
```

Integers because Mangle's comparisons are integer-only (a float operand fails
evaluation loudly); `scoring.ToPerMille` truncates, so a borderline score fails
the bar rather than sneaking over it.

**Two lessons this policy paid for:**

1. *Per-class bars must be total.* When bars existed only for the support
   domain, every screening/expense case **auto-routed at any confidence** — the
   halt rule could not join `class_bar/2`, so it never fired. Absence of a
   policy fact must never mean "allowed"; hence the catch-all above.
2. *Negate a unary derived predicate.* `!class_bar(C, B)` with `B` unbound is a
   mode error at evaluation time (269/300 calls errored). `has_class_bar(O) :-
   class_bar(O, B).` then `!has_class_bar(C)` works.

And the generation lane now publishes **no score at all** (`RequestNoScore`):
a letter is not a distribution. It therefore auto-routes 0/300 and lands in
review via `no_score_present_review` — which is the result, not a bug. An
earlier version fabricated `P=1.0` there and auto-routed all 300.

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
main.go              # tour: score -> gate -> Explain -> hot swap -> fail-safe reload
scoring.go           # example glue only: Decision/Scorer aliases, MockScorer, MockTokenizer
generate.go          # Generator for lane G; MockGenerator (simulated primacy bias); SGLangGenerator
gate.go              # supervised route_ticket + outcome mapping (thresholds via the kernel adapter)
policy.dl            # strict thresholds (rule-defined, see header)
policy_relaxed.dl    # relaxed twin for the hot-swap demo
policy_broken.dl     # intentionally invalid — used by the fail-safe test
bench.go             # Truc A: barrier-synchronized two-lane harness (one gate per lane)
threshold_demo.go    # Truc B: ReloadPolicy + fail-safe + escalation-mix table
calibration.go       # reliability bins, ECE, temperature scaling (1 scalar)
permute.go           # flip-rate measurement + AverageOverPermutations
datasets/            # package datasets: deterministic generator + committed fixtures.jsonl
main_test.go         # contract tests (mock-only, CI-green without keys)
bench_test.go        # bench/threshold/calibration/permute tests + env-gated live smoke
hosted_test.go       # the key-only path through THIS gate, incl. truncated-distribution escape
```

### What lives in the kernel vs here

The scoring machinery is no longer in this example — it is
`manglekit/adapters/scoring` ([ADR-005](../../../../docs/adr/005-scoring-is-a-preparer.md)),
and this package is its consumer:

| Concern | Where |
|---|---|
| `Decision`, restricted softmax (`Build`), single-token label validation | kernel `adapters/scoring` |
| Key-only hosted scorer (`ChatScorer`, chat logprobs) and `/v1/score` scorer (`ScoreServer`), `HTTPTokenizer` | kernel `adapters/scoring` |
| `ErrLabelsUnobserved` — truncated top-K yields no fabricated probability | kernel `adapters/scoring` |
| Thresholds read from the active policy; ambiguity = hard error | kernel `ThresholdsFromPolicy` |
| The gated envelope (meta flags + confidence/margin/entropy facts) | kernel `scoring.Request` |
| Fixture-replaying mock, prompt building, calibration, permutation, benchmark, tour narrative | this example |

Practical consequence: `MANGLE_SCORER=openai` needs **no scoring server** — just
`OPENAI_API_KEY` (plus optional `OPENAI_BASE_URL`, `OPENAI_MODEL`,
`MANGLE_LLM_TOP_LOGPROBS`). `MANGLE_SCORER_BACKEND` + `MANGLE_SCORER_MODEL`
selects the `/v1/score` path instead, which cannot truncate candidates.

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
