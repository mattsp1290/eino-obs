# Redaction And Privacy Policy

This file is the contract for `eino-obs-6on.9`. Redaction-sensitive
implementation must follow this policy before adding normalization, recorder, or
exporter behavior.

## Default Policy

The default policy is safe by omission. Raw sensitive payload fields are not
captured, normalized, recorded, exported, logged, or stored.

Unsupported raw captures in the first public contract:

- prompt text;
- model input messages;
- model output text;
- tool inputs;
- tool outputs;
- attachments;
- reasoning text;
- encrypted reasoning values;
- provider request/response bodies;
- arbitrary binary payloads.

The library may record non-sensitive metadata such as IDs, provider names, model
names, token counts, durations, retry attempt numbers, status strings, and
caller-provided bounded summaries.

## Omitted And Redacted Representation

Sensitive raw fields are represented by absence plus redaction metadata, never
by placeholder strings that could be confused with model or tool data.

Normalized observations should use these conventions:

- omit the sensitive field value entirely;
- include redaction metadata in a dedicated structure or attribute namespace;
- record the redaction reason as a stable string such as `default_omitted`,
  `summary_disabled`, `summary_truncated`, `field_limit_exceeded`, or
  `encrypted_reasoning_forbidden`;
- record counts or booleans only when they do not reveal sensitive content.

Until [schema.md](schema.md) defines final field names, tests should assert this
minimal logical redaction metadata shape:

```go
type RedactionRecord struct {
    FieldPath string
    Reason string
    OriginalBytes int
    RetainedBytes int
}
```

`FieldPath` is a stable dotted logical path such as `summary.text`,
`summary.name`, `summary.fields.authorization`, or `metadata.api_key`.
`OriginalBytes` and `RetainedBytes` are set when a value is truncated or omitted
because of a byte limit; otherwise they may be zero.

Do not emit placeholder values such as `[REDACTED]`, `<omitted>`, empty strings
standing in for real content, or hashed raw payloads. Hashes of sensitive
payloads are treated as derived sensitive data and are not part of the first
contract.

## Caller-Provided Summaries

`Summary` values from [public-api.md](public-api.md) are the only opt-in content
capture mechanism in the first contract. Summaries are supplied by the consumer;
`eino-obs` does not generate summaries from raw payloads.

Summary capture rules:

- summaries are ignored unless the relevant `RedactionOptions` flag enables
  them;
- `CaptureInputSummary` controls input-side summaries;
- `CaptureOutputSummary` controls output-side summaries;
- `MaxSummaryBytes` limits each summary text value and each summary field value;
- when `MaxSummaryBytes` is zero, the implementation must use the default limit
  of 1024 bytes;
- negative `MaxSummaryBytes` values are invalid and must be rejected by
  construction or validation before observations are recorded;
- summary names and field keys are bounded to 128 bytes each;
- summary field maps are limited to 32 entries;
- over-limit summary names are omitted with reason `field_limit_exceeded`;
- over-limit field keys are omitted with reason `field_limit_exceeded`;
- when a summary field map has more than 32 entries, canonicalize keys, sort
  them lexicographically by canonical key, retain the first 32 entries, and omit
  the rest with reason `field_limit_exceeded`;
- truncation is by UTF-8 byte length and must not split a rune;
- truncated summaries include redaction metadata with original byte length,
  retained byte length, and reason `summary_truncated`;
- disabled summaries are omitted and include reason `summary_disabled`;
- summary maps are defensively copied before retention or export.

Summary names and fields must still pass sensitive-name filtering. Canonicalize
summary names, field keys, and metadata keys before matching by trimming leading
and trailing Unicode whitespace, lowercasing ASCII letters, and treating `-`,
`_`, `.`, and space as equivalent separators.

Canonical names that match `authorization`, `api_key`, `apikey`, `x_api_key`,
`access_token`, `refresh_token`, `bearer_token`, `token`, `password`, `secret`,
`client_secret`, `cookie`, `set_cookie`, `encrypted_reasoning`, or
`reasoning_encrypted` are omitted with redaction metadata.

