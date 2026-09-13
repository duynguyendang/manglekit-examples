# manglekit-examples

Example applications demonstrating [Manglekit](https://github.com/duynguyendang/manglekit) — a Sovereign Neuro-Symbolic Logic Kernel for Go with policy-based guardrails, cognitive loops, and neuro-symbolic reasoning.

## Makefile

```bash
make build       # build all examples
make test        # run all example tests
make run/<domain>/<name>  # run a specific example (e.g. make run/governance/devops_policy_gate)
make clean       # clean test artifacts and binaries
```

## Prerequisites

- Go 1.25.1
- A Google AI API key (`GOOGLE_API_KEY`) for examples marked "Yes" in the API Key column
- Optional: an OpenAI key (`OPENAI_API_KEY`) for **real_llm_gate**'s live mode (falls back to a deterministic mock without any key)

## Setup

```bash
# Clone with the manglekit SDK as a sibling directory
git clone <this-repo> manglekit-examples
git clone <manglekit-repo> manglekit

# From the manglekit-examples root
cd manglekit-examples

# (Optional) For examples that use an LLM
export GOOGLE_API_KEY=your-key
```

## Learning Path

**Quick tour (one per idea):** `real_llm_gate` → `devops_policy_gate` →
`ooda_east_generation` → `learn_from_code` → `http_service`.
Packages marked **deep dive** below are specialist tours — read the foundations
first.

**Every package answers one business question** (full UC catalog: workspace
`docs/use-cases/`):

| Question | Package | Proof you can run |
|---|---|---|
| Can prompt injection exfiltrate data? | `governance/jailbreak_proof_agent` | tainted exfil blocked at pre-check |
| Dangerous infra ops blocked — and *why*? | `governance/devops_policy_gate` | T1 denies, T2 advises, Explain tree |
| Audit trail for regulators (GDPR)? | `governance/compliance_proof` | tiered halt proofs |
| Cheap model, still certified correct? | `governance/verified_reasoning` | wrong→wrong→right retry loop |
| Reuse an existing policy DSL, zero engine changes? | `governance/custom_policy_format` | compile → LoadPolicy |
| Generation that self-corrects until compliant? | `cognition/ooda_east_generation` | retry converges; ROUTE/paradox |
| Nothing unverified streams out? | `cognition/genkit_middleware_showcase` | deny before first chunk (0 provider calls) |
| Approvals before prod deploys, planned? | `cognition/goal_based_planning` | plan gated + deny path |
| Learn policy from a codebase; promote humans-only? | `learning/learn_from_code` | ALLOW→DENY after `--confirm` |
| Agents get better across sessions? | `learning/skill_learning` | 2 attempts → 1 attempt |
| Security team writes policy in English? | `learning/policy_copilot` | NL→rule→signed gene; tamper rejected |
| Org access graph answers with proof? | `knowledge/knowledge_graph_reasoning` | graph tiered queries |
| Tenant data stays in tenant? | `knowledge/hybrid_rag` | cross-tenant RAG blocked |
| Was authorization valid *at transaction time*? | `knowledge/temporal_compliance` | revoked-after misuse denied |
| Short-lived facts: where, who, when-die? | `storage/silo_storage` | TTL expiry + coordination facts |
| Agent survives crash/restart? | `storage/persistent_store` | close/reopen resume; crash sim |
| Gate inside a real service, staying up? | `integration/http_service` | 403; hot reload flips; breaker |
| MCP tools can't bypass policy? | `integration/mcp_tool_integration` | gated tool calls |
| CI catches architecture violations? | `integration/code_to_policy_extractor` | `ci_gate.sh` exit 1/0 |
| Ship skills from YAML? | `integration/config_driven_app` | config-loaded gated skill |
| Hello, gate (real LLM, mock fallback)? | `integration/real_llm_gate` | allow + deny |
| Long multi-agent jobs resume? | `integration/multi_agent_research` | hydrated resume |
 `temporal_compliance` answers a real audit question (was an
authorization valid *at transaction time*?); the engine feature is opt-in
(`EnableTemporal`), the story is not a demo.


Examples live under six domain folders — `governance/`, `cognition/`,
`learning/`, `knowledge/`, `storage/`, `integration/` — one runnable package
each, with names kept stable since 2026-09. Tables below are ordered by
complexity within each group; start with Foundations and work your way up.

### 1. Foundations

Core governance patterns — the simplest entry point.

| Example | Description | API Key | Run |
|---|---|---|---|
| **code_to_policy_extractor** | Clean-Architecture linter on PRs + `ci_gate.sh`: the same policy as a CI gate through `mkit eval` exit codes (0 clean / 1 deny / 2 usage) | No | `go run ./integration/code_to_policy_extractor/` |
| **custom_policy_format** | A user-defined policy DSL compiled to Datalog and loaded via `LoadPolicy` — the documented replacement for the removed Gherkin compiler | No | `go run ./governance/custom_policy_format/` |
| **config_driven_app** | Declarative YAML config, generics API (`Define[In,Out]`), and provider registry | No | `go run ./integration/config_driven_app/` |
| **real_llm_gate** | Minimal supervised LLM call with `QuickClient` + `RegisterSupervised`: allowed and policy-denied case | Optional (mock fallback) | `go run ./integration/real_llm_gate/` |

### 2. Policy & Governance

Zero-trust policy enforcement — the core value prop of manglekit.

| Example | Description | API Key | Run |
|---|---|---|---|
| **devops_policy_gate** | CI/CD security gates (Terraform/K8s) + tier semantics (T1 blocks vs T2 advisory, P0.7) + Explainable Deny: structured `PolicyViolationError` and the derivation proof tree | No | `go run ./governance/devops_policy_gate/` |
| **compliance_proof** | GDPR as tiered Datalog — `AssessPlan` renders `AuditTrail` as machine-checkable proof | No | `go run ./governance/compliance_proof/` |
| **jailbreak_proof_agent** | T0 taint axiom blocks data exfiltration — mock LLM complies with injection but kernel holds | No | `go run ./governance/jailbreak_proof_agent/` |
| **verified_reasoning** | Cheap model + symbolic verifier = certified-correct output via verify-retry loop | No | `go run ./governance/verified_reasoning/` |
| **temporal_compliance** | **Was authorization valid at transaction time?** Retro-audit with temporal facts (UC-G5; engine feature is opt-in via `EnableTemporal`) | No | `go run ./knowledge/temporal_compliance/` |

### 3. Knowledge & Reasoning

Knowledge graphs, vector search, and hybrid RAG patterns.

| Example | Description | API Key | Run |
|---|---|---|---|
| **knowledge_graph_reasoning** | Load N-Triples knowledge graphs, define transitive Datalog rules, query with audit trails | No | `go run ./knowledge/knowledge_graph_reasoning/` |
| **hybrid_rag** | Multi-tenant RAG with transitive access control and egress tainting | No (mocks) | `go run ./knowledge/hybrid_rag/` |
| **silo_storage** | **Where do short-lived facts live, who shares them, when do they die?** Session TTL/expiry, transient OODA coordination facts, vectors, N-Triples loading (UC-K3) | No | `go run ./storage/silo_storage/` |

> **Note:** hybrid_rag's access-control and egress scenarios run on the full
> `client.Supervise()` pre-check path: `ExecuteByName` recalls memory
> (`RecallWithFacts` injects `memory_hit_N` metadata), the supervisor
> pre-flight check evaluates the policy with the request's metadata, labels,
> and `action_operation`/payload facts, and the action only executes on
> PROCEED — enforcement happens in the kernel, not in demo code.

### 4. Cognitive Loops (OODA / EAST)

Observe-Orient-Decide-Act loops with entropic steering and self-correction.

| Example | Description | API Key | Run |
|---|---|---|---|
| **ooda_east_generation** | **Can a generation loop self-correct until policy is satisfied?** Run/RunEAST tours: Teacher-Student retry, mixed-precision memory, ROUTE + SteerKB + paradox (ex `route_chaining`), custom 5-phase convergence (ex `ooda_document_generator`) | No | `go run ./cognition/ooda_east_generation/` |
| **skill_learning** | Cross-session skill learning: file-backed `ooda.Memory` (auto-Commit learner, Orient-time Recall) + `ports.ReasoningPort` route learning for `SteerKB` — session 2 needs fewer refinements and takes the learned fast path after a simulated restart | No | `go run ./learning/skill_learning/` |
| **learn_from_code** | UC-L8 "learn from code" skill: deterministic intent router (LEARN/EVAL/PROMOTE/STATUS) — source tree → signals → induced `x/genes` candidates (advisory T2/T3 only, signed, provenanced), shadow EVAL through the real gate, human-confirmed PROMOTE to T1 that then DENIES (`docs/use-cases/learning.md`) | No | `go run ./learning/learn_from_code/` |

### 5. Orchestration & Planning

Workflow engines, goal-driven planning, and multi-agent coordination.

| Example | Description | API Key | Run |
|---|---|---|---|
| **goal_based_planning** | Datalog-driven action planning with `client.Plan()` and `ExecutePlan()` | No | `go run ./cognition/goal_based_planning/` |
| **multi_agent_research** | Sequential, parallel, and hydrated workflow executors with session resume | No | `go run ./integration/multi_agent_research/` |

### 6. Integration & Production

External system integrations, LLM bridges, and production infrastructure.

| Example | Description | API Key | Run |
|---|---|---|---|
| **mcp_tool_integration** | Model Context Protocol server integration with policy-gated tool execution | No | `go run ./integration/mcp_tool_integration/` |
| **genkit_middleware_showcase** | Genkit middleware (Retry, Fallback, Tool Approval) + Supervised Streaming + OODA-as-Genkit-flows (`OODAFlow`, `FlowRegistry`, ex `ooda_genkit_flow`): pre-check denies BEFORE the first chunk (provider never opened); OUTPUT-rule post-check refuses the assembled stream | Yes | `go run ./cognition/genkit_middleware_showcase/` |
| **policy_copilot** | NL→Datalog generation + signed `x/genes` packaging (tamper-checked pool, policy-channel enforcement) + text→struct extraction bridge (ex `extractor_bridge`) | No (mock LLM) | `go run ./learning/policy_copilot/` |
| **persistent_store** | Durable state tour: BadgerDB StateProvider (`WithSyncWrites(true)`, close/reopen restart-resume) + in-memory checkpoint/hydrate & crash simulation (ex `session_recovery`) | No | `go run ./storage/persistent_store/` |
| **http_service** | Production wiring tour: `/ask` supervised action (403 on deny) + `/admin/reload` fail-safe hot policy swap + circuit breaker (Closed→Open→HalfOpen) & OTel tracer around the same stack (ex `production_resilience`) | No | `go run ./integration/http_service/` |

## Proof points

What this suite demonstrates — and where you can watch it fail closed:

| Claim | Watch it happen |
|---|---|
| Deny is tier-aware, not binary | `devops_policy_gate`: same gate, a T1 halt stops the deploy, a T2 speaks and steps aside |
| Every deny carries its own proof | `devops_policy_gate`: structured `PolicyViolationError` (tier/rule/action) + `client.Explain` derivation tree with `[negation]` nodes |
| Policy changes without restarts — safely | `http_service`: `POST /admin/reload` flips verdicts mid-flight; a broken policy file is rejected and the old one keeps serving |
| Nothing unverified streams out | `genkit_middleware_showcase`: the denied stream proves the provider was never opened (call counter 0→0); a post-check refusal kills the assembled output's finality |
| The gate works in CI where it counts | `code_to_policy_extractor`: `ci_gate.sh` — exit 0 clean / 1 deny / never a masquerade |
| Generated rules need provenance + signatures | `policy_copilot`: NL → Datalog → signed T3 gene; a whitespace-only edit is detected and rejected |
| The kernel learns from itself | `learn_from_code`: whole-tree LEARN → ASK your drafts against the material → PROMOTE is human-only; the kernel's own pool + drift CI live in this repo's `dogfood/` |

## Manglekit Features Demonstrated

| Feature | Package | Examples |
|---|---|---|
| Datalog policy engine | `core.Evaluator` | All examples |
| Zero-trust supervisor | `client.Supervise()` → `client.ExecuteByName()` | code_to_policy_extractor, devops_policy_gate, mcp_tool_integration, hybrid_rag, goal_based_planning, persistent_store, config_driven_app |
| Assess (policy evaluation) | `Engine().Assess()` | custom_policy_format, ooda_east_generation, config_driven_app, knowledge_graph_reasoning |
| AssessPlan (pure policy-decision) | `Engine().AssessPlan()` | compliance_proof, knowledge_graph_reasoning, verified_reasoning, ooda_east_generation, jailbreak_proof_agent |
| AuditTrail rendering | `core.AuditTrail` | compliance_proof, knowledge_graph_reasoning, verified_reasoning, ooda_east_generation |
| Tiered governance (T0–T3, real block/advisory split) | `core.Tier`, supervisor gate | compliance_proof, devops_policy_gate, ooda_east_generation |
| Explainable deny | `client.Explain`, structured `PolicyViolationError` | devops_policy_gate |
| Hot policy reload | `client.ReloadPolicy` (fail-safe atomic swap) | http_service |
| Supervised streaming | `adapters/ai.NewStreamingSupervisedAction` | genkit_middleware_showcase |
| CI exit-code contract | `mkit eval` → 0/1/2/3 | code_to_policy_extractor (`ci_gate.sh`) |
| Signed learned-rule packaging | `x/genes` (Gene/Pool/Compile/ApplyTo) | policy_copilot, learn_from_code |
| Goal-based planning | `client.Plan()` / `client.ExecutePlan()` | goal_based_planning |
| Function adapter | `adapters/func` | hybrid_rag, devops_policy_gate, code_to_policy_extractor |
| Knowledge graphs (N-Triples) | `adapters/knowledge` | knowledge_graph_reasoning, hybrid_rag, silo_storage |
| Hybrid memory (RAG) | `sdk.HybridMemory` | hybrid_rag |
| Vector store | `adapters/vector` | hybrid_rag, silo_storage |
| External predicates | `client.RegisterExternalPredicate()` | hybrid_rag |
| EvaluateSteering | `Engine().EvaluateSteering()` | hybrid_rag, ooda_east_generation |
| Security / taint labels | `core.Envelope.SecurityLabels` | jailbreak_proof_agent, hybrid_rag |
| Policy violation detection | `core.IsPolicyViolationError()` | code_to_policy_extractor, devops_policy_gate, mcp_tool_integration, hybrid_rag, goal_based_planning |
| OODA cognitive loop (5-phase) | `x/ooda` | ooda_east_generation |
| RunOODA / RunOODAEAST | `x/ooda` (`RunOODA`), `x/east` (`RunOODAEAST`) | ooda_east_generation |
| EAST steering (entropy/saliency) | `x/east` | ooda_east_generation, skill_learning |
| Mixed-precision memory | `ooda.PinAxiom` / `AddContext` / `ShaveContext` | ooda_east_generation |
| Tool registry & dispatcher | `ooda.Registry` / `ooda.Dispatcher` | ooda_east_generation, genkit_middleware_showcase |
| OODA as Genkit flow | `x/oodaflow` | genkit_middleware_showcase |
| FlowRegistry | `x/oodaflow.FlowRegistry` | genkit_middleware_showcase |
| ROUTE decision (dynamic chaining) | `core.DecisionRoute` | ooda_east_generation |
| Paradox injection | `x/east.ShouldInjectParadox()` | ooda_east_generation |
| Cross-session skill learning | `ooda.Memory` (`Builder.WithMemory`, auto-Commit in `eastPostAct`) + `ports.ReasoningPort` (`SteerKB`) | skill_learning |
| Learn-from-code skill (UC-L8): intent router + `x/genes` induction/promotion | `x/genes` (`Gene`/`Pool`/`Compile`/`ApplyTo`), `sdk.Client` supervised EVAL | learn_from_code |
| MCP integration | `adapters/mcp` | mcp_tool_integration |
| Genkit middleware | `adapters/ai` | genkit_middleware_showcase, real_llm_gate |
| Session state recovery (checkpoint/hydrate) | `core.StateProvider`, `core.SessionState` | persistent_store, multi_agent_research |
| Multi-agent runtime | `multiagent.AgentSystem` | multi_agent_research |
| Workflow executors (sequential/parallel/hydrated) | `multiagent.WorkflowExecutor` | multi_agent_research |
| Condition evaluation (Datalog) | `AgentSystem.EvaluateCondition()` | multi_agent_research |
| Taint labels (security) | `core.Envelope.SecurityLabels` | jailbreak_proof_agent, hybrid_rag |
| Symbolic verification | `Engine().Query()` | verified_reasoning |
| Custom policy format → Datalog | user DSL → `Client.LoadPolicy()` | custom_policy_format |
| Generics API | `sdk.Define[In,Out]()` | config_driven_app |
| Declarative YAML config | `config.Load()` / `sdk.WithConfig()` | config_driven_app |
| Provider registry | `sdk.RegisterProvider()` / `sdk.WithProviderConfig()` | config_driven_app |
| NL→Datalog policy copilot | `sdk.NewPolicyGenerator()` | policy_copilot |
| Schema extraction (mangle tags) | `Generator.extractSchema()` | policy_copilot |
| Text→struct extraction | `adapters/extractor` | policy_copilot (extractor section) |
| Circuit breaker | `adapters/resilience.CircuitBreaker` | http_service (resilience section) |
| OpenTelemetry tracing | `sdk.WithStdoutTracer()` | http_service (resilience section) |
| Scenario runner (BDD tests) | `scenario.Run()` | jailbreak_proof_agent |
| Struct → Datalog (Zero-Config Reflection) | `mangle` struct tags | code_to_policy_extractor, hybrid_rag, policy_copilot |
| QuickClient / RegisterSupervised | `manglekit.QuickClient` / `Client.RegisterSupervised` | real_llm_gate, http_service |
| cwd-safe fixture loading | `manglekit.MustReadFile` | hybrid_rag, compliance_proof, ooda_east_generation, verified_reasoning, real_llm_gate, http_service |
| Test doubles (mocks) | `manglekit/testutil` (MockLLM, DeterministicEmbedder, InMemoryStateProvider, WorkflowSessionStore) | hybrid_rag, silo_storage, persistent_store, multi_agent_research, real_llm_gate, http_service |
| Durable BadgerDB state | `WithSyncWrites(true)` + batch fact writes | persistent_store |

## Behavior notes

These examples pin the **current** architecture behavior:

- **Supervisor pre-check is the trustworthy gate.** Every `Supervise` + `ExecuteByName` example
  relies on the pre-check (VerifyAtoms) for enforcement.

- **Post-check (Reflect) is fail-closed since ADR-001** — it matches the
  pre-check gate. A verifier/engine error blocks the result with
  `core.SupervisorError` (no silent pass-through); a detected violation at
  T0/T1 blocks with `core.PolicyViolationError`; explicitly tier-tagged
  T2/T3 violations are advisory (logged `Warn`, not blocking). See the
  workspace docs `docs/context/governance/enforcement-contract.md` (the
  authoritative contract) and `manglekit/internal/supervisor/action.go`
  pre-flight `:64-79` / post-flight `:128-152`.

- **Fail-closed is the only mode (v0.6).** `WithFailMode` was removed: the
  pre-check gate always fails closed, and text-extraction failures block
  execution. Block behavior is decided by policy alone.

- **Datalog escaping is applied** on the live path (`atomToDatalog` at `sdk_adapter.go:22`)
  via `engine.EscapeString` and an identifier-regex predicate check, and was hardened
  in v0.5.0 to cover the `multiagent` paths (`agent_system.go:269`, `workflow_executor.go:155`).
  Facts built from dynamic/trusted-local values are safe.

- **`ooda.NewLoop` (used by ooda_east_generation's document-phases section) is experimental** (ROADMAP §P3); the
  canonical entry points are `ooda.NewBuilder()...Build()` + `ooda.RunOODA` and
  `x/east.RunOODAEAST`. Since v0.8 the EAST (OODA v4) path lives in the optional
  `x/east` extension — the deterministic `x/ooda` chassis stays dependency-free of it.

- **`client.Execute` is steering-gated** (errors unless `WithSteeringEnabled`). Examples
  prefer `ExecuteByName`/`generator.Generate`.

## Testing

```bash
go test ./...
```

All examples include tests. No external API keys required for tests — mocks are used where needed.

## Repo Structure

```manglekit-examples/
  governance/            -- policy gate mechanics
    devops_policy_gate/  -- CI/CD gates + T1/T2 tier split + Explain proofs
    compliance_proof/    -- GDPR tiered Datalog with audit proof
    jailbreak_proof_agent/ -- T0 taint axiom blocks exfiltration
    verified_reasoning/  -- symbolic verify-retry certification loop
    custom_policy_format/ -- user DSL → Datalog via LoadPolicy
  cognition/             -- OODA / EAST loops (x/ extension family)
    ooda_east_generation/ -- OODA tour: EAST retry, ROUTE/SteerKB/paradox, custom phases
    genkit_middleware_showcase/ -- middleware + supervised streaming
    goal_based_planning/ -- Datalog-driven action planning
  learning/              -- learn-from-X, human-gated promotion
    learn_from_code/     -- tree LEARN + ASK + dogfood pool + CI gate
    skill_learning/      -- cross-session runtime learning (ooda.Memory)
    policy_copilot/      -- NL→Datalog + signed genes + text→struct bridge
  knowledge/             -- facts, retrieval, time
    knowledge_graph_reasoning/ -- N-Triples graph reasoning
    hybrid_rag/          -- multi-tenant RAG with access control
    temporal_compliance/ -- validity-at-time audit reasoning
  storage/               -- The Silo
    silo_storage/        -- session/transient/vector/MEB subsystems
    persistent_store/    -- durable store + checkpoint/hydrate recovery tour
  integration/           -- real-world wiring
    http_service/        -- /ask + hot reload + circuit breaker + OTel
    mcp_tool_integration/ -- MCP tools behind the gate
    code_to_policy_extractor/ -- arch-linter + ci_gate.sh exit codes
    config_driven_app/   -- YAML config + generics + provider registry
    real_llm_gate/       -- minimal supervised LLM call
    multi_agent_research/ -- workflow orchestration
```

Each example has its own `package main` and can be run independently. ★ = new in this release.

## See also

- [manglekit](https://github.com/duynguyendang/manglekit) — the SDK
