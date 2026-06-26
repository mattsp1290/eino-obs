# Failure Surface Contract

This file is the contract for `eino-obs-6on.11`.

## Decision

Instrumentation helpers must be best-effort and non-panicking. They do not
return exporter errors from normal observation calls. Exporter and recorder
failures surface through:

- `Flush(ctx) error`;
- `Shutdown(ctx) error`;
- recorder/exporter state snapshots;
- optional error hooks decided here;
- normalized observation error records where the schema supports them.

This preserves ergonomic instrumentation call sites while still giving
applications explicit synchronization points for delivery failures.

## Helper Behavior

Start, end, event, chunk, and handle `Error` helpers must not panic when
exporting or recording fails. Helper methods should return no error unless a
later contract-freeze bead deliberately revises the public API.

On helper-time failure:

- the helper records an internal observation error if recorder state is
  available;
- the helper invokes the optional error hook if configured;
- the helper marks the observer/exporter state as dirty so `Flush` and
  `Shutdown` can report the failure;
- the helper drops the failed observation only when it cannot safely retain it
  for retry or inspection.

Application operation errors, such as `ModelCallError`, `StreamError`, and
`ToolCallError`, are domain observations. Exporter/recorder failures are
observation failures. Implementations must keep those two categories distinct.

## Error Types

The first implementation should expose a root-package observation error type:

```go
type ObservationError struct {
    Operation string
    Classification string
    Err error
    Retryable bool
    Dropped bool
}

type ErrorHandler func(context.Context, ObservationError)
```

`Operation` is a stable string such as `record`, `export`, `flush`, `shutdown`,
`redact`, `batch`, or `credential_validation`.

`Classification` is a stable string such as `recorder_failure`,
`exporter_failure`, `auth`, `rate_limit`, `timeout`, `canceled`,
`payload_too_large`, `invalid_config`, `redaction`, or `unknown`.

`Dropped` means the observation cannot be retried or inspected through recorder
state.

## Error Hooks

`Config` may include an optional `ErrorHandler`.

Hook rules:

- hooks are best-effort notification only;
- hooks must never be required for correctness;
- hooks run after the failure is recorded in state when state is available;
- hooks must not be called while holding recorder/exporter locks;
- hooks must receive a defensive copy of the `ObservationError`;
- hook panics are recovered, converted to an `ObservationError` with
  `Operation: "error_handler"` and `Classification: "panic"`, and recorded in
  state when possible;
- hook failures must not recursively invoke the same hook.

## Flush Behavior

`Flush(ctx)` is the caller-visible delivery synchronization point.

Flush must:

- attempt to deliver all buffered exportable observations known at call time;
- respect context cancellation and deadlines;
- return nil when all known observations are delivered or there is no real
  exporter configured;
- return an error when delivery, batching, credential validation, payload
  encoding, or context cancellation prevents known observations from being
  delivered;
- retain retryable observations when practical;
- mark permanently rejected observations as dropped in state;
- be safe to call more than once.

If multiple failures occur, `Flush` should return an aggregate error or an error
that preserves access to each underlying failure through `errors.Is` /
`errors.As` or an exported aggregate shape chosen by implementation.

## Shutdown Behavior

`Shutdown(ctx)` is terminal for a real exporter.

Shutdown must:

- call `Flush(ctx)` or perform equivalent draining;
- release exporter resources;
- prevent new observations from being accepted for export after shutdown starts;
- make subsequent helper calls no-op into recorder state or return no-op handles
  without panicking;
- be idempotent;
- return the flush/drain/release error if shutdown cannot complete cleanly.

After shutdown starts, helper-time observation failures should be classified as
`shutdown` or `exporter_closed` rather than `unknown`.

## Recorder State After Failures

The fake recorder and any stateful exporter test double must expose enough state
to assert failures without live credentials.

State snapshots should include:

- successfully recorded observations;
- pending buffered observations;
- dropped observation count;
- last observation error;
- all observation errors in occurrence order, when bounded storage permits;
- flush count and last flush error;
- shutdown count and last shutdown error.

State snapshots must be safe for concurrent readers and must not expose mutable
internal maps or slices.

## Fake Error Injection

Fake recorder/exporter implementations must support deterministic error
injection for:

- record failures;
- export failures;
- flush failures;
- shutdown failures;
- credential validation failures;
- hook panics or hook-recording failures.

Error injection should be scoped by operation and, where practical, by call
number so tests can assert retry and state behavior.

## Redaction Failures

Redaction policy violations under library control are observation failures, not
application operation failures.

Examples:

- invalid `MaxSummaryBytes` is an `invalid_config` failure and should be rejected
  during construction or validation before observations are recorded;
- unsupported encrypted reasoning paths detected by name/key identity are
  omitted and recorded through redaction metadata rather than returned from
  helpers;
- unrecoverable redaction implementation errors are classified as `redaction`
  and surfaced through state, hook, flush, or shutdown as appropriate.

## Testable Error Surfaces

Implementation beads must test:

- helper methods do not panic when recorder/exporter record fails;
- helper methods do not return exporter errors from normal observation calls;
- helper failures are visible in recorder/exporter state;
- optional error hooks are invoked after state is updated;
- hook panics are recovered and recorded without recursion;
- `Flush(ctx)` returns delivery, auth, payload, retry exhaustion, and context
  cancellation errors;
- `Flush(ctx)` is idempotent and retains retryable observations when practical;
- `Shutdown(ctx)` drains, releases resources, is idempotent, and reports flush or
  release errors;
- helper calls after shutdown do not panic;
- fake error injection can target record, export, flush, shutdown, credential
  validation, and hook paths;
- application operation errors remain distinct from observation/export errors.

## Related Contracts

- [public-api.md](public-api.md): defines handle-level application error call
  shapes.
- [fake-recorder.md](fake-recorder.md): defines no-network inspection and
  concurrency behavior.
- [exporter-config.md](exporter-config.md): defines credential validation,
  endpoint, auth, and configuration errors for the selected Datadog HTTP
  surface.
