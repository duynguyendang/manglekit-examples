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

- Go 1.24+
- A Google AI API key (`GOOGLE_API_KEY`) for examples marked "Requires API key"

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

Examples are ordered by complexity. Start with the basics and work your way up.

### Beginner

| Example | Description | API Key | Run |
|---|---|---|---|
| **code_to_policy_extractor** | Dynamic Architecture Linter enforcing Clean Architecture rules on PRs | No | `go run ./code_to_policy_extractor/` |

### Intermediate

| Example | Description | API Key | Run |
|---|---|---|---|
| **knowledge_graph_reasoning** | Load N-Triples knowledge graphs, define transitive Datalog rules, query with audit trails | No | `go run ./knowledge_graph_reasoning/` |
| **goal_based_planning** | Datalog-driven action planning with `client.Plan()` and `ExecutePlan()` | No | `go run ./goal_based_planning/` |
| **devops_policy_gate** | CI/CD security gates blocking dangerous Terraform/K8s operations | No | `go run ./devops_policy_gate/` |
| **session_recovery** | Durable session state persistence and crash recovery | No | `go run ./session_recovery/` |

### Advanced

| Example | Description | API Key | Run |
|---|---|---|---|
| **mcp_tool_integration** | Model Context Protocol server integration with policy-gated tool execution | No | `go run ./mcp_tool_integration/` |
| **hybrid_rag** | Multi-tenant RAG with transitive access control and egress tainting | No (mocks) | `go run ./hybrid_rag/` |
| **ooda_document_generator** | Full 5-phase OODA loop with self-correction and Datalog policies | No | `go run ./ooda_document_generator/` |

> **Note:** hybrid_rag's access-control and egress scenarios run on the full
> `client.Supervise()` pre-check path: `ExecuteByName` recalls memory
> (`RecallWithFacts` injects `memory_hit_N` metadata), the supervisor
> pre-flight check evaluates the policy with the request's metadata, labels,
> and `action_operation`/payload facts, and the action only executes on
> PROCEED — enforcement happens in the kernel, not in demo code.

### Security & Verification

| Example | Description | API Key | Run |
|---|---|---|---|
| **jailbreak_proof_agent** | T0 taint axiom blocks data exfiltration — mock LLM complies with injection but kernel holds | No | `go run ./jailbreak_proof_agent/` |
| **compliance_proof** | GDPR as tiered Datalog — AssessPlan renders AuditTrail as machine-checkable proof | No | `go run ./compliance_proof/` |
| **verified_reasoning** | Cheap model + symbolic verifier = certified-correct output via verify-retry loop | No | `go run ./verified_reasoning/` |

### LLM-Powered (Requires API Key)

| Example | Description | API Key | Run |
|---|---|---|---|
| **genkit_middleware_showcase** | Genkit 1.7 middleware composition (Retry, Fallback, Tool Approval) | Yes | `go run ./genkit_middleware_showcase/` |

## Manglekit Features Demonstrated

| Feature | Package | Examples |
|---|---|---|
| Datalog policy engine | `core.Evaluator` | All examples |
| Knowledge graphs | `adapters/knowledge` | knowledge_graph_reasoning, hybrid_rag |
| Action planning | `sdk.Plan()` | goal_based_planning |
| OODA cognitive loop | `sdk/ooda` | ooda_document_generator |
| MCP integration | `adapters/mcp` | mcp_tool_integration |
| Hybrid memory (RAG) | `sdk.HybridMemory` | hybrid_rag |
| Genkit middleware | `adapters/ai` | genkit_middleware_showcase |
| Session state recovery | `core.StateProvider` | session_recovery |
| Supervisor (zero-trust) | `client.Supervise()` → `client.ExecuteByName()` | code_to_policy_extractor, devops_policy_gate, mcp_tool_integration, hybrid_rag, goal_based_planning, session_recovery |
| Function adapter | `adapters/func` | hybrid_rag |
| Taint labels (security) | `core.Envelope.SecurityLabels` | jailbreak_proof_agent, hybrid_rag |
| Tiered governance (T0-T3) | `core.Tier` | compliance_proof, devops_policy_gate |
| AuditTrail rendering | `core.AuditTrail`, `NewAuditRecordFromTrail` | compliance_proof |
| Symbolic verification | `engine.Query()` | verified_reasoning |

## Examples pin current (regressed) library behavior

These examples are written against the **real** `manglekit` architecture and
deliberately pin its *current* behavior — including the three regressions
documented in `docs/CODE_REVIEW.md` (re-audit 2026-07-15):

- **P0.1 — supervisor post-check (Reflect) is fail-open** (`internal/supervisor/action.go:140`,
  `err == nil && !res.Pass`). Examples treat the **pre-check** as the trustworthy gate.
- **P0.3 — `WithFailMode` is a no-op for the policy gate.** It is only consulted in the
  text-extraction fallback (`internal/supervisor/sdk_adapter.go:271`); the verify/Reflect
  gates ignore it. Examples do **not** claim `WithFailMode` changes block behavior.
- **P0.5 — Datalog escaping.** The live path's `atomToDatalog` (`sdk_adapter.go:22`) *does*
  escape subject/object and validate the predicate via `engine.EscapeString` and an
  identifier regex; facts built from dynamic/trusted-local values are safe. The examples
  do not rely on escaping for LLM-derived input and comment accordingly.

Governance is demonstrated through `client.Supervise(action)` + `client.ExecuteByName(...)`.
Direct `client.Engine().Assess`/`AssessPlan` calls bypass the supervisor and (for
non-supervised paths) do not inject `action_operation`/`meta`/`label` facts, so they carry
an explicit comment and manually append `action_operation("Req", Name).` where a halt rule
requires it. `client.Execute` is steering-gated (errors unless `WithSteeringEnabled`), so
examples prefer `ExecuteByName`/`generator.Generate`.

`ooda.NewLoop` (used by `ooda_document_generator`) is **experimental** (ROADMAP §P3); the
canonical entry points are `ooda.NewBuilder()...Build()` + `ooda.RunOODA`/`RunOODAEAST`.

## Testing

```bash
go test ./...
```

All examples include tests. No external API keys required for tests — mocks are used where needed.

## Repo Structure

```
manglekit-examples/
  code_to_policy_extractor/    -- Dynamic Architecture Linter
  devops_policy_gate/          -- CI/CD Security Gates
  genkit_middleware_showcase/  -- Genkit 1.7 middleware composition
  goal_based_planning/         -- Datalog-driven action planning
  hybrid_rag/                  -- Multi-Tenant RAG with access control
  knowledge_graph_reasoning/   -- N-Triples knowledge graph reasoning
  mcp_tool_integration/        -- Model Context Protocol integration
  ooda_document_generator/     -- Full 5-phase OODA loop
  session_recovery/            -- Durable state persistence
  jailbreak_proof_agent/       -- T0 taint axiom blocks exfiltration
  compliance_proof/            -- GDPR tiered Datalog with audit proof
  verified_reasoning/          -- Symbolic verify-retry loop
```

Each example has its own `package main` and can be run independently.

## See also

- [manglekit](https://github.com/duynguyendang/manglekit) — the SDK
