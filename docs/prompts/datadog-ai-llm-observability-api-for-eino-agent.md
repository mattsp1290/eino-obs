# Project Planning with Beads

## Agent Instructions

You are an expert software architect creating a comprehensive task breakdown. This task graph will be executed by AI agents working in parallel, coordinated through MCP Agent Mail with file reservations to prevent conflicts.

<quality_expectations>
Create a thorough, production-ready task graph. Include all necessary setup, implementation, testing, and documentation tasks. Go beyond the basics - consider edge cases, error handling, security considerations, and integration points. Each task should be specific enough for an agent to execute independently without ambiguity.
</quality_expectations>

## Project Information

### Links to Relevant Documentation

- Request file: `/Users/punk1290/.agents/projects/eino-obs/requests/2026-06-26-datadog-llm-observability-api-for-eino-agent.md`
- Consumer plan: `~/git/eino-agent/docs/prompts/eino-agent-go-runtime-for-ag-ui-and-datadog.md`
- Datadog Agent/LLM Observability overview: https://docs.datadoghq.com/llm_observability/
- Datadog LLM Observability SDK reference: https://docs.datadoghq.com/llm_observability/instrumentation/sdk/
- Datadog LLM Observability HTTP API: https://docs.datadoghq.com/llm_observability/instrumentation/api/
- Datadog OpenTelemetry LLM Observability instrumentation: https://docs.datadoghq.com/llm_observability/instrumentation/otel_instrumentation/

### Project Description

Build a Go library in `github.com/mattsp1290/eino-obs` that provides a stable Datadog AI/LLM observability API for the `github.com/mattsp1290/eino-agent` runtime. The repository currently has only a placeholder README, so the plan should include initial Go module setup, package structure, implementation, tests, documentation, and release preparation.

The library must expose agent-runtime instrumentation helpers for session/run root spans, provider/model call spans, streaming model turn spans with token usage and latency, tool call spans for server-side tools and client-proposed AG-UI tools, and events for interrupt/resume, retry, compaction, and errors. It must carry correlation fields for session ID, run ID, agent ID, provider, model, tool call ID, assistant message ID, AG-UI thread ID, and AG-UI run ID.

The consumer must not need to know Datadog endpoint details at every instrumentation call site. The task graph should include an early architecture decision that chooses and documents the export strategy: Datadog LLM Observability HTTP API, OpenTelemetry GenAI semantic conventions with Datadog-compatible OTLP export, or a thin abstraction that can support both. The preferred direction is a thin internal exporter abstraction with a no-network recorder/fake exporter first, then one real Datadog-compatible export path.

The task graph must make the architecture decision, public API contract, normalized span/event schema, redaction policy, and fake recorder contract explicit setup dependencies for implementation. Exporter implementation beads must depend on the export-strategy decision bead. Public helper implementation beads must depend on the API contract and normalized schema beads. Redaction-sensitive implementation and test beads must depend on the redaction policy bead.

Consumer contract from `eino-agent`: this library should remain an observability library, not an agent runtime. It should not compile against concrete `eino-agent`, `eino-agui`, provider, or AG-UI runtime types unless an explicit adapter package is added later. The stable API should use adapter-friendly Go primitives and structs that `eino-agent` can populate from runtime contexts containing session ID, agent ID, assistant message ID, tool call ID, provider/model identity, trace/span context, and cancellation. It must support instrumentation for session admission, run lifecycle, provider calls, streaming chunks, token usage, tool registration/materialization/execution/settlement, retries, compaction, interrupts, and error paths.

### Technical Stack

