# Normalized Span And Event Schema

This file is the contract for `eino-obs-6on.8`. It defines the transport-neutral
model shared by public helpers, recorders, and exporters before mapping to the
selected Datadog LLM Observability trace spans HTTP API.

## Model Shape

The normalized model has two record types:

- `Span`: a timed operation with `StartTime`, optional `EndTime`, duration, kind,
  status, attributes, events, redaction metadata, and optional error fields.
- `Event`: an instantaneous observation attached to a span or emitted with
  correlation context.

All timestamps use `time.Time` in UTC. Durations and latencies use milliseconds
as integer values in normalized attributes. Exporters may convert units only at
transport boundaries.

## Span Kinds

Required span kinds:

- `session`: top-level session/admission scope.
- `run`: one agent run under a session.
- `model_call`: non-streaming provider/model call.
- `stream`: streaming model turn.
- `tool_call`: server-side or client-proposed tool execution.
- `export_flush`: optional internal span for explicit flush operations in fake
  recorder/exporter tests.
- `export_shutdown`: optional internal span for explicit shutdown operations in
  fake recorder/exporter tests.

Span status values:

- `ok`
- `error`
- `canceled`

## Event Names

Required event names:

- `stream.chunk`
- `stream.first_token`
- `tool.registered`
- `tool.materialized`
- `tool.settled`
- `retry`
- `compaction`
- `interrupt`
- `resume`
- `cancellation`
- `error`
- `observation.error`
- `redaction.applied`

Events use the same correlation, timing, redaction, and error field conventions
as spans where applicable.

## Parent-Child Hierarchy

The default hierarchy is:

```text
session
  run
    model_call
    stream
    tool_call
```

Rules:

- `session` is the root when `session.id` is present.
- `run` is the root when `session.id` is absent and `run.id` is present.
- model, stream, tool, and lifecycle events attach to the current run when
  `run.id` is present.
- tool spans can be children of a model/stream span when the consumer supplies a
  parent observation ID; otherwise they attach to the run.
- lifecycle events attach to the most specific active span when a handle emits
  them, otherwise to the run/session context.
- observations with no parent context remain root observations but still carry
  available correlation attributes.

## Required Attributes

Every span and event has:

- `obs.kind`: span kind or event category.
- `obs.name`: stable operation or event name.
- `obs.status`: `ok`, `error`, or `canceled` for spans; optional for events.
- `timestamp`: UTC occurrence time.
- `duration_ms`: required for ended spans.
- `correlation.*`: any non-empty correlation fields listed below.

Required correlation attributes when values are available:

- `correlation.session_id`
- `correlation.run_id`
- `correlation.agent_id`
- `correlation.assistant_message_id`
- `correlation.thread_id`
- `correlation.agui_run_id`
- `correlation.tool_call_id`

Empty correlation values are omitted.

## Optional Common Attributes

Optional attributes:

- `service.name`
- `service.env`
- `service.version`
- `metadata.*`
- `redaction.records`
- `error.type`
- `error.message`
- `error.classification`
- `error.retryable`
- `error.canceled`
- `error.dropped`

`metadata.*` values are already redacted and bounded by
[redaction.md](redaction.md).

## Model And Token Attributes

Model call and stream observations use:

- `genai.provider`: provider name.
- `genai.model`: model name.
- `genai.request.summary`: caller-provided input summary when enabled.
- `genai.response.summary`: caller-provided output summary when enabled.
- `genai.usage.input_tokens`
- `genai.usage.output_tokens`
- `genai.usage.total_tokens`
- `genai.usage.reasoning_tokens`
- `genai.usage.cached_input_tokens`
- `genai.latency.first_token_ms`
- `genai.latency.total_ms`
- `genai.retry.attempt`

Token counts are int64 and omitted when unknown. Unknown counts are not encoded
as zero unless the caller explicitly supplied zero.

## Streaming Schema

`stream` spans represent the full streaming turn. They include provider/model,
correlation, optional summaries, usage, first-token latency, total duration, and
terminal status.

