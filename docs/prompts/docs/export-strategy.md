# Export Strategy Decision

This file is the contract for `eino-obs-6on.6`.

## Decision

For `v0.1.0`, implement exactly one real Datadog-compatible export path:

**Datadog LLM Observability HTTP intake API behind an internal exporter
abstraction.**

The first release should not implement an OpenTelemetry/OTLP exporter. The
internal normalized model and exporter interface should remain transport-neutral
enough that an OTel GenAI/OTLP exporter can be added later without changing the
public instrumentation API.

Primary docs checked on 2026-06-26:

- Datadog LLM Observability SDK/API instrumentation:
  <https://docs.datadoghq.com/llm_observability/instrumentation/api/>
- Datadog LLM Observability OpenTelemetry instrumentation:
  <https://docs.datadoghq.com/llm_observability/instrumentation/otel_instrumentation/>
- OpenTelemetry GenAI semantic conventions:
  <https://opentelemetry.io/docs/specs/semconv/gen-ai/>

## Rationale

The library goal is a stable Datadog AI/LLM observability API for `eino-agent`.
The first exporter should minimize semantic translation risk and avoid requiring
consumers to understand Datadog endpoint details at every call site.

The Datadog HTTP intake path is the most direct fit for the first release:

- It targets Datadog LLM Observability without depending on an OpenTelemetry
  collector, SDK, resource configuration, or semantic-convention maturity at
  call sites.
- It lets `eino-obs` own batching, payload limits, retries, flush/shutdown, and
  credential validation in one small exporter package.
- It keeps normal tests credential-free by using fake HTTP endpoints and the
  no-network recorder.
- It avoids exposing OpenTelemetry types in the public API while the normalized
  model, schema, redaction, and fake recorder contracts are still being
  stabilized.

## Tradeoffs: Datadog HTTP API

Advantages:

- Direct Datadog LLM Observability target for `v0.1.0`.
- No required OpenTelemetry SDK dependency in the first release.
- No collector or OTLP pipeline required for consumers.
- Easier fake-endpoint tests for auth, endpoint/site selection, retry,
  timestamp, batching, and payload-limit behavior.
- Clear separation between public instrumentation helpers and transport details.

Costs:

- `eino-obs` must implement HTTP payload shaping, batching, retry/backoff,
  timestamp handling, flush/shutdown, and error classification itself.
- Datadog-specific payload details live in the first real exporter package.
- A future OTel exporter will require a mapping pass from the normalized model
  to OpenTelemetry GenAI names.

## Tradeoffs: OpenTelemetry GenAI/OTLP

Advantages:

- Aligns with the OpenTelemetry ecosystem and may integrate naturally with
  existing collector pipelines.
- Provides a vendor-neutral semantic vocabulary for future exporter work.
- Could let deployments route observations through existing OTLP infrastructure.

Costs for `v0.1.0`:

- Adds OpenTelemetry SDK/OTLP dependencies before this repository has frozen its
  normalized schema, redaction policy, fake recorder, and failure surface.
- Pushes resource, span processor, exporter, and collector configuration into
  the first release scope.
- Requires immediate mapping decisions from `eino-obs` concepts to GenAI
  semantic conventions, even where Datadog LLM Observability may need
  Datadog-specific fields.
- Makes no-network default behavior and fake-recorder tests more complex.

## Abstraction Direction

Keep the internal abstraction dual-capable, but do not implement both transports
in `v0.1.0`.

Implementation guidance:

- Public helpers emit a normalized internal model, not Datadog HTTP structs and
  not OpenTelemetry SDK objects.
- `internal/exporter` owns batching, flush/shutdown contracts, retry policy, and
  transport-neutral exporter interfaces.
- The first real exporter package maps the normalized model to Datadog HTTP
  payloads.
- Datadog HTTP config belongs in the exporter constructor and environment
  parsing described by [exporter-config.md](exporter-config.md).
- OTel GenAI names can be recorded in [schema.md](schema.md) as a future mapping
  column, but implementation beads must not build an OTLP exporter for
  `v0.1.0`.

## Implementation Beads Unblocked

This decision unblocks:

- `eino-obs-6on.8`: define normalized schema and include Datadog HTTP mapping
  rules plus future OTel mapping notes.
- `eino-obs-6on.12`: define exporter config for Datadog HTTP intake, including
  API key, site, endpoint override, credential validation, fake endpoint tests,
  batching, retry/backoff, flush, shutdown, payload limits, and timestamps.
- `eino-obs-6on.28`: implement the selected Datadog HTTP exporter behind the
  exporter abstraction.
- `eino-obs-6on.50`: implement selected exporter configuration and client
  scaffold.

## Non-Goals For v0.1.0

- No OTLP exporter implementation.
- No OpenTelemetry SDK dependency in the public API.
- No requirement for a collector or live Datadog credentials in normal tests.
- No direct dependency on concrete `eino-agent`, provider, AG-UI, or Datadog SDK
  runtime types at instrumentation call sites.
