# Normalized Span And Event Schema

This file is the frozen first-implementation schema contract after
`eino-obs-6on.59`. It defines the transport-neutral model shared by public
helpers, recorders, and exporters before mapping to the selected Datadog LLM
Observability trace spans HTTP API.

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

Each observation has normalized identity fields outside the attribute map:

- `obs_id`: required unique identifier for every span and event.
- `parent_obs_id`: optional parent observation identifier.
- `trace_id`: required trace identifier shared by related observations. A session
  trace uses the session span's `obs_id`; a run without session uses the run
  span's `obs_id`; root observations without session/run context use their own
  `obs_id`.

The default hierarchy is:

```text
session
  run
    model_call
    stream
    tool_call
```

Parent resolution is deterministic:

1. If the caller supplies a valid explicit `parent_obs_id`, use it.
2. Otherwise, if the recorder has an active span stack for the goroutine/context,
   use the most specific active span.
3. Otherwise, if `correlation.run_id` matches an active run span, parent to that
   run span.
4. Otherwise, if `correlation.session_id` matches an active session span, parent
   to that session span.
5. Otherwise, emit a root observation with no `parent_obs_id`.

`session` spans are normally roots. `run` spans parent to `session` when a
matching session span exists. `model_call`, `stream`, and `tool_call` spans use
the parent resolution order above. A `tool_call` that is triggered by a model or
stream must pass the model/stream `obs_id` as explicit parent when that
relationship is known; otherwise it falls back to the active span or run.
Lifecycle events attach to the most specific active span by rule 2, not directly
to the run unless no more specific span exists.

## Common Attribute Rules

Every span has these required fields:

- `obs.kind`: span kind.
- `obs.name`: stable operation name.
- `obs.status`: `ok`, `error`, or `canceled`.
- `timestamp`: UTC start time.
- `duration_ms`: required when the span has ended.

Every event has these required fields:

- `obs.kind`: event category.
- `obs.name`: event name.
- `timestamp`: UTC occurrence time.

Events may include `obs.status` only when the event represents a terminal or
state transition outcome.

Correlation fields are conditional required fields: when the source context has
a non-empty value, the recorder must preserve it; when the value is empty, the
field must be omitted. Supported correlation fields are:

- `correlation.session_id`
- `correlation.run_id`
- `correlation.agent_id`
- `correlation.assistant_message_id`
- `correlation.thread_id`
- `correlation.agui_run_id`
- `correlation.tool_call_id`

`correlation.tool_call_id` comes from public `Correlation.ToolCallID` or from a
tool helper `ToolCallID` field. Tool observations also set `tool.call_id`.
Public `Correlation.TraceID`, `Correlation.ObservationID`, and
`Correlation.ParentObservationID` carry the normalized identity fields
`trace_id`, `obs_id`, and `parent_obs_id` when callers need explicit parent
resolution.

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

## Span Attribute Matrix

Required means the field must be present for that span kind when a span is
recorded. Conditional means the field is required only when the condition
applies.

| Span kind | Required attributes | Conditional attributes | Optional attributes |
| --- | --- | --- | --- |
| `session` | `obs.kind`, `obs.name`, `obs.status`, `timestamp` | `duration_ms` when ended; non-empty `correlation.*` | `service.*`, `metadata.*`, `redaction.records`, `error.*` for terminal error/cancel |
| `run` | `obs.kind`, `obs.name`, `obs.status`, `timestamp` | `duration_ms` when ended; non-empty `correlation.*`; `parent_obs_id` when session parent exists | `service.*`, `metadata.*`, `redaction.records`, `error.*` for terminal error/cancel |
| `model_call` | `obs.kind`, `obs.name`, `obs.status`, `timestamp`, `genai.provider`, `genai.model` | `duration_ms` when ended; non-empty `correlation.*`; `parent_obs_id` when parent exists; token counts when known; `error.*` for terminal error/cancel | request/response summaries, `genai.latency.total_ms`, `genai.retry.attempt`, `metadata.*`, `redaction.records` |
| `stream` | `obs.kind`, `obs.name`, `obs.status`, `timestamp`, `genai.provider`, `genai.model` | `duration_ms` when ended; non-empty `correlation.*`; `parent_obs_id` when parent exists; token counts when known; `genai.latency.first_token_ms` once known; `error.*` for terminal error/cancel | request/response summaries, `genai.latency.total_ms`, `genai.retry.attempt`, `metadata.*`, `redaction.records` |
| `tool_call` | `obs.kind`, `obs.name`, `obs.status`, `timestamp`, `tool.name`, `tool.call_id`, `tool.kind`, `tool.status` | `duration_ms` and `tool.latency.ms` when ended; non-empty `correlation.*`; `parent_obs_id` when parent exists; `tool.output.summary` on success when enabled; `error.*` for terminal failure/cancel | `tool.input.summary`, `metadata.*`, `redaction.records` |
| `export_flush` | `obs.kind`, `obs.name`, `obs.status`, `timestamp` | `duration_ms` when ended; `error.*` for terminal error/cancel | `service.*`, `metadata.*` |
| `export_shutdown` | `obs.kind`, `obs.name`, `obs.status`, `timestamp` | `duration_ms` when ended; `error.*` for terminal error/cancel | `service.*`, `metadata.*` |

