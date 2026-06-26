# Normalized Span And Event Schema

Use this file for the canonical normalized model that recorders and exporters
share before transport mapping.

## Pending Content

- Span kinds and event names.
- Required and optional attributes.
- Parent-child relationships.
- Timestamp source and latency units.
- Token usage fields.
- Error fields and classification.
- Correlation fields for session, run, agent, provider, model, tool call,
  assistant message, AG-UI thread, and AG-UI run identifiers.
- Datadog and OpenTelemetry attribute naming rules once the export strategy is
  chosen.

## Related Packages

- `internal/model`
- `internal/exporter`
- `recorder`
- `exporter/fake`
