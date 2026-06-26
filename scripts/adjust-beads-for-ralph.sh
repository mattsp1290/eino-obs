#!/bin/bash
# Adjust the existing eino-obs Beads graph for Ralph execution.
# Generated/applied: 2026-06-26

set -euo pipefail

EPIC="eino-obs-6on"

u() {
    local id="$1"
    local description="$2"
    local acceptance="$3"
    bd update "$id" --description "$description" --acceptance "$acceptance"
}

echo "Adding Ralph-visible descriptions and acceptance criteria..."

u "$EPIC.1" \
  "Reservation: go.mod, go.sum, README.md. Scope: initialize module github.com/mattsp1290/eino-obs, choose supported Go version, document dependency policy and platform assumptions without adding exporter SDK dependencies yet." \
  "go.mod exists with the selected module path and Go version; README or docs record the Go version/dependency policy; go test ./... succeeds or the no-package state is explicitly documented."

u "$EPIC.2" \
  "Reservation: package directories only, including internal/model, internal/exporter, internal/redaction, recorder or exporter/fake, examples, and tests. Scope: scaffold empty/minimal packages without committing public API behavior." \
  "Package layout exists and compiles; package names match the architecture direction; no concrete eino-agent, provider, AG-UI, or Datadog transport types are imported."

u "$EPIC.3" \
  "Reservation: Makefile, scripts, README/docs. Scope: document and, where useful, script local quality gates for gofmt, go vet, go test, and race testing." \
  "Documented commands exist; commands are usable from repo root; race testing expectations note any practical exclusions."

u "$EPIC.4" \
  "Reservation: .github/workflows/**. Scope: add CI for the same Go quality gates used locally." \
  "Workflow runs gofmt check, go vet, and go test ./...; it does not require live Datadog credentials."

u "$EPIC.5" \
  "Reservation: docs/prompts/docs/**. Scope: create concrete context files for architecture decisions, schema, redaction, fake recorder, exporter config, examples, and release pinning." \
  "Each follow-on design bead has an obvious file to edit; docs index links those files; no design decision is hidden only in issue text."

u "$EPIC.6" \
  "Reservation: docs/prompts/docs/export-strategy.md. Scope: choose the first release export strategy and explain why, including whether the internal abstraction remains dual-capable." \
  "Decision selects exactly one real Datadog-compatible export path for v0.1.0; tradeoffs for Datadog HTTP API and OTel/OTLP are recorded; implementation beads can proceed without re-deciding transport."

u "$EPIC.7" \
  "Reservation: docs/prompts/docs/public-api.md. Scope: define exported types and helper style for sessions, runs, model calls, streams, tools, lifecycle events, context propagation, and failure handling call sites." \
  "Contract uses adapter-friendly primitives and structs; it avoids concrete eino-agent/eino-agui/provider types; examples show intended call shapes."

u "$EPIC.8" \
  "Reservation: docs/prompts/docs/schema.md. Scope: define normalized span/event kinds, hierarchy, attributes, timestamps, latency units, usage fields, error fields, and Datadog/OTel naming." \
  "Required and optional attributes are explicit; parent-child relationships are explicit; streaming and tool schemas are covered."

u "$EPIC.9" \
  "Reservation: docs/prompts/docs/redaction.md. Scope: define safe defaults, summary capture, truncation, omitted/redacted representation, and encrypted reasoning handling." \
  "Raw prompt/tool/attachment/reasoning payload capture is unsupported by default; encrypted reasoning is never emitted; summary limits and truncation are testable."

u "$EPIC.10" \
  "Reservation: docs/prompts/docs/fake-recorder.md. Scope: define recorder ordering, concurrency, reset, snapshot, inspection, and error injection behavior for no-network tests." \
  "Contract states concurrency guarantees, snapshot/reset semantics, failure injection knobs, and how tests inspect post-redaction normalized output."

u "$EPIC.11" \
  "Reservation: docs/prompts/docs/failure-surface.md. Scope: decide how exporter and observation failures surface through helpers, flush, shutdown, hooks, and recorder state." \
  "Instrumentation helpers do not panic on exporter failure; flush/shutdown behavior is explicit; testable error surfaces are listed."

