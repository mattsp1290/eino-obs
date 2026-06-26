# Exporter Configuration Contract

This file is the contract for `eino-obs-6on.12`. It defines configuration for
the first real Datadog-compatible exporter selected in
[export-strategy.md](export-strategy.md): the Datadog LLM Observability trace
spans HTTP API.

Primary docs checked on 2026-06-26:

- Datadog LLM Observability HTTP API:
  <https://docs.datadoghq.com/llm_observability/instrumentation/api/>
- Datadog sites:
  <https://docs.datadoghq.com/getting_started/site/>

The selected HTTP endpoint is `POST
/api/intake/llm-obs/v1/trace/spans`. Requests use JSON and the `DD-API-KEY`
header. Normal `go test ./...` must never require live Datadog credentials or
network access.

## Constructor Shape

The real exporter package should expose a constructor with typed options instead
of requiring callers to build raw URLs:

```go
type Config struct {
    APIKey string
    Site string
    Endpoint string
    MLApp string
    Service string
    Env string
    Version string
    HTTPClient *http.Client
    Timeout time.Duration
    BatchSize int
    MaxPayloadBytes int
    MaxRetries int
    RetryBaseDelay time.Duration
    RetryMaxDelay time.Duration
    ValidateCredentials bool
    DisableCompression bool
    AllowMissingAPIKeyForTesting bool
}
```

Names may be adjusted during implementation, but these configuration concepts
must be present. `Endpoint` is an override for tests, local proxies, or future
compatible intake services. Normal application call sites should set `APIKey`,
`Site`, and `MLApp`; they should not need to know the raw Datadog intake path.

The constructor must return an error for invalid static configuration before
the exporter accepts observations.

## Environment Variables

Constructor options have highest precedence. Empty constructor fields fall back
to environment variables, then defaults.

| Config field | Environment variable | Required | Default |
| --- | --- | --- | --- |
| `APIKey` | `DD_API_KEY` | Required for real Datadog export | none |
| `Site` | `DD_SITE` | Optional | `datadoghq.com` |
| `Endpoint` | `EINO_OBS_DATADOG_ENDPOINT` | Optional | derived from `Site` |
| `MLApp` | `DD_LLMOBS_ML_APP`, then `EINO_OBS_ML_APP` | Required logically | `Service`, then `eino-obs` |
| `Service` | `DD_SERVICE` | Optional | root observer service, then `eino-obs` |
| `Env` | `DD_ENV` | Optional | empty |
| `Version` | `DD_VERSION` | Optional | empty |
| `Timeout` | `EINO_OBS_EXPORT_TIMEOUT` | Optional | 10 seconds |
| `BatchSize` | `EINO_OBS_EXPORT_BATCH_SIZE` | Optional | 100 spans |
| `MaxPayloadBytes` | `EINO_OBS_EXPORT_MAX_PAYLOAD_BYTES` | Optional | conservative implementation default |
| `MaxRetries` | `EINO_OBS_EXPORT_MAX_RETRIES` | Optional | 3 |
| `RetryBaseDelay` | `EINO_OBS_EXPORT_RETRY_BASE_DELAY` | Optional | 200 milliseconds |
| `RetryMaxDelay` | `EINO_OBS_EXPORT_RETRY_MAX_DELAY` | Optional | 5 seconds |
| `ValidateCredentials` | `EINO_OBS_VALIDATE_CREDENTIALS` | Optional | false in tests, true only when requested |
| `DisableCompression` | `EINO_OBS_EXPORT_DISABLE_COMPRESSION` | Optional | false |
| `AllowMissingAPIKeyForTesting` | none | Optional | false |

Environment parsing rules:

- trim ASCII whitespace around string values;
- treat empty strings as unset;
- parse durations with Go `time.ParseDuration`;
- parse positive integers in base 10;
- parse booleans with `strconv.ParseBool`;
- reject negative durations, non-positive batch size, non-positive payload
  limit, and negative retry counts with `invalid_config`;
