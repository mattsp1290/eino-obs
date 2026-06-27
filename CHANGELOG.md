# Changelog

All notable changes to `eino-obs` are documented here.

## v0.1.0 - Unreleased

Initial release of Datadog AI/LLM observability helpers for Eino-based Go
agents.

### API

- Added the root `einoobs` observer API with `New`, functional options, and
  safe no-network defaults.
- Added session and run helpers: `StartSession`, `StartRun`, terminal `End`,
  and terminal `Error` methods.
- Added model call instrumentation with provider/model identity, latency,
  token usage, output summary support, and terminal failure/cancellation
  fields.
- Added streaming instrumentation for chunks, first-token timing, token usage,
  stream completion, stream errors, and cancellation events.
- Added tool instrumentation for registered/materialized tools, tool call
  spans, AG-UI tool lifecycle helpers, and settled tool events.
- Added lifecycle helpers for retries, compaction, interrupts, resumes,
  cancellations, and generic error events.
- Added correlation helpers for trace, session, run, parent observation, and
  tool-call identity propagation through `context.Context`.
- Added `Flush` and `Shutdown` synchronization points for exporter delivery
  and lifecycle errors.

### Redaction And Safe Defaults

- The default observer path is no-network and does not require Datadog
  credentials.
- Raw prompt text, model outputs, tool inputs/outputs, provider request and
  response bodies, attachments, reasoning text, and encrypted reasoning are not
  captured by default.
- Caller-provided summaries are opt-in through `RedactionOptions`.
- Summary and metadata retention is bounded, defensively copied, and annotated
  with redaction records when fields are omitted or truncated.
- Sensitive summary names, metadata keys, and encrypted reasoning identifiers
  are omitted before recorder snapshots or exporter payloads are produced.

### No-Network And Fake Recorder

- Added the in-process no-network exporter used by default for local
  development, examples, and tests.
- Added `Observer.Snapshot` and `Observer.Reset` for inspecting and resetting
  no-network observations.
- Added the `recorder` package with defensive snapshots of normalized spans,
  events, dropped observations, errors, counters, and dirty state.
- Added the `exporter/fake` package for tests that need an `Exporter`
  implementation with capacity limits, operation counters, snapshots, reset,
  flush/shutdown behavior, and deterministic failure injection.

### Datadog Exporter

- Added `exporter/datadog`, a Datadog-compatible HTTP exporter for LLM
  observation span intake.
- Added direct configuration and environment-variable resolution for API key,
  site, endpoint override, ML app, service, environment, version, timeout,
  batching, payload size, retry behavior, credential validation, and
  compression.
- Added site resolution for supported Datadog sites and localhost endpoint
  support for tests and examples.
- Added batching, payload-size splitting, gzip compression by default, retry
  classification, pending retryable failures, dropped non-retryable failures,
  and shutdown draining.
- Added Datadog payload mapping for normalized spans, events, metrics,
  metadata, errors, and redaction records.

### Examples And Documentation

- Added README documentation for installation, minimal usage, API overview,
  safe defaults, no-network mode, fake exporters, Datadog export
  configuration, supported sites, and validation gates.
- Added package documentation and Go examples for minimal session
  instrumentation, model/stream/tool lifecycle instrumentation, no-network
  operation, and Datadog-compatible local fake endpoint export.
- Added design documentation under `docs/prompts/docs/` for public API,
  schema, redaction, recorder behavior, failure surface, exporter strategy,
  exporter configuration, examples, and release pinning.

### Validation

- Added unit and integration coverage for normalized span/event shapes,
  hierarchy, correlation fields, timestamps, latency, token usage, redaction,
  fake recorder behavior, exporter configuration, exporter failures,
  no-network behavior, examples, and public helper workflows.
- Added `Makefile` gates for `make fmt-check`, `make vet`, `make test`,
  `make race`, and `make check`.
- Added GitHub Actions CI for gofmt check, `go vet ./...`, and
  `go test ./...`.
- Final local release-readiness gates passed with no live Datadog credentials:
  `make fmt-check`, `make vet`, `make test`, `make race`, `go build ./...`,
  and `git diff --check`.

### Known Limitations

- This release targets Go 1.24 or newer in the Go 1.x release line.
- The Datadog exporter targets Datadog LLM Observability span intake; OTLP
  export is not included in `v0.1.0`.
- Live Datadog credential validation is opt-in. Normal tests, examples, and
  validation gates use no-network exporters or local fake endpoints.
- The library does not generate summaries from raw prompts, completions, tool
  payloads, attachments, provider bodies, or reasoning data. Applications must
  provide any safe summaries explicitly.
- Snapshot and fake-exporter APIs are intended for tests, local tooling, and
  validation rather than long-term durable telemetry storage.