u "$EPIC.12" \
  "Reservation: docs/prompts/docs/exporter-config.md. Scope: define env vars, constructor options, endpoint/site/API-key or OTLP config, and validation behavior for the selected exporter." \
  "All required and optional config fields are named; credential validation and auth errors are specified; no call site needs raw Datadog endpoint details."

u "$EPIC.13" \
  "Reservation: docs/prompts/docs/release-pinning.md. Scope: define the release/tag and eino-agent pinning response format." \
  "Document states expected semver tag or commit pin format and what eino-agent should record."

u "$EPIC.14" \
  "Reservation: internal/model/**. Scope: implement normalized in-memory span/event structs and validation helpers only." \
  "Types represent the schema contract; package stays internal; go test ./... passes."

u "$EPIC.15" \
  "Reservation: internal/redaction/** and redaction tests. Scope: implement redaction defaults, summaries, truncation, and redaction metadata over normalized model values." \
  "Sensitive raw fields are omitted; encrypted reasoning cannot be emitted; truncation is deterministic and tested."

u "$EPIC.16" \
  "Reservation: internal/exporter/** and exported interfaces where required. Scope: implement exporter/recorder interfaces and flush/shutdown semantics without concrete network transport." \
  "Interfaces match the failure-surface decision; no network dependency is required; fake and real exporters can implement the same contract."

u "$EPIC.17" \
  "Reservation: correlation/context files in public package and internal model usage. Scope: implement correlation structs and context.Context helpers." \
  "Session, run, agent, provider/model, tool call, assistant message, AG-UI thread, AG-UI run, and trace/span context fields can be propagated and tested."

u "$EPIC.18" \
  "Reservation: internal/model or internal/errors files. Scope: implement observation error classification and normalized error fields." \
  "Errors carry class/code/message/cause metadata as designed; helper code can record errors without panics."

u "$EPIC.19" \
  "Reservation: public package session/run files and focused tests. Scope: implement session admission and run lifecycle helpers only." \
  "Helpers create correctly parented normalized spans/events; correlation fields flow from context; focused tests or examples verify shapes."

u "$EPIC.20" \
  "Reservation: public package model-call files and focused tests. Scope: implement non-streaming provider/model call spans, usage, latency, retry, and error fields." \
  "Model call helpers use normalized model, redaction, error classification, and correlation context; go test ./... passes."

u "$EPIC.21" \
  "Reservation: public package streaming files and focused tests. Scope: implement streaming turn start/chunk/first-token/end/error/cancellation observations." \
  "First-token latency, total latency, token usage, retries, partial failures, and cancellation are represented as designed."

u "$EPIC.22" \
  "Reservation: public package tool files and focused tests. Scope: integrate the smaller tool instrumentation beads into a cohesive API surface." \
  "Registration/materialization, server-side execution/settlement, and AG-UI client-proposed tool helpers share consistent correlation, redaction, and error behavior."

u "$EPIC.23" \
  "Reservation: public package lifecycle event files and focused tests. Scope: integrate lifecycle event helper beads into one consistent event API." \
  "Interrupt/resume, retry/compaction, cancellation, and generic error events use shared schema, redaction, and correlation behavior."

u "$EPIC.24" \
  "Reservation: public package config/types files. Scope: implement public constructors, options, and documented stable exported types. No-network wiring remains in the dedicated no-network bead." \
  "Exported config/types match the public API contract; constructors do not expose transport details at every call site."

u "$EPIC.25" \
  "Reservation: recorder/**, exporter/fake/**, internal/recorder/**. Scope: implement the fake no-network recorder/exporter contract." \
  "Snapshot/reset/inspection are concurrency-safe; output is post-redaction normalized data; go test ./... passes."

u "$EPIC.26" \
  "Reservation: fake recorder files and tests. Scope: implement deterministic error injection for export, flush, and shutdown paths." \
  "Tests can force each failure path and observe the configured failure surface without network access."

u "$EPIC.27" \
  "Reservation: public package constructors/test helpers and fake recorder wiring. Scope: make no-network mode ergonomic for consumers and tests." \
  "Default/test constructors can use fake recorder without credentials; call sites remain transport-agnostic."

u "$EPIC.28" \
  "Reservation: selected exporter package files. Scope: integrate the selected real exporter client scaffold and payload mapping behind the exporter abstraction." \
  "Exactly one Datadog-compatible export path is available; it compiles behind the abstraction and does not affect fake recorder mode."