Terminal errored spans require `error.operation`, `error.classification`, and
`error.retryable`. They include `error.type` and redacted `error.message` when
safe and available. Terminal canceled spans require `error.operation`,
`error.classification`, `error.retryable=false`, and `error.canceled=true`.

## Event Attribute Matrix

| Event name | Required attributes | Conditional attributes | Optional attributes |
| --- | --- | --- | --- |
| `stream.chunk` | `obs.kind`, `obs.name`, `timestamp`, `stream.chunk.index` | `stream.chunk.summary` when summary capture is enabled; non-empty `correlation.*`; `parent_obs_id` when stream span exists | `redaction.records`, `metadata.*` |
| `stream.first_token` | `obs.kind`, `obs.name`, `timestamp`, `genai.latency.first_token_ms` | non-empty `correlation.*`; `parent_obs_id` when stream span exists | `metadata.*` |
| `tool.registered` | `obs.kind`, `obs.name`, `timestamp`, `tool.name`, `tool.kind`, `tool.status=registered` | `tool.call_id` when known; non-empty `correlation.*`; `parent_obs_id` when parent exists | `metadata.*`, `redaction.records` |
| `tool.materialized` | `obs.kind`, `obs.name`, `timestamp`, `tool.name`, `tool.call_id`, `tool.kind`, `tool.status=materialized` | non-empty `correlation.*`; `parent_obs_id` when parent exists | `tool.input.summary`, `metadata.*`, `redaction.records` |
| `tool.settled` | `obs.kind`, `obs.name`, `timestamp`, `tool.name`, `tool.call_id`, `tool.kind`, `tool.status` | `tool.latency.ms` when execution started; `tool.output.summary` on success when enabled; `error.*` on failure/cancel; non-empty `correlation.*`; `parent_obs_id` when parent exists | `metadata.*`, `redaction.records` |
| `retry` | `obs.kind`, `obs.name`, `timestamp`, `retry.attempt`, `retry.reason` | non-empty `correlation.*`; `parent_obs_id` when parent exists | `error.classification`, `error.retryable`, `metadata.*` |
| `compaction` | `obs.kind`, `obs.name`, `timestamp`, `compaction.reason` | non-empty `correlation.*`; `parent_obs_id` when parent exists | `metadata.*` |
| `interrupt` | `obs.kind`, `obs.name`, `timestamp`, `interrupt.reason` | non-empty `correlation.*`; `parent_obs_id` when parent exists | `metadata.*` |
| `resume` | `obs.kind`, `obs.name`, `timestamp`, `resume.reason` | non-empty `correlation.*`; `parent_obs_id` when parent exists | `metadata.*` |
| `cancellation` | `obs.kind`, `obs.name`, `timestamp`, `cancellation.reason`, `error.canceled=true` | non-empty `correlation.*`; `parent_obs_id` when parent exists; `error.operation` when tied to an operation | `metadata.*` |
| `error` | `obs.kind`, `obs.name`, `timestamp`, `error.operation`, `error.classification`, `error.retryable` | `error.type` and redacted `error.message` when safe and available; non-empty `correlation.*`; `parent_obs_id` when parent exists | `error.canceled`, `error.dropped`, `metadata.*`, `redaction.records` |
| `observation.error` | `obs.kind`, `obs.name`, `timestamp`, `error.operation`, `error.classification`, `error.retryable`, `error.dropped` | `error.type` and redacted `error.message` when safe and available; non-empty `correlation.*`; `parent_obs_id` when parent exists | `metadata.*`, `redaction.records` |
| `redaction.applied` | `obs.kind`, `obs.name`, `timestamp`, `redaction.records` | non-empty `correlation.*`; `parent_obs_id` when parent exists | `metadata.*` |

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

Chunk indexing starts at `0` for the first emitted chunk and increments by one
for each chunk in recorder order. Missing chunks are represented by absence, not
by skipped indexes unless the source explicitly reports a skipped chunk.

`stream.chunk.summary` is conditional: it is required when summary capture is
enabled and safe after redaction, and omitted otherwise. Raw chunk content is
never required and must not be recorded unless a later public API explicitly
adds an opt-in safe field.

`stream.first_token` is emitted once when first-token latency is known. The event
requires `genai.latency.first_token_ms`; the parent `stream` span also stores
the same value once the stream ends or is flushed. If first-token latency is
unknown, do not emit `stream.first_token` and omit the span attribute.

