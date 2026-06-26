# Fake Recorder And No-Network Exporter

This file is the contract for `eino-obs-6on.10`. It defines how tests inspect
post-redaction normalized observations without live Datadog credentials or
network access.

## Package Ownership

The first implementation should split responsibilities this way:

- `recorder`: in-memory no-network observation recorder used by root-package
  observer defaults, integration tests, and local assertions.
- `exporter/fake`: no-network exporter implementation that satisfies the public
  `Exporter` interface from [public-api.md](public-api.md) and exposes export,
  flush, shutdown, pending, dropped, and injected-failure state.
- `internal/model`: owns normalized `Span`, `Event`, `RedactionRecord`, and
  attribute structures from [schema.md](schema.md). Public test helpers may
  expose defensive snapshots of those values but must not let callers mutate
  internal state.
- `internal/redaction`: runs before observations enter either fake package.

`recorder` and `exporter/fake` must not import Datadog SDK, OpenTelemetry SDK,
provider SDK, `eino-agent`, AG-UI runtime, or live network transport packages.

## Public Testing Surface

Names may be adjusted during implementation, but the first test API should offer
these capabilities:

```go
type Snapshot struct {
    Observations []ObservationSnapshot
    Pending []ObservationSnapshot
    Dropped []DroppedObservationSnapshot
    Errors []ObservationErrorSnapshot
    LastError *ObservationErrorSnapshot
    DroppedErrorHistory int
    Dirty bool
    RecordCount int64
    ExportCount int64
    FlushCount int64
    ShutdownCount int64
    CredentialValidationCount int64
    ErrorHandlerCount int64
    OperationCounts map[string]int64
    LastFlushError *ObservationErrorSnapshot
    LastShutdownError *ObservationErrorSnapshot
}
```

Snapshots expose normalized observations after redaction. They must include
enough data for tests to assert:

- observation identity: `obs_id`, `parent_obs_id`, and `trace_id`;
- span/event kind, name, status, timestamps, duration, and attributes;
- event ordering within a span and global observation order;
- correlation attributes with empty values omitted;
- redaction records with `field_path`, `reason`, `original_bytes`, and
  `retained_bytes`;
- observation/export failures from [failure-surface.md](failure-surface.md)
  separately from application operation errors.

Snapshot values are immutable from the caller's point of view. Implementations
must deep-copy slices, maps, redaction records, nested events, and
snapshot-safe error fields before returning them. Snapshots must not expose raw
Go `error` values.

## Ordering Guarantees

The fake recorder/exporter must preserve two deterministic orders:

1. `sequence`: a monotonic one-based integer assigned to every accepted
   observation in recorder acceptance order.
2. `span_event_sequence`: a monotonic one-based integer assigned to events
   attached to the same parent span in acceptance order.

For single-goroutine use, snapshot order must match call order. For concurrent
use, snapshot order must match the order in which the recorder accepted each
observation under its lock; callers must not rely on scheduler order before
acceptance.

Parent-child order is not required to be topological. If a child is accepted
before its parent because the caller supplied an explicit parent ID, snapshots
preserve acceptance order and retain the parent ID. Tests that need hierarchy
should sort or group by `trace_id`, `parent_obs_id`, and `obs_id`.

Streaming chunk indexes use the schema contract. The recorder must not
renumber `stream.chunk.index`; it only records the caller/handle-assigned index
and global/event sequence.

## Concurrency Guarantees

The fake recorder and fake exporter must be safe for concurrent calls to:

- record observations and events;
- inspect snapshots;
- reset state;
- flush;
- shutdown;

Internal state must be protected by synchronization. Snapshot and reset methods
must not race with active recording, flushing, or shutdown under `go test
-race`.

Failure-injection configuration is setup-only. It must be safe to configure
before first use, but implementations may reject or panic on configuration after
the first record, export, flush, shutdown, credential-validation, or error
handler attempt. This keeps operation-scoped call indexes deterministic.

Handles from the root observer remain single-logical-operation handles as
defined by [public-api.md](public-api.md), but fake packages must tolerate
concurrent terminal calls defensively:

- exactly one terminal state is recorded for a span handle;
- duplicate terminal calls are ignored after recording at most one bounded
  observation error with classification `recorder_failure` or
  `exporter_closed`, depending on state;
