# Iteration 54 Public Helper Coverage Matrix Evidence

Selected bead: `eino-obs-6on.35`

The public helper coverage matrix is satisfied by tests merged in earlier focused
helper-test iterations plus the streaming/lifecycle workflow test from iteration
53.

Coverage evidence:

- Sessions and runs:
  - `session_test.go`: `TestSessionEndExportsCorrelationAndMetadata`,
    `TestSessionStartRunParentsRunToSession`,
    `TestSessionErrorExportsFailureAndCancellation`,
    `TestRunErrorExportsTerminalFailureOnce`,
    `TestRunCanceledWithIdentityValidatesNormalizedTerminalFields`.
- Model calls:
  - `model_call_test.go`: provider/model, usage, latency, retry, redaction,
    correlation, known zero usage, terminal error, and cancellation coverage.
- Streams:
  - `stream_test.go`: chunks, first-token latency, usage, total latency,
    redaction, partial failures, cancellation, and stream event hierarchy.
- Tools:
  - `tool_test.go`: registration, materialization, server execution,
    settlement, AG-UI proposed tool IDs, redacted summaries, latency, statuses,
    errors, and cancellation.
- Lifecycle events:
  - `lifecycle_test.go`: retry, compaction, interrupt, resume, cancellation,
    generic error, shared schema/redaction/correlation integration.
- Cross-helper public workflow:
  - `public_helper_stream_lifecycle_test.go`:
    `TestPublicStreamingAndLifecycleHelpersWorkflow` covers streaming success,
    partial failure, cancellation, lifecycle event metadata, redaction behavior,
    public identity/timing, and error envelopes through exported helpers.

Validation:

- `go test ./... -run 'Test(Session|Run|ModelCall|Stream|Tool|Retry|Compaction|Interrupt|Resume|Cancellation|Error|PublicStreamingAndLifecycle)'`: pass.