- Language: Go.
- Go version: select and document a current supported Go version during setup, with a preference for the same version used by nearby Eino repos if present.
- Module target: `github.com/mattsp1290/eino-obs`.
- Consumer: `github.com/mattsp1290/eino-agent`.
- Likely packages: root public instrumentation API package, internal event/span model package, exporter abstraction package, fake recorder/exporter package, Datadog or OTLP exporter package, examples package, and tests. Keep only documented packages stable; keep transport internals under `internal/` where possible.
- Testing: Go unit tests and integration-style tests that run with `go test ./...` and require no live Datadog credentials by default. Include `go vet ./...`, `gofmt`, and `go test -race ./...` where practical for concurrent recorder/exporter behavior.
- Documentation: README and package examples showing minimal Eino session/model/tool instrumentation and Datadog export configuration.
- External observability options to evaluate: Datadog LLM Observability HTTP API and OpenTelemetry GenAI semantic conventions with Datadog-compatible OTLP export.
- Dependency policy: keep dependencies minimal; add OpenTelemetry or Datadog SDK/API dependencies only behind the chosen real exporter path. The fake recorder and public API must work without credentials or network access.
- Supported platforms: standard Go-supported Linux and macOS development/test environments unless an architecture decision narrows this.

### Specific Requirements

- Safe defaults: redact or omit prompt text, tool inputs, tool outputs, reasoning, encrypted reasoning values, and attachments by default.
- Explicit capture options: allow consumers to opt in to caller-provided summaries for selected inputs/outputs. A summary is a bounded string or structured field map supplied by the consumer, not generated from raw payloads by this library. Default max size and truncation behavior must be documented and tested. Raw prompt/tool payload capture should remain unsupported unless a separate explicit design bead chooses otherwise.
- Redaction representation: omitted/redacted fields should be represented consistently in normalized output, for example absent fields plus redaction metadata, rather than placeholder text that could be confused with model data. Encrypted reasoning values must never be emitted, including through opt-in summary capture.
- Public API style: prefer idiomatic Go constructors and `context.Context` propagation; define whether spans are manually ended, closure/callback-based, or both. Define stable exported types for config, correlation context, token usage, stream observations, tool observations, redaction options, exporter interface, and recorder output before implementing helpers.
- Normalized data model: define span kinds, event names, required vs optional attributes, parent-child relationships, timestamp source, latency units, token usage fields, error fields/classification, and Datadog/OTel attribute naming rules before exporter work begins.
- Streaming semantics: define start/chunk/end/error events or spans for streaming model turns, including how token usage, first-token latency, total latency, retries, cancellation, and partial failures are represented.
- No-network mode: provide a fake exporter/recorder that captures post-redaction normalized span/event shapes for `eino-agent` tests.
- Fake recorder contract: specify ordering guarantees, concurrency behavior, reset/snapshot APIs, error injection hooks, and how tests inspect recorded spans/events.
- Tests: verify span/event shapes, correlation fields, token usage and latency fields, redaction defaults, opt-in summary capture behavior, fake exporter behavior, concurrency behavior, and exporter failure handling without live credentials.
- Error handling: create a design bead that chooses the exporter failure surface. The implementation must then consistently expose failures through the chosen mechanism, such as returned errors on flush/shutdown, an error hook, recorder state, or a combination documented in the public API. Instrumentation helpers must record observation errors without panics.
- Real exporter scope: after the export-strategy decision, implement exactly one Datadog-compatible export path for the first release. The corresponding task beads must cover batching, retry/backoff, flush/shutdown behavior, payload limits, timestamp handling, credential validation, auth errors, endpoint/site configuration, and test coverage with fake HTTP/OTLP endpoints.
- Configuration: document environment variables and constructor options for real Datadog export, including API key/site/endpoint or OTLP endpoint depending on the selected export path.
- API ergonomics: consumer call sites should be simple enough for Eino agent runtime code and should not scatter Datadog transport details throughout `eino-agent`.
- Release: include release-preparation beads for changelog/release notes, version/tag decision, and recording the exact tag or commit in `README.md` or a dedicated release response file for `eino-agent` to pin. Prefer semver-style tags such as `v0.1.0` for the first integration tag if the repository is ready for a tag.
- Out of scope: do not build the `eino-agent` runtime, do not own AG-UI conversion logic from `eino-agui`, and do not require live Datadog credentials for normal tests.
- Acceptance: `eino-agent` can instrument sessions, provider turns, streams, tools, interrupts, and failures through `eino-obs`; tests prove redaction defaults and fake-exporter behavior; documentation includes a minimal Eino model-call example and Datadog export configuration; `go test ./...` passes; a tag or commit is recorded for `eino-agent` to pin.

---

## Your Task