u "$EPIC.29" \
  "Reservation: selected exporter batching files and tests. Scope: integrate batch queue, payload limits, timestamps, flush/shutdown, and backpressure behavior." \
  "Batching honors limits and timestamps; flush/shutdown are deterministic; no live credentials are required for tests."

u "$EPIC.30" \
  "Reservation: selected exporter retry/config files and tests. Scope: integrate credential validation, endpoint/site auth behavior, retry, and backoff." \
  "Auth/config failures are classified; retries are bounded/testable; endpoint/site options match docs."

u "$EPIC.31" \
  "Reservation: selected exporter fake endpoint tests. Scope: test real exporter payloads and failure paths against local fake HTTP or OTLP endpoints only." \
  "Payload, retry, auth, flush, and shutdown tests run with go test ./... and require no live Datadog credentials."

u "$EPIC.32" \
  "Reservation: focused unit tests for normalized shapes. Scope: cover span/event hierarchy, correlation fields, timestamps, latency, and token usage." \
  "Tests fail on schema regressions and pass with go test ./..."

u "$EPIC.33" \
  "Reservation: redaction tests. Scope: prove sensitive defaults, omitted metadata, summary truncation, and encrypted reasoning non-emission." \
  "Tests include prompt/tool/attachment/reasoning cases and prove raw sensitive payloads do not leak."

u "$EPIC.34" \
  "Reservation: fake recorder tests. Scope: cover ordering, concurrency, snapshot, reset, inspection, and error injection behavior." \
  "Tests are deterministic under go test ./... and compatible with go test -race ./..."

u "$EPIC.35" \
  "Reservation: public helper test files. Scope: integrate smaller helper test beads into a coherent exported API coverage matrix." \
  "Coverage includes sessions, runs, model calls, streams, tools, retries, compaction, interrupts, cancellation, and errors."

u "$EPIC.36" \
  "Reservation: config and failure tests. Scope: cover config validation and exporter failure handling without live credentials." \
  "Tests exercise invalid/missing credentials and exporter failures through documented public surfaces."

u "$EPIC.37" \
  "Reservation: test fixes only. Scope: run and stabilize go test -race ./... for concurrent recorder/exporter paths where practical." \
  "Race test result is recorded; any practical exclusion is documented with rationale."

u "$EPIC.38" \
  "Reservation: README.md and docs. Scope: write user-facing overview, safe defaults, Datadog export configuration, no-network mode, and platform assumptions after APIs/exporter behavior exist." \
  "README includes minimal Eino-style usage, safe redaction defaults, fake recorder mode, and selected exporter configuration."

u "$EPIC.39" \
  "Reservation: package docs and examples. Scope: add Go package documentation and examples for session, model call, streaming, tool, and lifecycle instrumentation." \
  "Examples compile with go test ./...; docs avoid concrete eino-agent runtime dependencies."

u "$EPIC.40" \
  "Reservation: examples/**. Scope: create runnable examples for fake recorder mode and selected real exporter configuration." \
  "Examples are smoke-tested locally without live credentials unless explicitly skipped with documented reason."

u "$EPIC.41" \
  "Reservation: CHANGELOG.md or docs release notes. Scope: prepare v0.1.0 changelog/release notes after final verification and docs are complete." \
  "Release notes summarize API, redaction defaults, fake recorder, selected exporter, tests, and known limitations."

u "$EPIC.42" \
  "Reservation: docs/prompts/docs/release-pinning.md or dedicated release response file. Scope: record the exact tag or commit for eino-agent after final verification." \
  "File contains a concrete tag or commit SHA and the recommended eino-agent pinning instruction."

u "$EPIC.43" \
  "Reservation: no feature ownership; verification fixes only. Scope: run final gofmt, go vet ./..., go test ./..., and go test -race ./... where practical before release notes/pinning." \
  "All required gates pass or documented exceptions are justified; no live Datadog credentials are required."

echo "Splitting broad Ralph tasks into smaller prerequisite beads..."

