# eino-obs

Datadog AI/LLM observability helpers for Eino-based Go agents.

`eino-obs` records sessions, runs, tool lifecycle events, retry/compaction
events, interrupts, and resumes through a small Go API. The root package is
safe-by-default: if you do not configure an exporter, observations stay in
process and can be inspected with a no-network snapshot.

## Install

```bash
go get github.com/mattsp1290/eino-obs
```

The module targets Go 1.24 or newer in the Go 1.x release line.

## Minimal Usage

```go
package main

import (
	"context"
	"fmt"

	einoobs "github.com/mattsp1290/eino-obs"
)

func main() {
	observer := einoobs.New(einoobs.Config{
		Service: "agent",
		Env:     "local",
	})

	ctx := einoobs.ContextWithCorrelation(context.Background(), einoobs.Correlation{
		TraceID:       "trace-1",
		ObservationID: "session-1",
		SessionID:     "session-1",
	})

	session := observer.StartSession(ctx, einoobs.SessionStart{Name: "chat"})
	run := session.StartRun(einoobs.RunStart{Name: "answer question"})
	run.End(einoobs.RunEnd{})
	session.End(einoobs.SessionEnd{})

	if err := observer.Flush(context.Background()); err != nil {
		panic(err)
	}

	fmt.Println(len(observer.Snapshot().Observations))
}
```

`examples/no_network` shows a fuller no-network run with retry and compaction
events:

```bash
go run ./examples/no_network
```

## API Overview

Create one `Observer` per service or agent process with `einoobs.New`.
Configuration can be supplied directly through `einoobs.Config` or with options
such as `WithService`, `WithEnv`, `WithVersion`, `WithRedaction`,
`WithExporter`, `WithErrorHandler`, and `WithNoNetwork`.

Use `StartSession` for the user or agent session and `StartRun` for individual
model or workflow runs. End them with `End`, or call `Error` to record a
terminal failure. Use `ContextWithCorrelation` and `Correlation` to carry trace,
session, run, parent observation, and tool-call identity across helpers.

The observer also records agent lifecycle events:

- `ToolRegistered` and `ToolMaterialized` for tool availability and tool-call
  materialization.
- `Retry` for retry attempts and retry classification.
- `Compaction` for context compaction, summary, and token changes.
- `Interrupt` and `Resume` for interrupted and resumed workflows.

Call `Flush(ctx)` before checking delivery results and `Shutdown(ctx)` during
process shutdown. Exporter errors are returned as `ObservationError` values with
operation, classification, retryability, and dropped-state fields. To observe
asynchronous recording failures, set `Config.ErrorHandler`.

## Safe Defaults

The default `New(Config{})` path uses the in-process no-network exporter. It
does not open sockets, require credentials, or send telemetry outside the
process.

Input and output summaries are not captured unless enabled:

```go
observer := einoobs.New(einoobs.Config{
	Redaction: einoobs.RedactionOptions{
		CaptureInputSummary:  true,
		CaptureOutputSummary: true,
		MaxSummaryBytes:      4096,
	},
})
```

When summaries are captured, redaction records identify omitted or truncated
fields. Full prompt, completion, tool input, and compaction summary payloads
should not be placed into metadata unless your application has already made them
safe to export.

## No-Network And Fake Recorders

No-network mode is the default and can also be forced explicitly:

```go
observer := einoobs.New(einoobs.Config{}, einoobs.WithNoNetwork())
```

Use `observer.Snapshot()` in tests or local tools to inspect recorded
observations and exporter lifecycle counters. Use `observer.Reset()` to clear
the no-network recorder between assertions.

For tests that need a recorder-style exporter with capacity controls, use the
`exporter/fake` package:

```go
fakeExporter := fake.New(fake.Config{
	Redaction: einoobs.RedactionOptions{CaptureInputSummary: true},
	Capacity:  100,
})
observer := einoobs.New(einoobs.Config{Exporter: fakeExporter})
```

The fake exporter implements `Export`, `Flush`, `Shutdown`, `Snapshot`, and
`Reset` without network access.

## Datadog Export

Use `exporter/datadog` when you are ready to send LLM observation spans to a
Datadog intake endpoint:

```go
import (
	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/exporter/datadog"
)

exporter, err := datadog.New(datadog.Config{
	APIKey: "redacted",
	Site:   "datadoghq.com",
	MLApp:  "agent-app",
	Service: "agent",
	Env:    "prod",
})
if err != nil {
	return err
}

observer := einoobs.New(einoobs.Config{Exporter: exporter})
```

Call `Flush` when you need pending observations delivered and `Shutdown` before
process exit. The Datadog exporter batches spans, applies payload limits,
gzip-compresses requests by default, retries retryable transport/status
failures, and keeps retryable failures pending for a later flush.

`examples/datadog_fake_endpoint` exercises the real Datadog exporter against a
local `httptest` endpoint:

```bash
go run ./examples/datadog_fake_endpoint
```

### Configuration

Datadog exporter fields can be set directly or through environment variables:

| Config field | Environment | Default |
| --- | --- | --- |
| `APIKey` | `DD_API_KEY` | required for real Datadog endpoints |
| `Site` | `DD_SITE` | `datadoghq.com` |
| `Endpoint` | `EINO_OBS_DATADOG_ENDPOINT` | derived from `Site` |
| `MLApp` | `DD_LLMOBS_ML_APP`, `EINO_OBS_ML_APP` | `Service`, `DD_SERVICE`, then `eino-obs` |
| `Service` | `DD_SERVICE` | `eino-obs` |
| `Env` | `DD_ENV` | empty |
| `Version` | `DD_VERSION` | empty |
| `Timeout` | `EINO_OBS_EXPORT_TIMEOUT` | `10s` |
| `BatchSize` | `EINO_OBS_EXPORT_BATCH_SIZE` | `100` |
| `MaxPayloadBytes` | `EINO_OBS_EXPORT_MAX_PAYLOAD_BYTES` | `1048576` |
| `MaxRetries` | `EINO_OBS_EXPORT_MAX_RETRIES` | `3` |
| `RetryBaseDelay` | `EINO_OBS_EXPORT_RETRY_BASE_DELAY` | `200ms` |
| `RetryMaxDelay` | `EINO_OBS_EXPORT_RETRY_MAX_DELAY` | `5s` |
| `ValidateCredentials` | `EINO_OBS_VALIDATE_CREDENTIALS` | `false` unless set in config |
| `DisableCompression` | `EINO_OBS_EXPORT_DISABLE_COMPRESSION` | `false` |

The supported Datadog sites are:

| Site | Endpoint host |
| --- | --- |
| `datadoghq.com` | `https://api.datadoghq.com` |
| `us3.datadoghq.com` | `https://api.us3.datadoghq.com` |
| `us5.datadoghq.com` | `https://api.us5.datadoghq.com` |
| `datadoghq.eu` | `https://api.datadoghq.eu` |
| `ap1.datadoghq.com` | `https://api.ap1.datadoghq.com` |
| `ap2.datadoghq.com` | `https://api.ap2.datadoghq.com` |
| `ddog-gov.com` | `https://api.ddog-gov.com` |

`Endpoint` overrides are intended for tests, proxies, and local fake endpoints.
Plain `http` endpoints are accepted only for localhost or loopback hosts.

## Platform And Validation

The supported development and test platforms are standard Go-supported Linux
and macOS environments. The normal test suite must not require live Datadog
credentials or external network access.

Run the current validation gates with:

```bash
go test ./...
go test -race ./...
```
