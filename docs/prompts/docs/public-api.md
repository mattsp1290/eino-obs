# Public API Contract

Use this file for `eino-obs-6on.7`.

## Pending Content

- Exported types for configuration, correlation context, token usage, streams,
  tools, redaction options, exporter interfaces, and recorder output.
- Session and run lifecycle helpers.
- Provider/model call helpers.
- Streaming turn helpers and lifecycle style.
- Tool call helpers.
- Interrupt, retry, compaction, and error event helpers.
- Context propagation rules.
- Whether spans are ended manually, closure/callback-based, or both.
- Failure handling call sites and how they relate to
  [failure-surface.md](failure-surface.md).

## Constraints

- Use adapter-friendly Go primitives and structs.
- Do not import concrete `eino-agent`, `eino-agui`, provider, or AG-UI runtime
  types unless a later adapter package is explicitly designed.
- Keep transport details out of normal consumer call sites.
