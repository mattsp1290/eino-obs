# Architecture Decisions

Use this file for cross-topic architecture notes and links. Narrow decisions
belong in their reserved files so each follow-on bead has one obvious edit
target.

## Pending Beads

- `eino-obs-6on.6`: decide the export strategy in
  [export-strategy.md](export-strategy.md).
- `eino-obs-6on.7`: define the public API contract and helper lifecycle style
  in [public-api.md](public-api.md).
- `eino-obs-6on.11`: decide the failure surface in
  [failure-surface.md](failure-surface.md).

## Context To Preserve

- `eino-obs` is an observability library, not an agent runtime.
- Public APIs should use adapter-friendly Go primitives and structs rather than
  concrete `eino-agent`, provider, AG-UI, or Datadog transport types.
- Transport-specific implementation should stay behind package boundaries until
  the export strategy is chosen.
