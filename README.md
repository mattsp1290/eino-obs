# eino-obs

Datadog AI/LLM observability helpers for Eino-based Go agents.

## Module Status

This repository is initialized as the Go module
`github.com/mattsp1290/eino-obs`.

The supported Go version is Go 1.24 or newer in the Go 1.x release line. The
module is intentionally dependency-free at this stage: exporter SDKs,
OpenTelemetry packages, and Datadog transport dependencies should only be added
behind an explicitly chosen exporter implementation.

The initial platform target is the standard Go-supported Linux and macOS
development and test environments. Normal tests must not require live Datadog
credentials or network access.

Run the current validation gate with:

```bash
go test ./...
```
