# manglekit-examples

Example applications demonstrating [Manglekit](https://github.com/duynguyendang/manglekit) — an AI governance framework with policy-based guardrails.

## ⚠️ Security

This repository historically contained a populated `GOOGLE_API_KEY` in `.env` that has since been removed. **If you previously cloned this repo, rotate your Google API key in the [Google Cloud Console](https://console.cloud.google.com/apis/credentials) immediately.** The `.env` file is gitignored; the example now ships with only a placeholder.

## Prerequisites

- Go 1.24+
- A Google AI API key (`GOOGLE_API_KEY`) for any example that uses an LLM provider. Without it, `hybrid_rag` falls back to a mock embedder (no real API calls), `config_driven_bot` and `logistics_optimizer` require it to run.

## Setup

```bash
# Clone with the manglekit SDK as a sibling directory
git clone <this-repo> manglekit-examples
git clone <manglekit-repo> manglekit
```

```bash
# Copy and populate the environment file
cp .env.example .env
# Edit .env and set GOOGLE_API_KEY=your-key
```

```bash
# From the manglekit-examples root
cd manglekit-examples
```

## Examples

| Example | Description | Run command |
|---|---|---|
| **autonomous_router** | Supervised agent with retry correction and tier-based routing (gold → VIP agent). No external deps. | `go run ./autonomous_router/` |
| **config_driven_bot** | LLM chat agent configured via YAML. Uses Google Gemini. | `go run ./config_driven_bot/` |
| **infrastructure_copilot** | Kubernetes safety guardrail — blocks delete on critical pods and writes during peak hours. No external deps. | `go run ./infrastructure_copilot/` |
| **logistics_optimizer** | Seating arrangement solver with LLM-generated JSON + Datalog constraint validation. Uses Google Gemini. | `go run ./logistics_optimizer/` |
| **hybrid_rag** | RAG pipeline with transitive access control, PII detection via retry, and security taint propagation. Mock LLM — no API key needed for basic testing. | `go run ./hybrid_rag/` |
| **bdd_policies** | Gherkin-based governance policy definitions (`.feature` files). Documentation only; run via manglekit CLI. | See [README](bdd_policies/README.md) |

## Testing

```bash
go test ./...
```

The test suite includes smoke tests for each example that verify:
- Policies load without syntax errors
- Client construction succeeds
- Key positive and negative scenarios behave as expected

No external API keys required for smoke tests.

## Repo structure

```
manglekit-examples/
  autonomous_router/    -- Retry & route pattern
  bdd_policies/         -- Gherkin policy files
  config_driven_bot/    -- YAML-driven LLM agent
  hybrid_rag/           -- RAG + transitive access control
  infrastructure_copilot/ -- K8s safety guardrails
  logistics_optimizer/  -- LLM + Datalog constraint solving
```

Each example has its own `package main` and can be run independently.

## See also

- [manglekit](https://github.com/duynguyendang/manglekit) — the SDK
- [BDD Policy README](bdd_policies/README.md) — Gherkin policy authoring guide