- never log or include `APIKey` in returned error strings, snapshots, or
  metadata.

## Site And Endpoint Resolution

Supported Datadog site parameters for `v0.1.0`:

| Site | API host |
| --- | --- |
| `datadoghq.com` | `https://api.datadoghq.com` |
| `us3.datadoghq.com` | `https://api.us3.datadoghq.com` |
| `us5.datadoghq.com` | `https://api.us5.datadoghq.com` |
| `datadoghq.eu` | `https://api.datadoghq.eu` |
| `ap1.datadoghq.com` | `https://api.ap1.datadoghq.com` |
| `ap2.datadoghq.com` | `https://api.ap2.datadoghq.com` |
| `ddog-gov.com` | `https://api.ddog-gov.com` |

The default endpoint is:

```text
https://api.<site>/api/intake/llm-obs/v1/trace/spans
```

with the host table above handling `datadoghq.com` and other exact site
parameters. Unknown `Site` values are invalid unless `Endpoint` is also set.

`Endpoint` override rules:

- must be absolute `http` or `https`;
- may include the full intake path;
- if it omits a path, append `/api/intake/llm-obs/v1/trace/spans`;
- `http` is allowed only for localhost, loopback, Unix test servers, or explicit
  test configuration;
- endpoint overrides are the only supported way to point tests at fake HTTP
  servers;
- no OTLP endpoint is accepted in `v0.1.0`.

## Request Contract

The real exporter sends one HTTP `POST` per batch to the resolved endpoint.

Required headers:

- `DD-API-KEY: <api key>`
- `Content-Type: application/json`
- `User-Agent: eino-obs/<version or unknown>`

Optional headers:

- `Content-Encoding: gzip` when compression is enabled;
- request correlation headers only if a later contract adds them.

The payload is the Datadog mapping from [schema.md](schema.md). The exporter
owns only transport-level batching, encoding, retries, and errors. It must not
add raw prompt/tool/provider payloads outside the normalized post-redaction
model.

## Credential Validation

Credential validation has two levels:

1. Static validation always runs in the constructor.
2. Optional live validation runs only when `ValidateCredentials` is true.

Static validation:

- `APIKey` is required unless the exporter is explicitly constructed for a fake
  endpoint/test mode that does not send to Datadog;
- `APIKey` must be non-empty after trimming;
- `Site` or `Endpoint` must resolve to a valid endpoint;
- `MLApp` must resolve to a non-empty value after fallback;
- invalid config returns an `ObservationError`-compatible error with operation
  `credential_validation`, classification `invalid_config`, retryable false,
  and dropped false.

`AllowMissingAPIKeyForTesting` is accepted only when `Endpoint` resolves to
localhost, loopback, or an in-process test server. It has no environment
variable. Real Datadog site-derived endpoints must reject missing `APIKey`
regardless of this flag.

Live validation:

- must use the same resolved endpoint, headers, timeout, and HTTP client family
  as export requests where practical;
- must not be part of normal `go test ./...`;
- must classify 401/403 as `auth`, retryable false;
- must classify unsupported site/product responses as `invalid_config` or
  `auth` based on response semantics;
- must classify network timeouts and 5xx as `timeout` or `exporter_failure`,
  retryable true;
- must not include the API key or response body in error strings unless the
  response body passes redaction policy.

If Datadog does not provide a lightweight validation endpoint for this intake,
live validation may send a syntactically valid minimal test request only when
the caller explicitly enables it. That request must be documented as potentially
visible in Datadog.

## Batching And Payload Limits

The exporter batches normalized ended spans. Active spans are not exported until
they have duration, as required by [schema.md](schema.md).

Batching rules:

- `BatchSize` limits the number of Datadog span payload items per request;
- `MaxPayloadBytes` limits the encoded request body before compression;
- if a single span exceeds `MaxPayloadBytes`, drop it with operation `batch`,
  classification `payload_too_large`, retryable false, dropped true;
- if a batch exceeds `MaxPayloadBytes`, split it deterministically while
  preserving observation order;
- batching must preserve trace/span hierarchy IDs;
- event serialization follows [schema.md](schema.md) and must remain attached
  to the closest exported span.

## Retry And Response Classification

Retry behavior follows [failure-surface.md](failure-surface.md).

Retryable responses:

- HTTP 408;
- HTTP 409 when Datadog documents it as retryable or conflict/transient;
- HTTP 429;
- HTTP 500 through 599;
- network timeouts and temporary network errors.

Non-retryable responses:

- HTTP 400 and other validation payload errors;
- HTTP 401/403 auth errors;
- HTTP 404 for unsupported endpoint/site unless an endpoint override test
  explicitly injects it as retryable;
- HTTP 413 payload too large after deterministic split attempts fail.

Retries use bounded exponential backoff with jitter. Retry attempts must respect
the caller's `context.Context`; no retry may outlive the flush/shutdown context.
When retries are exhausted, retryable observations remain pending when practical
and visible in fake/state snapshots. Non-retryable failures are dropped and
recorded with `Dropped: true`.

## Flush And Shutdown

`Flush(ctx)` must:

- deliver all observations buffered before the call when possible;
- use the configured endpoint, headers, timeout, retry, and batching behavior;
- return nil only when pending observations are delivered and dirty state is
  clear;
- return aggregate `ObservationError` values compatible with `errors.Join`.

`Shutdown(ctx)` must:

- call `Flush(ctx)` or equivalent drain logic;
- stop accepting new exportable observations after shutdown starts;
- close idle HTTP resources when owned by the exporter;
- be idempotent;
- preserve the last shutdown/drain error in state.

If the caller supplies a custom `HTTPClient`, the exporter must not close shared
resources it does not own unless the implementation documents an explicit owner
option.

## Timestamp And Unit Handling

Normalized timestamps and durations are already defined by [schema.md](schema.md):

- normalized `timestamp` is UTC `time.Time`;
- normalized `duration_ms` is integer milliseconds;
- Datadog `start_ns` is Unix nanoseconds;
- Datadog `duration` is nanoseconds.

The exporter must reject or drop spans whose timestamps violate Datadog's
documented intake window with classification `invalid_config` or
`payload_too_large` only if the response or local preflight makes that
deterministic. Otherwise let Datadog respond and classify the HTTP response.

## No-Network Tests

Implementation beads must test configuration without live credentials:

- constructor precedence: explicit config beats env vars, env vars beat
  defaults;
- `DD_API_KEY` is required for real Datadog endpoints and omitted from all error
  strings/snapshots;
- site-to-endpoint mapping for every supported site;
- endpoint override to `httptest.Server`;
- request method, path, headers, content type, and JSON body shape;
- `ml_app`, service/env/version tags, token metrics, timestamps, durations, and
  payload splitting;
- invalid env values return `invalid_config`;
- 401/403 classify as `auth`;
- 408/429/5xx and temporary network errors classify as retryable;
- 400/413 classify as non-retryable payload/config failures;
- retry exhaustion leaves retryable observations pending when practical;
- non-retryable failures drop observations and mark dirty state;
- `Flush` and `Shutdown` use configured retry, timeout, and context behavior;
- normal `go test ./...` uses fake HTTP servers or fake exporter only.

## Non-Goals

The `v0.1.0` exporter configuration contract does not include:

- OTLP endpoint, collector, resource, or OpenTelemetry SDK configuration;
- Datadog generic traces intake;
- logs intake;
- Datadog Agent-only trace submission;
- Datadog application keys;
- SDK-only LLM Observability configuration;
- live credential validation in normal tests;
- exposing raw endpoint details at instrumentation call sites.
