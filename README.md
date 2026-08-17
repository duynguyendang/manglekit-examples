# manglekit-examples

Example applications demonstrating [Manglekit](https://github.com/duynguyendang/manglekit) — a Sovereign Neuro-Symbolic Logic Kernel for Go with policy-based guardrails, cognitive loops, and neuro-symbolic reasoning.

## Makefile

```bash
make build       # build all examples
make test        # run all example tests
make run/<name>  # run a specific example (e.g. make run/devops_policy_gate)
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

Examples are organized into six domains, ordered by complexity within each group. Start with Foundations and work your way up.

### 1. Foundations

Core governance patterns — the simplest entry point.

| Example | Description | API Key | Run |
|---|---|---|---|
| **code_to_policy_extractor** | Dynamic Architecture Linter enforcing Clean Architecture rules on PRs | No | `go run ./code_to_policy_extractor/` |
| **gherkin_policy** | BDD Gherkin feature files compiled to Datalog policies via `LoadGherkinPolicy` | No | `go run ./gherkin_policy/` |
| **config_driven_app** | Declarative YAML config, generics API (`Define[In,Out]`), and provider registry | No | `go run ./config_driven_app/` |
| **real_llm_gate** | Minimal supervised LLM call with `QuickClient` + `RegisterSupervised`: allowed and policy-denied case | Optional (mock fallback) | `go run ./real_llm_gate/` |

### 2. Policy & Governance

Zero-trust policy enforcement — the core value prop of manglekit.

| Example | Description | API Key | Run |
|---|---|---|---|
| **devops_policy_gate** | CI/CD security gates blocking dangerous Terraform/K8s operations | No | `go run ./devops_policy_gate/` |
| **compliance_proof** | GDPR as tiered Datalog — `AssessPlan` renders `AuditTrail` as machine-checkable proof | No | `go run ./compliance_proof/` |
| **jailbreak_proof_agent** | T0 taint axiom blocks data exfiltration — mock LLM complies with injection but kernel holds | No | `go run ./jailbreak_proof_agent/` |
| **verified_reasoning** | Cheap model + symbolic verifier = certified-correct output via verify-retry loop | No | `go run ./verified_reasoning/` |

### 3. Knowledge & Reasoning

Knowledge graphs, vector search, and hybrid RAG patterns.

| Example | Description | API Key | Run |
|---|---|---|---|
| **knowledge_graph_reasoning** | Load N-Triples knowledge graphs, define transitive Datalog rules, query with audit trails | No | `go run ./knowledge_graph_reasoning/` |
| **hybrid_rag** | Multi-tenant RAG with transitive access control and egress tainting | No (mocks) | `go run ./hybrid_rag/` |
| **silo_storage** | SessionStore (TTL), TransientFactsStore, N-Triples parsing, and Vector Store | No | `go run ./silo_storage/` |

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
| **ooda_document_generator** | Full 5-phase OODA loop with self-correction and Datalog policies | No | `go run ./ooda_document_generator/` |
| **ooda_east_generation** | `RunOODA` / `RunOODAEAST` with EAST steering, mixed-precision memory, Teacher-Student retry | No | `go run ./ooda_east_generation/` |
| **ooda_genkit_flow** | OODA loop exposed as Genkit HTTP flows — `DefineFlow`, `DefineStreamingFlow`, `FlowRegistry` | No (mock Brain) | `go run ./ooda_genkit_flow/` |
| **route_chaining** | `ROUTE` decision outcome, dynamic action chaining, paradox injection, SteerKB | No | `go run ./route_chaining/` |

### 5. Orchestration & Planning

Workflow engines, goal-driven planning, and multi-agent coordination.

| Example | Description | API Key | Run |
|---|---|---|---|
| **goal_based_planning** | Datalog-driven action planning with `client.Plan()` and `ExecutePlan()` | No | `go run ./goal_based_planning/` |
| **multi_agent_research** | Sequential, parallel, and hydrated workflow executors with session resume | No | `go run ./multi_agent_research/` |

### 6. Integration & Production

External system integrations, LLM bridges, and production infrastructure.

| Example | Description | API Key | Run |
|---|---|---|---|
| **mcp_tool_integration** | Model Context Protocol server integration with policy-gated tool execution | No | `go run ./mcp_tool_integration/` |
| **genkit_middleware_showcase** | Genkit middleware composition (Retry, Fallback, Tool Approval) | Yes | `go run ./genkit_middleware_showcase/` |
| **session_recovery** | Durable session state persistence and crash recovery | No | `go run ./session_recovery/` |
| **policy_copilot** | NL→Datalog rule generation with schema extraction, few-shot learning, and syntax verification | No (mock LLM) | `go run ./policy_copilot/` |
| **extractor_bridge** | LLM text→struct extraction — the neuro-symbolic bridge feeding Datalog | No (mock LLM) | `go run ./extractor_bridge/` |
| **production_resilience** | Circuit breaker (Closed→Open→HalfOpen) + OpenTelemetry tracing + middleware | No | `go run ./production_resilience/` |
| **persistent_store** | BadgerDB-backed session/knowledge store (`WithSyncWrites(true)` durability) with close/reopen restart-resume | No | `go run ./persistent_store/` |
| **http_service** | manglekit embedded in a `net/http` server: `/ask` endpoint executing a supervised action (403 on policy deny) | No | `go run ./http_service/` |

## Manglekit Features Demonstrated

| Feature | Package | Examples |
|---|---|---|
| Datalog policy engine | `core.Evaluator` | All examples |
| Zero-trust supervisor | `client.Supervise()` → `client.ExecuteByName()` | code_to_policy_extractor, devops_policy_gate, mcp_tool_integration, hybrid_rag, goal_based_planning, session_recovery, config_driven_app |
| Assess (policy evaluation) | `Engine().Assess()` | gherkin_policy, route_chaining, config_driven_app, knowledge_graph_reasoning |
| AssessPlan (pure policy-decision) | `Engine().AssessPlan()` | compliance_proof, knowledge_graph_reasoning, verified_reasoning, ooda_document_generator, jailbreak_proof_agent |
| AuditTrail rendering | `core.AuditTrail` | compliance_proof, knowledge_graph_reasoning, verified_reasoning, ooda_document_generator |
| Tiered governance (T0–T3) | `core.Tier` | compliance_proof, devops_policy_gate, ooda_document_generator |
| Goal-based planning | `client.Plan()` / `client.ExecutePlan()` | goal_based_planning |
| Function adapter | `adapters/func` | hybrid_rag, devops_policy_gate, code_to_policy_extractor |
| Knowledge graphs (N-Triples) | `adapters/knowledge` | knowledge_graph_reasoning, hybrid_rag, silo_storage |
| Hybrid memory (RAG) | `sdk.HybridMemory` | hybrid_rag |
| Vector store | `adapters/vector` | hybrid_rag, silo_storage |
| External predicates | `client.RegisterExternalPredicate()` | hybrid_rag |
| EvaluateSteering | `Engine().EvaluateSteering()` | hybrid_rag, route_chaining |
| Security / taint labels | `core.Envelope.SecurityLabels` | jailbreak_proof_agent, hybrid_rag |
| Policy violation detection | `core.IsPolicyViolationError()` | code_to_policy_extractor, devops_policy_gate, mcp_tool_integration, hybrid_rag, goal_based_planning |
| OODA cognitive loop (5-phase) | `sdk/ooda` | ooda_document_generator |
| RunOODA / RunOODAEAST | `sdk/ooda` | ooda_east_generation |
| EAST steering (entropy/saliency) | `sdk/ooda.EASTState` | ooda_east_generation, route_chaining |
| Mixed-precision memory | `ooda.PinAxiom` / `AddContext` / `ShaveContext` | ooda_east_generation |
| Tool registry & dispatcher | `ooda.Registry` / `ooda.Dispatcher` | ooda_east_generation, ooda_document_generator |
| OODA as Genkit flow | `adapters/ai.OODAFlow` | ooda_genkit_flow |
| FlowRegistry | `adapters/ai.FlowRegistry` | ooda_genkit_flow |
| ROUTE decision (dynamic chaining) | `core.DecisionRoute` | route_chaining |
| Paradox injection | `EASTState.ShouldInjectParadox()` | route_chaining |
| MCP integration | `adapters/mcp` | mcp_tool_integration |
| Genkit middleware | `adapters/ai` | genkit_middleware_showcase, ooda_genkit_flow |
| Session state recovery | `core.StateProvider` | session_recovery |
| Durable state (checkpoint/hydrate) | `core.SessionState` | session_recovery, multi_agent_research |
| Multi-agent runtime | `multiagent.AgentSystem` | multi_agent_research |
| Workflow executors (sequential/parallel/hydrated) | `multiagent.WorkflowExecutor` | multi_agent_research |
| Condition evaluation (Datalog) | `AgentSystem.EvaluateCondition()` | multi_agent_research |
| Taint labels (security) | `core.Envelope.SecurityLabels` | jailbreak_proof_agent, hybrid_rag |
| Symbolic verification | `Engine().Query()` | verified_reasoning |
| Gherkin → Datalog compilation | `client.LoadGherkinPolicy()` | gherkin_policy |
| Generics API | `sdk.Define[In,Out]()` | config_driven_app |
| Declarative YAML config | `config.Load()` / `sdk.WithConfig()` | config_driven_app |
| Provider registry | `sdk.RegisterProvider()` / `sdk.WithProviderConfig()` | config_driven_app |
| NL→Datalog policy copilot | `sdk.NewPolicyGenerator()` | policy_copilot |
| Schema extraction (mangle tags) | `extractor.New()` / `Generator.extractSchema()` | policy_copilot, extractor_bridge |
| Text→struct extraction | `adapters/extractor` | extractor_bridge |
| Circuit breaker | `adapters/resilience.CircuitBreaker` | production_resilience |
| OpenTelemetry tracing | `sdk.WithStdoutTracer()` | production_resilience |
| Scenario runner (BDD tests) | `scenario.Run()` | jailbreak_proof_agent |
| Struct → Datalog (Zero-Config Reflection) | `mangle` struct tags | code_to_policy_extractor, hybrid_rag, policy_copilot |
| QuickClient / RegisterSupervised | `manglekit.QuickClient` / `Client.RegisterSupervised` | real_llm_gate, http_service |
| cwd-safe fixture loading | `manglekit.MustReadFile` | hybrid_rag, compliance_proof, route_chaining, verified_reasoning, real_llm_gate, http_service |
| Test doubles (mocks) | `manglekit/testutil` (MockLLM, DeterministicEmbedder, InMemoryStateProvider, WorkflowSessionStore) | hybrid_rag, silo_storage, session_recovery, multi_agent_research, real_llm_gate, http_service |
| Durable BadgerDB state | `WithSyncWrites(true)` + batch fact writes | persistent_store |

## Behavior notes

These examples pin the **current** architecture behavior:

- **Supervisor pre-check is the trustworthy gate.** Every `Supervise` + `ExecuteByName` example
  relies on the pre-check (VerifyAtoms) for enforcement.

- **Post-check (Reflect) enforces detected violations but is fail-open on verifier errors.**
  The post-check at `internal/supervisor/action.go:140` returns `PolicyViolationError` when
  `!res.Pass`, but skips enforcement if `VerifyAtoms` itself returns an error (`err != nil`
  guard). This matches the "closed by default" design — the kernel blocks on hard
  violations (T0/T1) before and after execution, but defers to the caller on internal
  verifier failures.

- **Fail-closed is the only mode (v0.6).** `WithFailMode` was removed: the
  pre-check gate always fails closed, and text-extraction failures block
  execution. Block behavior is decided by policy alone.

- **Datalog escaping is applied** on the live path (`atomToDatalog` at `sdk_adapter.go:22`)
  via `engine.EscapeString` and an identifier-regex predicate check, and was hardened
  in v0.5.0 to cover the `multiagent` paths (`agent_system.go:269`, `workflow_executor.go:155`).
  Facts built from dynamic/trusted-local values are safe.

- **`ooda.NewLoop` (used by ooda_document_generator) is experimental** (ROADMAP §P3); the
  canonical entry points are `ooda.NewBuilder()...Build()` + `ooda.RunOODA`/`RunOODAEAST`.
  `RunOODAEAST` itself is experimental — it has no production caller yet and is
  exercised by tests and the `ooda_east_generation` example only.

- **`client.Execute` is steering-gated** (errors unless `WithSteeringEnabled`). Examples
  prefer `ExecuteByName`/`generator.Generate`.

## Testing

```bash
go test ./...
```

All examples include tests. No external API keys required for tests — mocks are used where needed.

## Repo Structure

```
manglekit-examples/
  code_to_policy_extractor/    -- Dynamic Architecture Linter
  compliance_proof/            -- GDPR tiered Datalog with audit proof
  config_driven_app/           -- YAML config + generics API + provider registry  ★
  devops_policy_gate/          -- CI/CD Security Gates
  extractor_bridge/            -- LLM text→struct neuro-symbolic bridge          ★
  genkit_middleware_showcase/  -- Genkit middleware composition
  gherkin_policy/              -- BDD Gherkin → Datalog compilation               ★
  goal_based_planning/         -- Datalog-driven action planning
  hybrid_rag/                  -- Multi-Tenant RAG with access control
  jailbreak_proof_agent/       -- T0 taint axiom blocks exfiltration
  knowledge_graph_reasoning/   -- N-Triples knowledge graph reasoning
  mcp_tool_integration/        -- Model Context Protocol integration
  multi_agent_research/        -- Multi-agent workflow orchestration
  ooda_document_generator/     -- Full 5-phase OODA loop
  ooda_east_generation/        -- RunOODA/RunOODAEAST with EAST steering
  ooda_genkit_flow/            -- OODA as Genkit HTTP flow + streaming           ★
  policy_copilot/              -- NL→Datalog rule generation                     ★
  production_resilience/       -- Circuit breaker + OpenTelemetry + middleware    ★
  route_chaining/              -- ROUTE decision + paradox injection             ★
  session_recovery/            -- Durable state persistence
  silo_storage/                -- SessionStore, TransientFacts, Vector Store
  verified_reasoning/          -- Symbolic verify-retry loop
  real_llm_gate/               -- Minimal supervised LLM call (QuickClient)     ★
  persistent_store/            -- BadgerDB durable session/knowledge store     ★
  http_service/                -- manglekit behind a net/http /ask endpoint    ★
```

Each example has its own `package main` and can be run independently. ★ = new in this release.

## See also

- [manglekit](https://github.com/duynguyendang/manglekit) — the SDK