Analyze this project and create a comprehensive **Beads task graph** using the `bd` CLI. Beads provides dependency-aware, conflict-free task management for multi-agent execution.

---

<critical_constraint>
Your ONLY output is a bash shell script containing `bd create` and `bd dep add` commands. Do NOT use `bd add` — the correct command to create a bead is `bd create`. Use `bd dep add` for dependencies between task beads. Do not implement anything yourself.

The script MUST create a single parent **epic** first (`bd create -t epic`) and parent **every** task bead to it via `--parent "$EPIC"`, so the whole project is one trackable rollup. The epic is an organizational rollup only — never make it a blocking dependency (do NOT `bd dep add` to or from the epic; `bd dep add` is for real ordering edges between task beads, and a blocking edge on an epic both excludes it wrongly and inverts `bd dep tree`). Membership is the `--parent` relationship, nothing else.
</critical_constraint>

## Output Format

Generate a shell script that creates the full task graph. The generator's only output should be the script text. Do not execute the script while generating it. The script should:

1. **Initialize Beads** (if not already initialized)
2. **Create one parent epic** (`bd create -t epic`) representing the whole project, capturing its ID into `$EPIC`
3. **Create all task beads** with appropriate priorities, each parented to the epic via `--parent "$EPIC"`
4. **Establish dependencies** between task beads (ordering edges only — never to or from the epic)

### Example Output

```bash
#!/bin/bash
# Project: eino-obs
# Generated: 2026-06-26

set -e

# Initialize beads if needed
if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating project beads..."

# ========================================
# Parent epic — every task below is parented to it (--parent "$EPIC").
# The epic is an organizational rollup: it is NEVER given a blocking dep
# (no `bd dep add` to or from it) and is never dispatched as work itself.
# ========================================

EPIC=$(bd create "Epic: eino-obs" -t epic -p 0 --silent)
bd update "$EPIC" --status in_progress   # rollup, not dispatchable work — keep it out of `bd ready`

# ========================================
# Phase 1: Project Setup & Infrastructure
# ========================================

SETUP_GO_MODULE=$(bd create "Initialize Go module and package layout" -p 0 --parent "$EPIC" --silent)

SETUP_CI=$(bd create "Configure Go test and lint workflow" -p 1 --parent "$EPIC" --silent)
bd dep add $SETUP_CI $SETUP_GO_MODULE

SETUP_DOCS=$(bd create "Create documentation skeleton and examples directory" -p 1 --parent "$EPIC" --silent)
bd dep add $SETUP_DOCS $SETUP_GO_MODULE

# ... continue for all phases ...

echo ""
echo "Bead graph created! View with:"
echo "  bd show $EPIC          # The parent epic and its rollup"
echo "  bd children $EPIC      # All task beads under the epic"
echo "  bd ready              # List unblocked tasks (the epic itself is not work)"
```

---

## Bead Creation Guidelines

### Epic / Hierarchy (REQUIRED)
- Create exactly **one parent epic** for the whole project: `EPIC=$(bd create "Epic: <project summary>" -t epic -p 0 --silent)`.
- Parent **every** task bead to it: add `--parent "$EPIC"` to every `bd create`.
- The epic is a **rollup, not work**: never `bd dep add` to or from it. Membership is `--parent`; `bd dep add` is reserved for real ordering edges *between task beads*. A blocking edge on an epic wrongly keeps it out of (or drops it into) `bd ready` and inverts `bd dep tree`.
- **Keep the epic out of `bd ready`** by marking it active right after creation: `bd update "$EPIC" --status in_progress`. `bd ready` excludes `in_progress`/`blocked`/`deferred`/`hooked`. Do **not** rely on `--exclude-type epic` — that flag is ineffective on some `bd`/`bn` builds, whereas status-based exclusion works everywhere.
- For very large projects you MAY use phase sub-epics (each `--parent "$EPIC"`, each with its own children), but a single top-level epic is the default and is sufficient for most projects.

### Priority Levels
- `-p 0` = Critical (blocking other work)
- `-p 1` = High (important but not blocking)
- `-p 2` = Medium (standard work)
- `-p 3` = Low (nice to have)