- duplicate terminal calls must not panic.

Hooks from [failure-surface.md](failure-surface.md) must not run while holding
fake recorder/exporter locks.

## Snapshot Semantics

`Snapshot` returns a point-in-time copy of all bounded state. It must not flush,
retry, clear dirty state, or mutate counters.

Recorder snapshots should include accepted observations, observation errors,
dirty state, and record counters. Exporter snapshots should include:

- exported observations, in batch send order;
- pending observations retained after retryable failures;
- dropped observations after non-retryable failures or bounded-capacity drops;
- operation counters for record, export, flush, shutdown,
  credential_validation, and error_handler attempts;
- last flush and shutdown errors;
- all retained observation errors in occurrence order.

If error history is bounded, implementations must preserve newest errors,
increment `DroppedErrorHistory`, and keep `LastError` accurate.

## Reset Semantics

`Reset` is a test-only state operation. It must:

- clear recorded, exported, pending, and dropped observations;
- clear retained observation errors, last errors, dirty state, and last
  flush/shutdown errors;
- reset operation counters to zero unless the implementation offers a separate
  `ResetData` that intentionally preserves counters;
- reset deterministic ID/sequence generators when those generators are owned by
  the fake;
- preserve static configuration such as service name, redaction options,
  capacity limits, and configured failure injection plan unless the caller uses
  an explicit `ResetFailures` or constructs a new fake;
- be synchronized with record, export, flush, shutdown, snapshot, and
  error-handler/error recording state updates.

After `Reset`, a new snapshot should be indistinguishable from a newly
constructed fake with the same configuration and failure plan.

`Reset` has a linearization point when it acquires the fake's state lock.
Operations whose state updates were accepted before that point are cleared.
Operations accepted after that point belong to the new epoch and receive
post-reset sequence numbers and counters. Operations already in progress when
`Reset` starts must publish any later state updates only after re-checking the
current epoch; if their epoch was reset, they must drop those stale updates
instead of reintroducing pre-reset observations, pending batches, errors, or
error-handler results. Tests may assert race safety across overlapping reset
calls, but must not assert semantic contents for operations intentionally racing
across the reset boundary beyond this epoch rule.

## Post-Redaction Inspection

Fake snapshots must expose only post-redaction normalized output. They must not
include raw prompt text, model output text, tool input/output payloads,
attachments, reasoning text, encrypted reasoning, provider bodies, credential
values, or unredacted error strings forbidden by [redaction.md](redaction.md).

Tests inspect summaries, metadata, errors, and redaction records through the
same normalized fields that real exporters receive. The fake recorder/exporter
must not provide a bypass API for pre-redaction values.

Redaction records must remain attached to the observation or event where the
redaction happened. A global snapshot may also aggregate redaction records for
convenience, but aggregation must not replace per-observation records.

`ObservationErrorSnapshot` must be redaction-safe. It should expose stable
fields such as operation, classification, retryable, dropped, canceled, sentinel
name, safe error type, and redacted message when policy allows it. It must not
hold a raw `error` interface value. `LastFlushError` and `LastShutdownError`
must reference this snapshot-safe shape, not `error.Error()` strings.

## Flush And Shutdown Behavior

The fake exporter must implement `Export`, `Flush`, and `Shutdown` with the
failure behavior from [failure-surface.md](failure-surface.md).

`Export(ctx, batch)`:

- defensively copies the batch before retaining it;
- increments the export call counter once per attempted batch;
- consumes one `export` failure-injection call index for each attempted batch,
  including attempts made from `Flush` or `Shutdown` drain logic;
- appends observations to exported state on success;
- appends retryable failed batches to pending state when practical;
- marks non-retryable failed observations as dropped;
- if `ctx` is canceled before acceptance, records no successful export and
  returns the context error as an observation error;
- if `ctx` is canceled after acceptance, retains the already accepted state and
  returns according to the completed operation state.

`Flush(ctx)`:

- increments the public flush counter once per call;
- retries pending observations known at call start;
- each retry send is an export batch attempt and increments `ExportCount`;
- returns nil only when pending observations are delivered and dirty state is
  clear;
- returns an aggregate error compatible with `errors.Join` when failures remain.

