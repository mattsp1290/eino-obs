# Exporter Configuration

Use this file for the first real Datadog-compatible exporter path once the
export strategy is chosen.

## Pending Content

- Constructor options and environment variables.
- API key, site, endpoint, or OTLP endpoint configuration.
- Credential validation and auth error behavior.
- Batching, retry/backoff, flush, and shutdown behavior.
- Payload limits and timestamp handling.
- Fake HTTP or OTLP endpoint test strategy.

## Constraint

Do not add live-credential requirements to normal `go test ./...`.