TOOL_REG=$(bd create "Implement tool registration and materialization observation helpers" -p 0 --parent "$EPIC" --description "Reservation: public package tool registration/materialization files and focused tests. Scope: implement tool registration/materialization observations only." --acceptance "Helpers emit normalized tool registration/materialization spans or events with correlation fields and redaction behavior; go test ./... passes." --silent)
TOOL_SERVER=$(bd create "Implement server-side tool execution and settlement helpers" -p 0 --parent "$EPIC" --description "Reservation: public package server tool execution files and focused tests. Scope: implement server-side tool execution, result settlement, latency, and error observations only." --acceptance "Server-side tool helpers capture tool call ID, latency, status, redacted summaries, and errors according to schema; go test ./... passes." --silent)
TOOL_AGUI=$(bd create "Implement AG-UI client-proposed tool observation helpers" -p 0 --parent "$EPIC" --description "Reservation: public package AG-UI tool files and focused tests. Scope: implement client-proposed tool observations using adapter-friendly primitives only." --acceptance "Helpers carry AG-UI thread/run IDs and tool call IDs without importing concrete AG-UI runtime types; go test ./... passes." --silent)

LIFE_INTERRUPT=$(bd create "Implement interrupt and resume lifecycle event helpers" -p 1 --parent "$EPIC" --description "Reservation: public package lifecycle interrupt/resume files and focused tests. Scope: implement interrupt and resume events only." --acceptance "Events include session/run correlation, reason/status metadata, and no sensitive payload leakage; go test ./... passes." --silent)
LIFE_RETRY_COMPACT=$(bd create "Implement retry and compaction lifecycle event helpers" -p 1 --parent "$EPIC" --description "Reservation: public package lifecycle retry/compaction files and focused tests. Scope: implement retry and compaction events only." --acceptance "Events include attempt/count/classification metadata and compaction summaries without raw prompt payloads; go test ./... passes." --silent)
LIFE_CANCEL_ERROR=$(bd create "Implement cancellation and generic error lifecycle event helpers" -p 1 --parent "$EPIC" --description "Reservation: public package lifecycle cancellation/error files and focused tests. Scope: implement cancellation and generic observation error events only." --acceptance "Events use shared error classification and correlation context; helper failures do not panic; go test ./... passes." --silent)

EXPORT_CLIENT=$(bd create "Implement selected exporter configuration and client scaffold" -p 0 --parent "$EPIC" --description "Reservation: selected exporter package config/client files. Scope: create the real exporter constructor, config parsing, and client scaffold for the selected path only." --acceptance "Exporter can be constructed with documented options/env vars; no export occurs during construction except validation; fake recorder remains dependency-light." --silent)
EXPORT_MAPPING=$(bd create "Map normalized spans and events to selected exporter payloads" -p 0 --parent "$EPIC" --description "Reservation: selected exporter payload mapping files and tests. Scope: map normalized model values to Datadog HTTP or OTel/OTLP payload shapes selected by the architecture decision." --acceptance "Mapping covers spans, events, attributes, errors, token usage, timestamps, and redaction metadata; tests use local fixtures only." --silent)

BATCH_QUEUE=$(bd create "Implement exporter batch queue and payload limit handling" -p 1 --parent "$EPIC" --description "Reservation: selected exporter batching queue files and tests. Scope: implement queueing, batch sizing, payload limits, and overflow/backpressure policy." --acceptance "Batch limits and overflow behavior are deterministic and tested without network credentials." --silent)
BATCH_FLUSH=$(bd create "Implement exporter timestamp, flush, shutdown, and backpressure behavior" -p 1 --parent "$EPIC" --description "Reservation: selected exporter lifecycle files and tests. Scope: implement timestamp normalization, flush, shutdown, and backpressure behavior around the batch queue." --acceptance "Flush/shutdown drain or report failures according to the failure-surface contract; tests use fake endpoints." --silent)

AUTH_CONFIG=$(bd create "Implement exporter credential validation, endpoint/site config, and auth errors" -p 1 --parent "$EPIC" --description "Reservation: selected exporter auth/config files and tests. Scope: implement API key/site/endpoint or OTLP endpoint validation and auth error classification." --acceptance "Invalid config fails early with documented errors; auth failures from fake endpoints are classified and surfaced." --silent)
RETRY_BACKOFF=$(bd create "Implement exporter retry and backoff policy" -p 1 --parent "$EPIC" --description "Reservation: selected exporter retry files and tests. Scope: implement bounded retry/backoff for retryable exporter failures only." --acceptance "Retry count, delay policy, retryable status handling, and permanent failure behavior are tested with fake endpoints." --silent)

