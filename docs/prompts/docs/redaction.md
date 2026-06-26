# Redaction And Privacy Policy

Use this file for default privacy behavior and opt-in capture rules.

## Pending Content

- Defaults for prompt text, tool inputs, tool outputs, reasoning, encrypted
  reasoning values, and attachments.
- Representation of omitted or redacted fields in normalized output.
- Caller-provided summary capture options, including bounded size and
  truncation behavior.
- Rules proving encrypted reasoning values are never emitted, including through
  opt-in summaries.
- Test coverage expected for redaction defaults and summary capture.

## Implementation Notes

Redaction-sensitive implementation should depend on this policy before adding
normalization or exporter behavior.
