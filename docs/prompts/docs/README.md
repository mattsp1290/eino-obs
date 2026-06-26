# eino-obs Design Context

This directory holds durable design context for the follow-on beads. Each design
bead should update the matching file here so decisions are not hidden only in
issue text.

## Freeze Status

The core implementation contracts are frozen by `eino-obs-6on.59` for the first
implementation pass:

- [public-api.md](public-api.md)
- [schema.md](schema.md)
- [redaction.md](redaction.md)
- [fake-recorder.md](fake-recorder.md)
- [failure-surface.md](failure-surface.md)
- [export-strategy.md](export-strategy.md)
- [exporter-config.md](exporter-config.md)

Implementation beads should treat these files as the source of truth. If a
later implementation discovers a blocking ambiguity, file a Beads issue and
update the affected contract in the same change that resolves it.

## Files

- [architecture.md](architecture.md): export strategy and package boundary
  overview and cross-topic architecture notes.
- [export-strategy.md](export-strategy.md): first-release Datadog-compatible
  export path decision.
- [public-api.md](public-api.md): exported helper, type, context propagation,
  and lifecycle contract.
- [schema.md](schema.md): normalized span and event model, attribute naming, and
  hierarchy rules.
- [redaction.md](redaction.md): default privacy behavior, opt-in summaries, and
  sensitive-field handling.
- [fake-recorder.md](fake-recorder.md): no-network recorder/exporter contract for
  tests.
- [failure-surface.md](failure-surface.md): instrumentation, flush, shutdown,
  error-handler, and recorder-state failure behavior.
- [exporter-config.md](exporter-config.md): Datadog-compatible exporter
  configuration, batching, retries, credentials, and endpoint notes.
- [examples.md](examples.md): public examples and documentation plan for
  `eino-agent` integration.
- [release-pinning.md](release-pinning.md): first-release tag and consumer
  pinning response format.

## Update Rule

When a bead makes or changes a design decision, update the relevant file here in
the same commit as the implementation or design note. If a decision spans
multiple topics, link between files instead of duplicating the decision text.
