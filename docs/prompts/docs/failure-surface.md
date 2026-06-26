# Failure Surface Contract

Use this file for `eino-obs-6on.11`.

## Pending Content

- Instrumentation helper behavior when exporters or recorders fail.
- Whether helper methods return errors, record internal observation errors,
  invoke hooks, or combine those mechanisms.
- Flush and shutdown error behavior.
- Hook behavior and ordering when observation/export failures occur.
- Recorder state after failures.
- How fake recorder/exporter error injection represents failures.
- Testable error surfaces for helper calls, flush, shutdown, hooks, and recorder
  state.

## Constraints

- Instrumentation helpers must not panic on exporter failure.
- Failure behavior must be consistent with the public API contract, fake recorder
  contract, and real exporter configuration.

## Related Files

- [public-api.md](public-api.md)
- [fake-recorder.md](fake-recorder.md)
- [exporter-config.md](exporter-config.md)
