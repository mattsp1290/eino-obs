// Package einoobs provides Datadog AI/LLM observability helpers for Go agents.
//
// The package records agent sessions, runs, model calls, streaming model turns,
// tool calls, and lifecycle events through a transport-agnostic API. Helpers use
// standard library types plus package-local structs, so call sites do not need
// concrete provider SDK, Eino runtime, AG-UI runtime, Datadog transport, or
// OpenTelemetry types.
//
// New returns an Observer. When no exporter is configured, the observer uses a
// no-network in-process exporter, which makes examples and tests safe to run
// without credentials. Use Snapshot to inspect recorded observations in that
// mode, and configure exporter/datadog when observations should be sent to a
// Datadog intake endpoint.
package einoobs