Partial failures use a terminal `stream` span status of `error` or `canceled`.
For `error`, the span requires the terminal error fields from the span matrix
and should include partial token usage when the provider supplies it. For
`canceled`, emit a `cancellation` event, set the stream span status to
`canceled`, set `error.canceled=true`, and preserve partial token usage when
available. Cancellation is not represented as an `error` event unless an
underlying operation error was also observed.

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

Tool lifecycle ordering is:

1. `tool.registered`: the tool became available. It does not require a
   `tool_call` span.
2. `tool.materialized`: a concrete proposed or server-side call exists and has
   `tool.call_id`.
3. `tool_call` span: required only when execution starts. Client-proposed tools
   that are never executed emit materialized and settled events without a
   `tool_call` span.
4. `tool.settled`: required when a materialized call reaches `succeeded`,
   `failed`, or `canceled`.

`tool.status` values map to lifecycle records as follows:

- `registered` appears only on `tool.registered`.
- `materialized` appears only on `tool.materialized`.
- `started` appears on the initial `tool_call` span while execution is active.
- `succeeded`, `failed`, and `canceled` appear on terminal `tool_call` spans and
  on `tool.settled` events.

`tool.latency.ms` belongs on the terminal `tool_call` span and is copied to
`tool.settled` when execution started. `tool.settled` with `failed` requires
`error.operation=tool_call`, `error.classification`, and `error.retryable`.
`tool.settled` with `canceled` requires `error.operation=tool_call`,
`error.classification`, `error.retryable=false`, and `error.canceled=true`.

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
state record when possible. `observation.error` is reserved for recorder,
redaction, batching, exporter, flush, shutdown, credential validation, or error
handler failures. Domain operation failures use the `error` event and terminal
span status instead.

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

Normalized `Span` records become Datadog LLM Observability HTTP span payload
items. The exporter maps required fields as follows:

| Normalized field | Datadog destination | Rule |
| --- | --- | --- |
| `trace_id` | `trace_id` | Use the normalized trace ID. |
| `obs_id` | `span_id` | Use the normalized observation ID. |
| `parent_obs_id` | `parent_id` | Use the parent span ID when present; root spans omit `parent_id` or encode Datadog's documented root value if required by the HTTP API client. |
| `timestamp` | `start_ns` | Convert UTC time to Unix nanoseconds. |
| `duration_ms` | `duration` | Convert milliseconds to nanoseconds. Active spans are not exported until duration is known. |
| `obs.name` | `name` | Stable operation name. |
| `obs.status` | `status` metadata | Preserve as metadata unless the Datadog payload exposes a first-class status field. |
| configured app name | `ml_app` | Use exporter config; fallback to `service.name`; final fallback `eino-obs`. |
| span kind | `meta.kind` | Map from the table below. |
| `genai.usage.input_tokens` | `metrics.input_tokens` | Omit when unknown; preserve explicit zero. |
| `genai.usage.output_tokens` | `metrics.output_tokens` | Omit when unknown; preserve explicit zero. |
| `genai.usage.total_tokens` | `metrics.total_tokens` | Omit when unknown; preserve explicit zero. |

Span kind mapping:

| Normalized span kind | Datadog `meta.kind` |
| --- | --- |
| `session` | `session` |
| `run` | `workflow` |
| `model_call` | `llm` |
| `stream` | `llm` |
| `tool_call` | `tool` |
| `export_flush` | `task` |
| `export_shutdown` | `task` |

Other normalized fields map to Datadog metadata with stable keys:

- `genai.provider` -> `metadata.genai.provider`
- `genai.model` -> `metadata.genai.model`
- request/response summaries -> `metadata.genai.request.summary` and
  `metadata.genai.response.summary`
- `genai.usage.reasoning_tokens` and `genai.usage.cached_input_tokens` ->
  same-key metadata until Datadog exposes first-class metrics for them
- `genai.latency.*` -> same-key metadata in milliseconds
- `tool.*` -> same-key metadata
- `correlation.*` -> same-key metadata
- `error.*` -> same-key metadata, with terminal status preserved
- `metadata.*` -> same-key Datadog metadata after redaction
- `redaction.records` -> `metadata.redaction.records`

Normalized `Event` records serialize on the closest mapped Datadog span. When
the Datadog HTTP payload supports span events, encode event `name`, `timestamp`,
and event attributes there. Otherwise encode an ordered metadata list under
`metadata.events`, preserving event `obs_id`, `parent_obs_id`, `obs.name`,
`timestamp` as Unix nanoseconds, and the event attribute map. Streaming chunk and
tool lifecycle events must not create standalone Datadog spans unless they are
represented by a normalized `tool_call` span.

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
- Datadog mapping uses the selected LLM Observability HTTP spans target,
  including `trace_id`, `span_id`, `parent_id`, `start_ns`, `duration`, `name`,
  `ml_app`, `meta.kind`, token metrics, and event serialization;
- no Datadog mapping path requires OTLP.