`stream.chunk` events include:

- `stream.chunk.index`
- `stream.chunk.summary`
- `timestamp`

`stream.first_token` is emitted once when first-token latency is known. Partial
failures use a terminal `stream` span status of `error` or `canceled` and may
include partial token usage.

## Tool Schema

Tool observations use:

- `tool.name`
- `tool.call_id`
- `tool.kind`: `server`, `client_proposed`, or `unknown`
- `tool.status`: `registered`, `materialized`, `started`, `succeeded`,
  `failed`, or `canceled`
- `tool.input.summary`
- `tool.output.summary`
- `tool.latency.ms`

`tool.registered`, `tool.materialized`, and `tool.settled` are events.
`tool_call` is the timed execution span.

AG-UI-specific IDs are represented through correlation attributes, not concrete
AG-UI runtime types.

## Lifecycle Events

Lifecycle events use these attributes:

- `retry.attempt`
- `retry.reason`
- `compaction.reason`
- `interrupt.reason`
- `resume.reason`
- `cancellation.reason`
- `error.classification`
- `error.retryable`

Lifecycle event payloads must not include raw prompt, tool, attachment, or
reasoning content.

## Error Fields

Domain operation errors and observation/export failures share error field names
but remain distinct by `obs.kind` and `error.operation`.

Error attributes:

- `error.operation`: `model_call`, `stream`, `tool_call`, `record`, `export`,
  `flush`, `shutdown`, `redact`, `batch`, `credential_validation`, or
  `error_handler`.
- `error.type`: stable type or sentinel name when safe.
- `error.message`: optional redacted message after failure-surface and redaction
  rules permit it.
- `error.classification`: stable classification from
  [failure-surface.md](failure-surface.md).
- `error.retryable`
- `error.canceled`
- `error.dropped`

Observation/export failures should also emit an `observation.error` event or
state record when possible.

## Redaction Metadata

Redaction records use the logical shape from [redaction.md](redaction.md):

- `field_path`
- `reason`
- `original_bytes`
- `retained_bytes`

Datadog payload mapping may encode records as nested metadata or attributes, but
recorders must preserve the logical fields for tests.

## Datadog Mapping

For `v0.1.0`, the real exporter maps normalized spans and events to the selected
Datadog LLM Observability trace spans HTTP payload from
[export-strategy.md](export-strategy.md).

Mapping rules:

- normalized `Span` becomes a Datadog LLM Observability span payload item;
- normalized `Event` becomes a span event or Datadog-supported event field
  attached to the closest mapped span;
- `genai.*`, `tool.*`, `correlation.*`, `error.*`, `metadata.*`, and redaction
  records map to Datadog span metadata unless the HTTP spans API has a more
  specific field;
- timestamps use normalized UTC time converted to the Datadog HTTP payload's
  expected unit;
- duration uses normalized `duration_ms` converted only at the transport
  boundary.

The exporter-config bead owns endpoint, API key, site, retry, and auth behavior.

## OpenTelemetry Notes

OpenTelemetry GenAI names are informational future mapping notes for `v0.1.0`.
No OTLP exporter is implemented in the first release.

Where practical, keep normalized names close to OTel GenAI concepts:

- `genai.provider`
- `genai.model`
- `genai.usage.*`
- `genai.latency.*`

If Datadog HTTP and OTel naming diverge, the normalized schema should preserve
the field needed by Datadog first and document the future OTel mapping here or
in a later schema update.

## Implementation Tests

Implementation beads must test:

- required and optional attributes for each span kind;
- session/run/model/stream/tool parent-child relationships;
- root behavior when session or run IDs are absent;
- streaming chunk, first-token, partial failure, cancellation, and token usage
  representation;
- tool registration, materialization, execution, settlement, failure, and
  cancellation representation;
- correlation omission for empty fields;
- redaction record preservation;
- observation/export error records remain distinct from domain operation errors;
- Datadog mapping uses the selected LLM Observability HTTP spans target and does
  not require OTLP.