If `Summary.Name` matches `encrypted_reasoning` or `reasoning_encrypted`, omit
the entire summary, including `Summary.Text` and all fields, with reason
`encrypted_reasoning_forbidden`. If `Summary.Name` matches another sensitive
name, omit the entire summary with reason `default_omitted`.

## Encrypted Reasoning

Encrypted reasoning values are never emitted. This rule is absolute and applies
to raw payloads, summaries, metadata, errors, logs, recorder snapshots, and
exporter payloads.

If a caller attempts to provide encrypted reasoning through `Summary`,
`Metadata`, an error string, or another string field known to the library, the
implementation must omit that field and record reason
`encrypted_reasoning_forbidden` when redaction metadata is available.

The library must not attempt to decrypt, hash, truncate, summarize, classify, or
count encrypted reasoning bytes.

Encrypted reasoning detection is based on typed field identity and canonical
name/key matches such as `encrypted_reasoning` and `reasoning_encrypted`. The
library does not classify arbitrary string contents as encrypted reasoning. If a
consumer places encrypted reasoning bytes in a generic string field with a safe
name, the consumer has violated the API contract; implementation tests should
cover all typed and name/key-identified encrypted reasoning paths under library
control.

## Metadata And Errors

`Metadata` maps are for stable, low-cardinality string attributes. They are not
payload fields.

Metadata rules:

- maps are defensively copied;
- keys are bounded to 128 bytes;
- values are bounded by `MaxSummaryBytes` or the 1024-byte default;
- maps are limited to 32 entries;
- metadata is captured by default after bounds and sensitive-key filtering;
- sensitive keys are omitted using the same key filter as summaries;
- over-limit metadata keys are omitted with reason `field_limit_exceeded`;
- when metadata has more than 32 entries, canonicalize keys, sort them
  lexicographically by canonical key, retain the first 32 entries, and omit the
  rest with reason `field_limit_exceeded`;
- values are truncated without splitting UTF-8 runes and marked with
  `summary_truncated` until the schema contract defines a metadata-specific
  reason.

Errors may expose `error.Error()` strings only after the failure-surface and
schema contracts decide the exact error fields. Until then, implementations
should prefer stable classification strings and avoid exporting raw error text
that could contain prompt, tool, provider response, credential, or attachment
content.

## Test Requirements

Implementation beads must include tests proving:

- raw prompt, model output, tool input, tool output, attachment, reasoning, and
  provider body fields are unsupported by default;
- encrypted reasoning is never present in normalized output, recorder snapshots,
  exporter payloads, logs, summaries, metadata, or errors under library control;
- disabled input and output summaries are omitted with `summary_disabled`;
- enabled summaries are retained up to the configured byte limit;
- negative `MaxSummaryBytes` values are rejected before observations are
  recorded;
- truncation preserves valid UTF-8 and records original and retained byte
  lengths;
- exactly-at-limit values are retained without truncation;
- tiny limits still preserve valid UTF-8;
- summary and metadata maps are defensively copied;
- sensitive summary and metadata keys are omitted;
- sensitive `Summary.Name` values omit the whole summary, including safe-looking
  fields under that summary;
- sensitive-name canonicalization handles case, whitespace, hyphen, underscore,
  dot, and space variants;
- oversized maps are retained deterministically by canonical-key lexical order;
- oversized names and keys are omitted with `field_limit_exceeded`;
- zero-value `RedactionOptions` use the safe default limit and do not enable
  summary capture;
- no-network fake recorder tests can inspect redaction metadata without live
  credentials.

## Open Dependencies

[schema.md](schema.md) must define the final redaction metadata shape and naming
rules. [fake-recorder.md](fake-recorder.md) must define how tests inspect
post-redaction observations. [failure-surface.md](failure-surface.md) must define
whether and how redaction errors are surfaced.