TEST_SESSION_MODEL=$(bd create "Add public helper tests for session, run, and model-call helpers" -p 0 --parent "$EPIC" --description "Reservation: public helper test files for session/run/model calls. Scope: test session/run/model helpers only." --acceptance "Tests verify shapes, hierarchy, correlation, token usage, latency, retries, and errors for non-streaming model calls." --silent)
TEST_STREAM_LIFE=$(bd create "Add public helper tests for streaming and lifecycle helpers" -p 0 --parent "$EPIC" --description "Reservation: public helper test files for streaming and lifecycle events. Scope: test streaming plus interrupt/resume/retry/compaction/cancellation/error helpers." --acceptance "Tests verify first-token latency, partial failures, cancellation, lifecycle event metadata, and redaction behavior." --silent)
TEST_TOOLS=$(bd create "Add public helper tests for tool instrumentation helpers" -p 0 --parent "$EPIC" --description "Reservation: public helper test files for tool observations. Scope: test registration/materialization, server-side execution/settlement, and AG-UI client-proposed tool helpers." --acceptance "Tests verify tool call IDs, AG-UI IDs, redacted summaries, latency, settlement status, and errors." --silent)

CONTRACT_FREEZE=$(bd create "Review and freeze API, schema, redaction, recorder, failure, and exporter config contracts" -p 0 --parent "$EPIC" --description "Reservation: docs/prompts/docs/** contract files only. Scope: reconcile design docs before implementation starts so Ralph agents consume stable contracts." --acceptance "Design docs are internally consistent; open questions are resolved or explicitly deferred; implementation beads depend on this freeze." --silent)
NO_NETWORK_VERIFY=$(bd create "Verify no-live-network and no-credentials test behavior" -p 1 --parent "$EPIC" --description "Reservation: tests and docs only. Scope: verify the full test suite does not require live Datadog credentials or external network by default." --acceptance "go test ./... passes with Datadog env vars unset; any networked tests are skipped or use local fake endpoints only." --silent)
EXAMPLE_SMOKE=$(bd create "Smoke-test runnable examples without live Datadog credentials" -p 1 --parent "$EPIC" --description "Reservation: examples/** and example tests. Scope: run or test examples for fake recorder and selected exporter configuration without contacting Datadog." --acceptance "Examples compile/run in no-network mode; real exporter example is documented or skipped safely without credentials." --silent)

echo "Adding missing and tightened dependencies..."

bd dep add "$EPIC.20" "$EPIC.14"
bd dep add "$EPIC.20" "$EPIC.17"
bd dep add "$EPIC.22" "$EPIC.14"
bd dep add "$EPIC.22" "$EPIC.17"
bd dep add "$EPIC.23" "$EPIC.14"
bd dep add "$EPIC.23" "$EPIC.15"
bd dep add "$EPIC.23" "$EPIC.17"
bd dep add "$EPIC.24" "$EPIC.7"
bd dep add "$EPIC.35" "$EPIC.19"

bd dep add "$CONTRACT_FREEZE" "$EPIC.7"
bd dep add "$CONTRACT_FREEZE" "$EPIC.8"
bd dep add "$CONTRACT_FREEZE" "$EPIC.9"
bd dep add "$CONTRACT_FREEZE" "$EPIC.10"
bd dep add "$CONTRACT_FREEZE" "$EPIC.11"
bd dep add "$CONTRACT_FREEZE" "$EPIC.12"

bd dep add "$EPIC.14" "$CONTRACT_FREEZE"
bd dep add "$EPIC.15" "$CONTRACT_FREEZE"
bd dep add "$EPIC.16" "$CONTRACT_FREEZE"
bd dep add "$EPIC.17" "$CONTRACT_FREEZE"
bd dep add "$EPIC.18" "$CONTRACT_FREEZE"
bd dep add "$EPIC.24" "$CONTRACT_FREEZE"
bd dep add "$EPIC.28" "$CONTRACT_FREEZE"

for bead in "$TOOL_REG" "$TOOL_SERVER" "$TOOL_AGUI"; do
    bd dep add "$bead" "$EPIC.7"
    bd dep add "$bead" "$EPIC.14"
    bd dep add "$bead" "$EPIC.15"
    bd dep add "$bead" "$EPIC.17"
