# Fake Recorder And No-Network Exporter

Use this file for the test recorder/exporter contract.

## Pending Content

- Ordering guarantees.
- Concurrency behavior.
- Reset and snapshot APIs.
- Error injection hooks.
- How tests inspect post-redaction normalized spans and events.
- Which package owns each concern between `recorder` and `exporter/fake`.

## Constraints

The fake recorder/exporter must not require credentials or network access and
must remain useful for `eino-agent` tests.
