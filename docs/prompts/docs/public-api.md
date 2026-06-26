# Public API Contract

This file is the frozen first-implementation public API contract after
`eino-obs-6on.59`. Implementation beads should treat the names and call shapes
here as the intended public surface unless a later Beads issue explicitly
changes this contract.

## Package Boundary

The root package is `github.com/mattsp1290/eino-obs` with package name
`einoobs`.

Stable public API belongs in the root package unless a later design explicitly
adds an adapter package. The public API must not import concrete `eino-agent`,
`eino-agui`, provider SDK, AG-UI runtime, Datadog transport, or OpenTelemetry
transport types.

Transport, normalized model, and redaction internals stay behind `internal/`.
No-network testing helpers live in `recorder` and `exporter/fake` after their
contracts are defined.

## Exported Types

The first public implementation should define these root-package types:

```go
type Config struct {
    Service string
    Env string
    Version string
    Redaction RedactionOptions
    Exporter Exporter
    ErrorHandler ErrorHandler
}

type Correlation struct {
    SessionID string
    RunID string
    AgentID string
    AssistantMessageID string
    ThreadID string
    AGUIRunID string
    ToolCallID string
}

type ProviderModel struct {
    Provider string
    Model string
}

type TokenUsage struct {
    InputTokens int64
    OutputTokens int64
    TotalTokens int64
    ReasoningTokens int64
    CachedInputTokens int64
}

type Summary struct {
    Name string
    Text string
    Fields map[string]string
}

type Metadata map[string]string

type RedactionOptions struct {
    CaptureInputSummary bool
    CaptureOutputSummary bool
    MaxSummaryBytes int
}

type RedactionRecord struct {
    FieldPath string
    Reason string
    OriginalBytes int
    RetainedBytes int
}

type ObservationError struct {
    Operation string
    Classification string
    Err error
    Retryable bool
    Dropped bool
}

type ErrorHandler func(context.Context, ObservationError)
```

Zero values must be usable. Empty IDs are omitted from normalized output rather
than replaced with placeholder strings. `Metadata` is the shared extension point
for start, end, and event structs that need stable string attributes without
provider-specific types.

`Summary` carries caller-provided bounded summaries only; raw prompt, tool
payload, attachment, and reasoning capture remains out of scope unless
[redaction.md](redaction.md) changes that contract. `Summary.Fields` and
`Metadata` maps are bounded by the redaction policy. Public helpers must
defensively copy map inputs before storing or exporting them.

## Exporter And Recorder Interfaces

The public package should expose a small exporter interface that avoids
transport-specific types:

```go
type Exporter interface {
    Export(ctx context.Context, batch []Observation) error
    Flush(ctx context.Context) error
    Shutdown(ctx context.Context) error
}

type Observation struct {
    ID string
    ParentID string
    TraceID string
    Kind string
    Name string
    Status string
    Timestamp time.Time
    Duration time.Duration
    DurationKnown bool
    Attributes map[string]any
    Events []Observation
    Redaction []RedactionRecord
    Error *ObservationError
}
```

`Observation` is the public compatibility boundary for normalized observations.
It is a snapshot-style value: exporters and fake recorders receive defensive
copies of maps, events, redaction records, and error values. Internal model
types may use richer representations, but conversion to `Observation` must
preserve the schema's identity, hierarchy, timing, redaction, and error fields.
`DurationKnown` distinguishes active or instantaneous observations from ended
spans with a zero duration.

`Config.ErrorHandler` is optional. It receives observation/export failures from
[failure-surface.md](failure-surface.md), not domain operation errors such as
model/tool failures recorded through handle `Error` methods. Helpers must not
require an error handler for correctness. `ObservationError.Err` may wrap the
raw in-process error for handler logic; snapshots and export payloads must use
the redacted `error.type` and `error.message` rules from [schema.md](schema.md)
and [redaction.md](redaction.md).

## Construction

Consumers should be able to construct an observer without network credentials:

```go
obs := einoobs.New(einoobs.Config{
    Service: "eino-agent",
    Env: "dev",
})
defer obs.Shutdown(ctx)
```

`New` must choose safe no-network defaults when `Config.Exporter` is nil. Real
Datadog-compatible exporters are configured through the constructor and
environment behavior in [exporter-config.md](exporter-config.md).

## Context Propagation