### Dependency Rules
1. Never create cycles
2. Every bead should have a clear dependency chain back to setup tasks
3. Use `bd dep add CHILD PARENT` (child depends on parent completing first)
4. Parallel work should share a common ancestor, not depend on each other
5. `bd dep add` is for ordering edges **between task beads only** — never use it to attach a task to the epic (that is `--parent`), and never add a blocking edge to or from the epic

### Task Granularity
- Each bead should be completable in **under 750 lines of code**
- Tasks should be atomic enough for one agent to complete without coordination
- If a task requires multiple file areas, consider splitting by file area

---

## File Reservation Planning

For each major work area, note the file patterns that will need exclusive reservation. If the available `bd create` supports descriptions, place reservation notes in descriptions; otherwise place reservation notes as shell comments immediately before the relevant `bd create` commands so future agents can preserve the reservation intent.

```bash
# Example reservation notes (add as bead descriptions)
# Public API and types: *.go, obs/**, api/**, tests touching exported API
# Export abstraction and fake recorder: exporter/**, internal/**, recorder/**, tests/exporter/**
# Datadog/OTLP exporter: exporter/datadog/** or exporter/otel/**, internal/datadog/**, tests/exporter/**
# Examples and docs: README.md, docs/**, examples/**
# CI and module setup: go.mod, go.sum, .github/workflows/**
```

This helps agents claim appropriate file surfaces when they start work.

---

## Context Documentation

Place any important context in `docs/prompts/docs/` for agents to reference. This includes:
- Architecture decision for Datadog HTTP API vs OpenTelemetry GenAI/OTLP vs dual-capable exporter abstraction
- Datadog environment variable and endpoint configuration notes
- Redaction policy and sensitive-field handling rules
- Public API examples for `eino-agent`
- Fake recorder/exporter contract for integration tests
- Normalized span/event schema and Datadog/OTel mapping rules
- Release/tag response for the `eino-agent` integration

---

## Verification Steps

After the script has been generated and saved by the human or orchestrator:

1. **Run it**: `chmod +x setup-beads.sh && ./setup-beads.sh`
2. **Check the rollup**: `bd children "$EPIC"` should list every task bead, and `bd dep tree` should show them under the epic with no orphan (un-parented) tasks
3. **Check ready work**: `bd ready` should show initial setup tasks and **not** the epic. Epics are rollups, never dispatched as work — mark the epic `in_progress` right after creating it so status-based exclusion keeps it out of `bd ready` on every build.

---

## Completeness Checklist

Ensure your task graph includes:

- [ ] A single parent epic (`-t epic`); every task bead parented to it via `--parent "$EPIC"`, with no orphan tasks and no blocking dep to/from the epic
- [ ] Go module initialization and package layout
- [ ] Go version, dependency policy, supported platform assumptions, `gofmt`, `go vet`, and race-test expectations
- [ ] Architecture decision task for Datadog HTTP API vs OpenTelemetry GenAI/OTLP vs dual-capable abstraction, with exporter implementation depending on this decision
- [ ] Public API design for session/run, model call, streaming turn, tool call, lifecycle events, error events, context propagation, lifecycle method style, and exporter failure surface
- [ ] Shared span/event data model with span kinds, event names, hierarchy, required/optional attributes, timestamp/latency units, token usage fields, error fields, correlation fields, and Datadog/OTel mapping rules
- [ ] Redaction defaults, redaction representation, caller-provided summary capture options, size limits, truncation behavior, and tests proving raw sensitive fields do not leak
- [ ] Fake recorder/exporter and no-network test mode, including ordering, concurrency, reset/snapshot, and error injection behavior
- [ ] Real Datadog-compatible export path with batching, retries, flush/shutdown, payload limits, timestamp handling, credential validation, and fake endpoint tests
- [ ] Unit and integration-style tests for shapes, redaction, fake exporter, config, concurrency, and exporter failures
- [ ] README, package docs, and minimal Eino model-call example
- [ ] Security and privacy review tasks
- [ ] Performance considerations for streaming and high-volume event paths
- [ ] CI/CD and release/tag preparation tasks, including where the pin-able tag or commit is recorded for `eino-agent`
- [ ] Clear dependency chains with no cycles
