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
`payload_too_large`, `invalid_config`, `redaction`, `shutdown`,
`exporter_closed`, `panic`, or `unknown`.

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

## Retry And Dirty State

Retryable export failures must keep their observations pending unless context
cancellation, shutdown, or bounded capacity prevents retention. A retained
retryable observation stays visible in pending state until a later successful
flush delivers it or until it is dropped with an observation error that has
`Dropped: true`.

Non-retryable export failures must mark the observation dropped and remove it
from pending export state.

Dirty state is set by:

- any helper-time record, redact, batch, hook, or export failure;
- any failed `Flush`;
- any failed `Shutdown`;
- any dropped observation.

While dirty state is set, `Flush(ctx)` must return an error even when there are
no buffered exportable observations. A successful `Flush(ctx)` clears dirty
state only when all pending retryable observations are delivered and no
undropped historical failure remains unresolved. Dropped observations and
shutdown failures remain visible in snapshots even after later successful
flushes.

`Shutdown(ctx)` reports dirty state before returning nil. If shutdown drains all
pending observations but dirty state remains because of dropped observations or
historical failures, shutdown returns the aggregate error for those failures.

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
- retain retryable observations unless context cancellation, shutdown, or bounded
  capacity prevents retention;
- mark permanently rejected observations as dropped in state;
- be safe to call more than once.

If multiple failures occur, `Flush` must return an error compatible with
`errors.Join`. Implementations may wrap `errors.Join`, but callers and tests
must be able to recover at least one `ObservationError` with `errors.As` and use
`errors.Is` for wrapped sentinel causes where those sentinels exist.

## Shutdown Behavior

`Shutdown(ctx)` is terminal for a real exporter.

Shutdown must:

- call `Flush(ctx)` or perform equivalent draining;
- release exporter resources;
- prevent new observations from being accepted for export after shutdown starts;
- make subsequent helper calls return inert handles or no-op after recording at
  most one bounded local observation error with `Classification:
  "exporter_closed"` when recorder snapshots are available;
- never enqueue exportable observations after shutdown starts;
- not invoke user error hooks for post-shutdown helper no-ops;
- be idempotent;
- return the flush/drain/release error if shutdown cannot complete cleanly.

After shutdown starts, helper-time observation failures should be classified as
`shutdown` or `exporter_closed` rather than `unknown`.

After shutdown completes, `Flush(ctx)` must not attempt delivery. It returns the
last shutdown error if shutdown failed; otherwise it returns nil unless dirty
state contains unresolved dropped observations or historical failures.

## Recorder State After Failures

The fake recorder and any stateful exporter test double must expose enough state
to assert failures without live credentials.

State snapshots should include:

- successfully recorded observations;
- pending buffered observations;
- dropped observation count;
- last observation error;
- all observation errors in occurrence order, when bounded storage permits;
- whether dirty state is set;
- flush count and last flush error;
- shutdown count and last shutdown error.

State snapshots must be safe for concurrent readers and must not expose mutable
internal maps or slices.

If error history is bounded and overflows, snapshots must preserve the newest
errors, increment a dropped-error-history count, and retain the last observation
error.

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

Fake error injection must be operation-scoped and call-indexed for every listed
operation. Call indexes are one-based attempted operation calls:

- `record`: each attempted recorder record call;
- `export`: each attempted export batch send, not each observation;
- `flush`: each public `Flush` call;
- `shutdown`: each public `Shutdown` call;
- `credential_validation`: each validation attempt;
- `hook`: each attempted user hook invocation.

Injected failures increment the operation call index even when they fail.

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
- retryable export failures remain pending and are delivered by a later
  successful flush;
- non-retryable export failures are marked dropped and removed from pending
  export state;
- dirty state causes `Flush` and `Shutdown` to return errors until the contract's
  clearing conditions are met;
- aggregate errors are compatible with `errors.Join` and expose
  `ObservationError` through `errors.As`;
- post-shutdown helper calls do not enqueue exportable observations, do not
  invoke hooks, and record at most one bounded `exporter_closed` state error;
- operation-scoped call-indexed fake failures count attempted calls as defined
  above;
- application operation errors remain distinct from observation/export errors.

## Related Contracts

- [public-api.md](public-api.md): defines handle-level application error call
  shapes.
- [fake-recorder.md](fake-recorder.md): defines no-network inspection and
  concurrency behavior.
- [exporter-config.md](exporter-config.md): defines credential validation,
  endpoint, auth, and configuration errors for the selected Datadog HTTP
  surface.