Correlation is propagated through `context.Context` with root-package helpers:

```go
ctx = einoobs.ContextWithCorrelation(ctx, einoobs.Correlation{
    SessionID: "session-123",
    RunID: "run-456",
    AgentID: "agent-main",
})

corr, ok := einoobs.CorrelationFromContext(ctx)
```

Helpers that start observations accept `context.Context`; they merge explicit
options with context correlation. Merge semantics are field-by-field:

- non-empty explicit `Correlation` fields override context fields for that
  observation only;
- empty explicit fields inherit the context value;
- there is no public "clear this inherited ID" operation in the first contract.

This keeps zero-value option structs ergonomic while preserving propagated IDs.
`Correlation.ToolCallID` is available for context propagation when a tool call
creates child observations. Tool helper `ToolCallID` fields also populate
`correlation.tool_call_id` and `tool.call_id` on tool observations.

## Lifecycle Style

Long-lived operations use start/end handles. Instantaneous lifecycle points use
event methods.

Start methods return a handle with `End`, `Error`, and event methods relevant to
that operation. `End` records successful or non-error terminal state. `Error`
records failed terminal state. Callers must invoke exactly one terminal method
per handle: either `End` or `Error`, not both.

All active-handle error structs include `Err error`,
`Classification string`, and `Canceled bool`. Error structs include
`Retryable bool` when retry is meaningful for that operation. Cancellation is a
terminal error state for active handles and can also be recorded as a lifecycle
event when the consumer needs a run-level cancellation marker.

Handles are intended for a single logical operation. Duplicate or concurrent
terminal calls are tolerated defensively as defined by
[fake-recorder.md](fake-recorder.md): exactly one terminal state is recorded and
duplicates must not panic.

Instrumentation helpers must not panic on exporter failure. Observation failures
surface through flush, shutdown, optional `ErrorHandler`, normalized
`observation.error` records, or recorder/exporter state as defined by
[failure-surface.md](failure-surface.md).

## Session And Run Helpers

```go
session := obs.StartSession(ctx, einoobs.SessionStart{
    Correlation: corr,
})

run := obs.StartRun(ctx, einoobs.RunStart{
    Correlation: corr,
    Name: "answer-user-message",
    Metadata: einoobs.Metadata{"workflow": "chat"},
})

if err != nil {
    run.Error(einoobs.RunError{Err: err, Classification: "runtime"})
} else {
    run.End(einoobs.RunEnd{})
}

session.End(einoobs.SessionEnd{})
```

`SessionStart`, `SessionEnd`, `RunStart`, and `RunEnd` should use primitives:
strings, booleans, numeric counters, `time.Time`, `time.Duration`, maps of
string metadata, and the shared `Correlation` type.

Run/session failures use handle-level terminal errors:

```go
session.Error(einoobs.SessionError{
    Err: err,
    Classification: "canceled",
    Canceled: true,
})

run.Error(einoobs.RunError{
    Err: err,
    Classification: "canceled",
    Canceled: true,
})
```

## Provider And Model Call Helpers

```go
call := obs.StartModelCall(ctx, einoobs.ModelCallStart{
    Correlation: corr,
    ProviderModel: einoobs.ProviderModel{
        Provider: "openai",
        Model: "gpt-example",
    },
    InputSummary: einoobs.Summary{Name: "user_request", Text: "short caller summary"},
})

call.End(einoobs.ModelCallEnd{
    Usage: einoobs.TokenUsage{InputTokens: 100, OutputTokens: 40, TotalTokens: 140},
    OutputSummary: einoobs.Summary{Name: "assistant_response", Text: "short caller summary"},
})
```

Model helpers should support provider, model, retry attempt, token usage,
latency, cancellation, and error fields without depending on provider SDK
types.

Model call failures use `Error` instead of an `End` value with error fields:

```go
call.Error(einoobs.ModelCallError{
    Err: err,
    Classification: "rate_limit",
    Retryable: true,
})

call.Error(einoobs.ModelCallError{
    Err: context.Canceled,
    Classification: "canceled",
    Canceled: true,
})
```

## Streaming Helpers

Streaming model turns use a start handle with chunk and terminal methods:

```go
stream := obs.StartStream(ctx, einoobs.StreamStart{
    Correlation: corr,
    ProviderModel: einoobs.ProviderModel{Provider: "openai", Model: "gpt-example"},
})

stream.Chunk(einoobs.StreamChunk{
    Index: 0,
    OutputSummary: einoobs.Summary{Name: "delta", Text: "caller summary"},
})

stream.End(einoobs.StreamEnd{
    Usage: einoobs.TokenUsage{InputTokens: 100, OutputTokens: 80, TotalTokens: 180},
})
```

The stream contract must allow first-token latency, total latency, partial
failure, cancellation, and final token usage to be represented according to
[schema.md](schema.md).

Stream failures and cancellations are terminal handle errors:

```go
stream.Error(einoobs.StreamError{
    Err: err,
    Classification: "canceled",
    Canceled: true,
    PartialUsage: einoobs.TokenUsage{InputTokens: 100, OutputTokens: 12},
})
```

## Tool Helpers

Tool observations are split by lifecycle point and use adapter-friendly IDs:

```go
obs.ToolRegistered(ctx, einoobs.ToolRegistered{
    Correlation: corr,
    ToolName: "search",
})

tool := obs.StartToolCall(ctx, einoobs.ToolCallStart{
    Correlation: corr,
    ToolCallID: "tool-call-1",
    ToolName: "search",
    InputSummary: einoobs.Summary{Name: "query", Fields: map[string]string{"kind": "web"}},
})

tool.End(einoobs.ToolCallEnd{
    OutputSummary: einoobs.Summary{Name: "result", Text: "caller summary"},
})
```

The same primitive contract must support server-side tools and AG-UI
client-proposed tools. AG-UI-specific correlation is represented through
`Correlation.ThreadID`, `Correlation.AGUIRunID`, `Correlation.ToolCallID`,
tool helper `ToolCallID`, and ordinary metadata fields, not AG-UI runtime types.

Tool materialization, settlement, and failure call shapes:

```go
obs.ToolMaterialized(ctx, einoobs.ToolMaterialized{
    Correlation: corr,
    ToolCallID: "tool-call-1",
    ToolName: "search",
    Metadata: einoobs.Metadata{"source": "client_proposed"},
})

tool.Error(einoobs.ToolCallError{
    Err: err,
    Classification: "tool_timeout",
    Retryable: false,
})

tool.Error(einoobs.ToolCallError{
    Err: context.Canceled,
    Classification: "canceled",
    Canceled: true,
})

obs.ToolSettled(ctx, einoobs.ToolSettled{
    Correlation: corr,
    ToolCallID: "tool-call-1",
    Status: "failed",
})
```

Expected `ToolSettled.Status` values are `succeeded`, `failed`, and `canceled`
as defined by [schema.md](schema.md).

## Lifecycle Event Helpers

Instant events use methods on `Observer` or an active run/session handle:

```go
obs.Retry(ctx, einoobs.RetryEvent{
    Correlation: corr,
    Attempt: 2,
    Reason: "rate_limit",
})

obs.Compaction(ctx, einoobs.CompactionEvent{Correlation: corr})
obs.Interrupt(ctx, einoobs.InterruptEvent{Correlation: corr, Reason: "user"})
obs.Resume(ctx, einoobs.ResumeEvent{Correlation: corr})
obs.Cancellation(ctx, einoobs.CancellationEvent{Correlation: corr})
obs.Error(ctx, einoobs.ErrorEvent{Correlation: corr, Err: err})
```

Event structs must avoid raw payloads and provider-specific error types. Error
classification should use stable strings plus wrapped `error` values where the
failure-surface contract permits them.

`CancellationEvent` and `ErrorEvent` are lifecycle markers, not substitutes for
active operation terminal methods. If a model call, stream, tool call, run, or
session is active, the active handle must receive `Error` exactly once for the
canceled or failed operation. A `CancellationEvent` may be emitted as additional
context around the same cancellation.

## Acceptance Checks For Implementation Beads

- Public helper inputs use primitives, standard library types, and local
  `einoobs` structs only.
- No public package imports concrete `eino-agent`, `eino-agui`, provider,
  AG-UI, Datadog transport, or OpenTelemetry transport types.
- Examples show intended session, model, stream, tool, lifecycle, and context
  propagation call shapes.
- Raw sensitive payload capture is not part of the public call shapes.
- Failure behavior remains compatible with [failure-surface.md](failure-surface.md).
- Public helpers copy `Summary.Fields` and `Metadata` maps before retaining
  them.
