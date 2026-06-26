# Architecture Decisions

Use this file for decisions about the first export strategy, public/internal
package boundaries, and whether the implementation uses the Datadog LLM
Observability HTTP API, OpenTelemetry GenAI/OTLP, or a thin abstraction that can
support both.

## Pending Beads

- `eino-obs-6on.6`: decide the export strategy.
- `eino-obs-6on.7`: define the public API contract and helper lifecycle style.

## Context To Preserve

- `eino-obs` is an observability library, not an agent runtime.
- Public APIs should use adapter-friendly Go primitives and structs rather than
  concrete `eino-agent`, provider, AG-UI, or Datadog transport types.
- Transport-specific implementation should stay behind package boundaries until
  the export strategy is chosen.