done
bd dep add "$TOOL_SERVER" "$EPIC.18"
bd dep add "$TOOL_AGUI" "$EPIC.18"
bd dep add "$EPIC.22" "$TOOL_REG"
bd dep add "$EPIC.22" "$TOOL_SERVER"
bd dep add "$EPIC.22" "$TOOL_AGUI"

for bead in "$LIFE_INTERRUPT" "$LIFE_RETRY_COMPACT" "$LIFE_CANCEL_ERROR"; do
    bd dep add "$bead" "$EPIC.7"
    bd dep add "$bead" "$EPIC.14"
    bd dep add "$bead" "$EPIC.15"
    bd dep add "$bead" "$EPIC.17"
done
bd dep add "$LIFE_CANCEL_ERROR" "$EPIC.18"
bd dep add "$EPIC.23" "$LIFE_INTERRUPT"
bd dep add "$EPIC.23" "$LIFE_RETRY_COMPACT"
bd dep add "$EPIC.23" "$LIFE_CANCEL_ERROR"

bd dep add "$EXPORT_CLIENT" "$EPIC.6"
bd dep add "$EXPORT_CLIENT" "$EPIC.12"
bd dep add "$EXPORT_CLIENT" "$EPIC.16"
bd dep add "$EXPORT_MAPPING" "$EPIC.14"
bd dep add "$EXPORT_MAPPING" "$EPIC.15"
bd dep add "$EXPORT_MAPPING" "$EXPORT_CLIENT"
bd dep add "$EPIC.28" "$EXPORT_CLIENT"
bd dep add "$EPIC.28" "$EXPORT_MAPPING"

bd dep add "$BATCH_QUEUE" "$EPIC.28"
bd dep add "$BATCH_FLUSH" "$BATCH_QUEUE"
bd dep add "$EPIC.29" "$BATCH_QUEUE"
bd dep add "$EPIC.29" "$BATCH_FLUSH"

bd dep add "$AUTH_CONFIG" "$EPIC.28"
bd dep add "$AUTH_CONFIG" "$EPIC.11"
bd dep add "$RETRY_BACKOFF" "$AUTH_CONFIG"
bd dep add "$EPIC.30" "$AUTH_CONFIG"
bd dep add "$EPIC.30" "$RETRY_BACKOFF"

bd dep add "$TEST_SESSION_MODEL" "$EPIC.19"
bd dep add "$TEST_SESSION_MODEL" "$EPIC.20"
bd dep add "$TEST_STREAM_LIFE" "$EPIC.21"
bd dep add "$TEST_STREAM_LIFE" "$EPIC.23"
bd dep add "$TEST_TOOLS" "$EPIC.22"
bd dep add "$EPIC.35" "$TEST_SESSION_MODEL"
bd dep add "$EPIC.35" "$TEST_STREAM_LIFE"
bd dep add "$EPIC.35" "$TEST_TOOLS"

bd dep add "$EPIC.38" "$EPIC.24"
bd dep add "$EPIC.38" "$EPIC.27"
bd dep add "$EPIC.38" "$EPIC.28"
bd dep add "$EPIC.38" "$EPIC.29"
bd dep add "$EPIC.38" "$EPIC.30"
bd dep add "$EPIC.39" "$EPIC.23"
bd dep add "$EPIC.39" "$EPIC.24"
bd dep add "$EPIC.39" "$EPIC.27"
bd dep add "$EPIC.40" "$EPIC.29"
bd dep add "$EPIC.40" "$EPIC.30"
bd dep add "$NO_NETWORK_VERIFY" "$EPIC.31"
bd dep add "$NO_NETWORK_VERIFY" "$EPIC.36"
bd dep add "$EXAMPLE_SMOKE" "$EPIC.40"

bd dep add "$EPIC.41" "$EPIC.37"
bd dep add "$EPIC.41" "$EPIC.38"
bd dep add "$EPIC.41" "$EPIC.39"
bd dep add "$EPIC.41" "$EPIC.40"
bd dep add "$EPIC.43" "$NO_NETWORK_VERIFY"
bd dep add "$EPIC.43" "$EXAMPLE_SMOKE"

bd dep remove "$EPIC.43" "$EPIC.42" || true
bd dep add "$EPIC.41" "$EPIC.43"
bd dep add "$EPIC.42" "$EPIC.41"
bd dep add "$EPIC.42" "$EPIC.43"

echo "Graph adjustment complete."
echo "Run: bd ready && bd dep cycles && bd children $EPIC"
