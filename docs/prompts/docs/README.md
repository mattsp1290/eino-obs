# eino-obs Design Context

This directory holds durable design context for the follow-on beads. Each design
bead should update the matching file here so decisions are not hidden only in
issue text.

## Files

- [architecture.md](architecture.md): export strategy and package boundary
  decisions.
- [schema.md](schema.md): normalized span and event model, attribute naming, and
  hierarchy rules.
- [redaction.md](redaction.md): default privacy behavior, opt-in summaries, and
  sensitive-field handling.
- [fake-recorder.md](fake-recorder.md): no-network recorder/exporter contract for
  tests.
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