`Shutdown(ctx)`:

- increments the public shutdown counter once per call;
- drains through `Flush` or equivalent behavior. If it calls public `Flush`, it
  increments `FlushCount`; if it uses equivalent internal drain logic, it must
  still increment `ExportCount` for each attempted pending-batch send and must
  expose the drain error as `LastShutdownError`;
- prevents later exports from being accepted once shutdown starts;
- is idempotent;
- exposes final state through snapshots.

After shutdown starts, new `Export` calls must not accept or enqueue
observations. They return or record an `ObservationErrorSnapshot` with operation
`export`, classification `exporter_closed`, `Retryable: false`, and `Dropped:
true`. Post-shutdown helper no-ops from the root observer still follow
[failure-surface.md](failure-surface.md): they do not invoke user error
handlers and record at most one bounded local `exporter_closed` state error
when snapshots are available.

When there is no real exporter configured, the default no-network recorder still
supports `Flush` and `Shutdown`; they are synchronization points over local
state and must not require credentials.

## Failure Injection

Fake recorder/exporter implementations must support deterministic operation
failures by operation and one-based call index.

Required operations:

- `record`: each attempted recorder record call;
- `export`: each attempted export batch send, not each observation;
- `flush`: each public `Flush` call;
- `shutdown`: each public `Shutdown` call;
- `credential_validation`: each validation attempt;
- `error_handler`: each attempted user error-handler invocation.

An injection rule should be able to specify:

- operation name;
- call index or repeated range;
- error value or sentinel;
- retryable flag;
- dropped flag for failures that discard observations;
- classification string from [failure-surface.md](failure-surface.md);
- whether the operation should still retain pending observations.

Injected failures increment the operation counter even when the operation fails.
Call indexes are scoped per operation. `record` index 1 and `export` index 1
are independent.

Failure injection configuration must be deterministic and inspectable. Snapshot
state should expose operation counters so tests can assert exactly which call
failed. Injection operation names match `error.operation` values from
[schema.md](schema.md); recorded errors and snapshot counters must use
`error_handler`.
Injection plans are immutable after first use unless an implementation provides
an explicitly synchronized `ResetFailures` that also resets all operation
counters.

## Capacity And Dropping

Fakes may support bounded capacity to test dropped observations. If capacity is
configured:

- capacity decisions must be deterministic;
- dropped observations increment dropped counts and create observation errors
  with `Dropped: true`;
- snapshots must identify whether a drop came from capacity, non-retryable
  export failure, shutdown, or injected failure;
- retryable failures should retain pending observations unless capacity,
  context cancellation, or shutdown prevents retention.

Zero capacity is valid only when explicitly configured for a drop test. The
default capacity must be sufficient for ordinary unit tests and must not drop
observations silently.

## Implementation Test Requirements

Implementation beads must test:

- no credentials or network access are required;
- snapshots expose post-redaction normalized spans/events and no raw sensitive
  values;
- snapshot values are deep copies and cannot mutate fake state;
- single-goroutine ordering follows call order;
- concurrent recording is race-free and sequence order matches accepted order;
- snapshot and reset are race-free against concurrent inspection and recording;
- reset clears observations, pending, dropped, dirty state, errors, and counters
  according to this contract;
- parent IDs, trace IDs, event ordering, correlation omission, token usage,
  latency units, and redaction records remain inspectable;
- fake exporter `Export`, `Flush`, and `Shutdown` counters and state transitions
  follow [failure-surface.md](failure-surface.md);
- retryable export failures remain pending and can be delivered by a later
  successful flush;
- non-retryable export failures are dropped and visible in snapshots;
- operation-scoped call-indexed failure injection works for record, export,
  flush, shutdown, credential validation, and error-handler paths;
- injected failures count attempted calls even when they fail;
- `Flush` and `Shutdown` retry sends increment `ExportCount` and consume
  `export` failure-injection indexes;
- dirty state, last errors, error history overflow, and dropped counts are
  inspectable without live credentials.

## Non-Goals

The fake recorder/exporter contract does not require:

- a Datadog HTTP payload mirror;
- an OpenTelemetry exporter;
- a background worker;
- live endpoint validation;
- a compatibility promise for pre-redaction inspection;
- provider-specific request/response object capture.
