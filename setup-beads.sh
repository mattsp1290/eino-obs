#!/bin/bash
# Project: eino-obs Datadog AI/LLM observability API
# Generated: 2026-06-26

set -e

if [ ! -d ".beads" ]; then
    bd init
fi

echo "Creating eino-obs task graph..."

EPIC=$(bd create "Epic: Build eino-obs Datadog AI/LLM observability library" -t epic -p 0 --silent)
bd update "$EPIC" --status in_progress

# Phase 1: Repository setup
# Reservations: go.mod, go.sum, README.md, docs/**, examples/**, .github/**
SETUP_MODULE=$(bd create "Initialize Go module, Go version, and dependency policy" -p 0 --parent "$EPIC" --silent)
SETUP_LAYOUT=$(bd create "Create package layout for public API, internal model, exporters, recorder, examples, and tests" -p 0 --parent "$EPIC" --silent)
SETUP_TOOLING=$(bd create "Configure gofmt, go vet, go test, and race-test expectations" -p 1 --parent "$EPIC" --silent)
SETUP_CI=$(bd create "Add CI workflow for gofmt check, go vet, and go test ./..." -p 1 --parent "$EPIC" --silent)
SETUP_DOCS_CONTEXT=$(bd create "Create docs/prompts/docs context index for architecture, schema, redaction, recorder, exporter, and release notes" -p 1 --parent "$EPIC" --silent)

bd dep add "$SETUP_LAYOUT" "$SETUP_MODULE"
bd dep add "$SETUP_TOOLING" "$SETUP_MODULE"
bd dep add "$SETUP_CI" "$SETUP_TOOLING"
bd dep add "$SETUP_DOCS_CONTEXT" "$SETUP_LAYOUT"

# Phase 2: Architecture and contracts
# Reservations: docs/prompts/docs/**, README.md, public API design notes
ARCH_DECISION=$(bd create "Decide export strategy: Datadog HTTP API vs OTel GenAI OTLP vs thin dual-capable abstraction" -p 0 --parent "$EPIC" --silent)
API_CONTRACT=$(bd create "Define public API contract for sessions, runs, model calls, streams, tools, lifecycle events, context propagation, and helper lifecycle style" -p 0 --parent "$EPIC" --silent)
SCHEMA_CONTRACT=$(bd create "Define normalized span and event schema with hierarchy, attributes, timestamps, latency units, token usage, errors, and Datadog/OTel names" -p 0 --parent "$EPIC" --silent)
REDACTION_POLICY=$(bd create "Define redaction policy, omitted-field representation, summary capture options, size limits, and encrypted reasoning handling" -p 0 --parent "$EPIC" --silent)
FAKE_CONTRACT=$(bd create "Define fake recorder/exporter contract for ordering, concurrency, reset, snapshot, inspection, and error injection" -p 0 --parent "$EPIC" --silent)
FAILURE_SURFACE=$(bd create "Decide exporter failure surface for instrumentation, flush, shutdown, hooks, and recorder state" -p 0 --parent "$EPIC" --silent)
CONFIG_CONTRACT=$(bd create "Define real exporter configuration contract, environment variables, endpoint/site options, and credential validation behavior" -p 1 --parent "$EPIC" --silent)
RELEASE_PLAN=$(bd create "Define first-release tagging and eino-agent pinning response format" -p 2 --parent "$EPIC" --silent)

bd dep add "$ARCH_DECISION" "$SETUP_DOCS_CONTEXT"
bd dep add "$API_CONTRACT" "$SETUP_DOCS_CONTEXT"
bd dep add "$SCHEMA_CONTRACT" "$ARCH_DECISION"
bd dep add "$REDACTION_POLICY" "$API_CONTRACT"
bd dep add "$FAKE_CONTRACT" "$SCHEMA_CONTRACT"
bd dep add "$FAKE_CONTRACT" "$REDACTION_POLICY"
bd dep add "$FAILURE_SURFACE" "$ARCH_DECISION"
bd dep add "$FAILURE_SURFACE" "$API_CONTRACT"
bd dep add "$CONFIG_CONTRACT" "$ARCH_DECISION"
bd dep add "$RELEASE_PLAN" "$SETUP_DOCS_CONTEXT"

# Phase 3: Core implementation
# Reservations: *.go, internal/model/**, internal/redaction/**, internal/exporter/**
CORE_MODEL=$(bd create "Implement internal normalized span and event model" -p 0 --parent "$EPIC" --silent)
REDACTION_IMPL=$(bd create "Implement default redaction and caller-provided summary capture with truncation" -p 0 --parent "$EPIC" --silent)
EXPORTER_INTERFACE=$(bd create "Implement exporter and recorder interfaces plus flush and shutdown contracts" -p 0 --parent "$EPIC" --silent)
CORRELATION_CONTEXT=$(bd create "Implement correlation context types and context.Context propagation helpers" -p 0 --parent "$EPIC" --silent)
ERROR_CLASSIFICATION=$(bd create "Implement observation error fields and classification helpers" -p 1 --parent "$EPIC" --silent)

bd dep add "$CORE_MODEL" "$SCHEMA_CONTRACT"
bd dep add "$REDACTION_IMPL" "$REDACTION_POLICY"
bd dep add "$REDACTION_IMPL" "$CORE_MODEL"
bd dep add "$EXPORTER_INTERFACE" "$FAILURE_SURFACE"
bd dep add "$EXPORTER_INTERFACE" "$CORE_MODEL"
bd dep add "$CORRELATION_CONTEXT" "$API_CONTRACT"
bd dep add "$CORRELATION_CONTEXT" "$SCHEMA_CONTRACT"
bd dep add "$ERROR_CLASSIFICATION" "$SCHEMA_CONTRACT"
bd dep add "$ERROR_CLASSIFICATION" "$FAILURE_SURFACE"

# Phase 4: Public instrumentation helpers
# Reservations: root package public files, obs/** if created, tests touching exported API
SESSION_RUN_HELPERS=$(bd create "Implement session admission and run lifecycle instrumentation helpers" -p 0 --parent "$EPIC" --silent)
MODEL_CALL_HELPERS=$(bd create "Implement provider and model call span helpers with provider, model, token, latency, retry, and error fields" -p 0 --parent "$EPIC" --silent)
STREAMING_HELPERS=$(bd create "Implement streaming model turn instrumentation for start, chunk, first token, end, cancellation, partial failure, and usage" -p 0 --parent "$EPIC" --silent)
TOOL_HELPERS=$(bd create "Implement server-side and AG-UI client-proposed tool instrumentation for registration, materialization, execution, settlement, and errors" -p 0 --parent "$EPIC" --silent)
LIFECYCLE_EVENTS=$(bd create "Implement interrupt, resume, retry, compaction, cancellation, and generic error event helpers" -p 1 --parent "$EPIC" --silent)
PUBLIC_CONFIG=$(bd create "Implement public constructors, options, no-network defaults, and documented stable exported types" -p 0 --parent "$EPIC" --silent)

bd dep add "$SESSION_RUN_HELPERS" "$API_CONTRACT"
bd dep add "$SESSION_RUN_HELPERS" "$CORE_MODEL"
bd dep add "$SESSION_RUN_HELPERS" "$CORRELATION_CONTEXT"
bd dep add "$MODEL_CALL_HELPERS" "$API_CONTRACT"
bd dep add "$MODEL_CALL_HELPERS" "$REDACTION_IMPL"
bd dep add "$MODEL_CALL_HELPERS" "$ERROR_CLASSIFICATION"
bd dep add "$STREAMING_HELPERS" "$MODEL_CALL_HELPERS"
bd dep add "$STREAMING_HELPERS" "$REDACTION_IMPL"
bd dep add "$TOOL_HELPERS" "$API_CONTRACT"
bd dep add "$TOOL_HELPERS" "$REDACTION_IMPL"
bd dep add "$TOOL_HELPERS" "$ERROR_CLASSIFICATION"
bd dep add "$LIFECYCLE_EVENTS" "$API_CONTRACT"
bd dep add "$LIFECYCLE_EVENTS" "$ERROR_CLASSIFICATION"
bd dep add "$PUBLIC_CONFIG" "$EXPORTER_INTERFACE"
bd dep add "$PUBLIC_CONFIG" "$CONFIG_CONTRACT"

# Phase 5: Fake recorder and no-network mode
# Reservations: recorder/**, internal/recorder/**, exporter/fake/**, tests/recorder/**
FAKE_RECORDER=$(bd create "Implement fake no-network recorder/exporter with snapshot, reset, ordering, concurrency, and inspection APIs" -p 0 --parent "$EPIC" --silent)
FAKE_ERROR_INJECTION=$(bd create "Implement fake recorder error injection for export, flush, and shutdown paths" -p 1 --parent "$EPIC" --silent)
NO_NETWORK_MODE=$(bd create "Wire no-network mode into public constructors and test helpers" -p 0 --parent "$EPIC" --silent)

bd dep add "$FAKE_RECORDER" "$FAKE_CONTRACT"
bd dep add "$FAKE_RECORDER" "$EXPORTER_INTERFACE"
bd dep add "$FAKE_RECORDER" "$REDACTION_IMPL"
bd dep add "$FAKE_ERROR_INJECTION" "$FAKE_RECORDER"
bd dep add "$FAKE_ERROR_INJECTION" "$FAILURE_SURFACE"
bd dep add "$NO_NETWORK_MODE" "$FAKE_RECORDER"
bd dep add "$NO_NETWORK_MODE" "$PUBLIC_CONFIG"

# Phase 6: Real Datadog-compatible exporter
# Reservations: exporter/datadog/** or exporter/otel/**, internal/datadog/**, internal/otel/**, tests/exporter/**
REAL_EXPORTER_CORE=$(bd create "Implement the selected Datadog-compatible exporter path behind the exporter abstraction" -p 0 --parent "$EPIC" --silent)
REAL_EXPORTER_BATCHING=$(bd create "Implement batching, payload limits, timestamp handling, flush, shutdown, and backpressure behavior" -p 1 --parent "$EPIC" --silent)
REAL_EXPORTER_RETRY=$(bd create "Implement retry, backoff, auth error handling, endpoint/site configuration, and credential validation" -p 1 --parent "$EPIC" --silent)
REAL_EXPORTER_TEST_SERVER=$(bd create "Add fake HTTP or OTLP endpoint tests for real exporter payloads, failures, retries, and shutdown" -p 1 --parent "$EPIC" --silent)

bd dep add "$REAL_EXPORTER_CORE" "$ARCH_DECISION"
bd dep add "$REAL_EXPORTER_CORE" "$CONFIG_CONTRACT"
bd dep add "$REAL_EXPORTER_CORE" "$EXPORTER_INTERFACE"
bd dep add "$REAL_EXPORTER_CORE" "$REDACTION_IMPL"
bd dep add "$REAL_EXPORTER_BATCHING" "$REAL_EXPORTER_CORE"
bd dep add "$REAL_EXPORTER_RETRY" "$REAL_EXPORTER_CORE"
bd dep add "$REAL_EXPORTER_RETRY" "$FAILURE_SURFACE"
bd dep add "$REAL_EXPORTER_TEST_SERVER" "$REAL_EXPORTER_BATCHING"
bd dep add "$REAL_EXPORTER_TEST_SERVER" "$REAL_EXPORTER_RETRY"

# Phase 7: Tests
# Reservations: *_test.go, tests/**, examples/**
TEST_SHAPES=$(bd create "Add unit tests for normalized span/event shapes, hierarchy, correlation fields, timestamps, latency, and token usage" -p 0 --parent "$EPIC" --silent)
TEST_REDACTION=$(bd create "Add tests proving redaction defaults, omitted-field metadata, summary truncation, and encrypted reasoning non-emission" -p 0 --parent "$EPIC" --silent)
TEST_FAKE=$(bd create "Add tests for fake recorder ordering, concurrency, snapshot, reset, inspection, and error injection" -p 0 --parent "$EPIC" --silent)
TEST_HELPERS=$(bd create "Add public helper tests for sessions, runs, provider calls, streams, tools, retries, compaction, interrupts, cancellation, and errors" -p 0 --parent "$EPIC" --silent)
TEST_CONFIG_FAILURES=$(bd create "Add tests for configuration validation and exporter failure handling without live credentials" -p 1 --parent "$EPIC" --silent)
TEST_RACE=$(bd create "Run and stabilize go test -race ./... for concurrent recorder and exporter behavior" -p 1 --parent "$EPIC" --silent)

bd dep add "$TEST_SHAPES" "$SESSION_RUN_HELPERS"
bd dep add "$TEST_SHAPES" "$MODEL_CALL_HELPERS"
bd dep add "$TEST_REDACTION" "$REDACTION_IMPL"
bd dep add "$TEST_FAKE" "$FAKE_ERROR_INJECTION"
bd dep add "$TEST_HELPERS" "$STREAMING_HELPERS"
bd dep add "$TEST_HELPERS" "$TOOL_HELPERS"
bd dep add "$TEST_HELPERS" "$LIFECYCLE_EVENTS"
bd dep add "$TEST_HELPERS" "$NO_NETWORK_MODE"
bd dep add "$TEST_CONFIG_FAILURES" "$REAL_EXPORTER_TEST_SERVER"
bd dep add "$TEST_CONFIG_FAILURES" "$FAKE_ERROR_INJECTION"
bd dep add "$TEST_RACE" "$TEST_FAKE"
bd dep add "$TEST_RACE" "$REAL_EXPORTER_BATCHING"

# Phase 8: Documentation and release preparation
# Reservations: README.md, docs/**, examples/**, CHANGELOG.md
README_DOCS=$(bd create "Write README with API overview, safe defaults, Datadog export configuration, no-network mode, and supported platform assumptions" -p 1 --parent "$EPIC" --silent)
PACKAGE_DOCS=$(bd create "Add package documentation and Go examples for minimal Eino session, model call, streaming, and tool instrumentation" -p 1 --parent "$EPIC" --silent)
EXAMPLES=$(bd create "Create runnable examples for fake recorder mode and selected Datadog-compatible exporter configuration" -p 1 --parent "$EPIC" --silent)
CHANGELOG=$(bd create "Prepare changelog and v0.1.0 release notes" -p 2 --parent "$EPIC" --silent)
PIN_RESPONSE=$(bd create "Record exact tag or commit for eino-agent to pin in release response document" -p 2 --parent "$EPIC" --silent)
FINAL_VERIFY=$(bd create "Run final gofmt, go vet ./..., go test ./..., and go test -race ./... where practical" -p 0 --parent "$EPIC" --silent)

bd dep add "$README_DOCS" "$API_CONTRACT"
bd dep add "$README_DOCS" "$CONFIG_CONTRACT"
bd dep add "$README_DOCS" "$REDACTION_POLICY"
bd dep add "$PACKAGE_DOCS" "$SESSION_RUN_HELPERS"
bd dep add "$PACKAGE_DOCS" "$MODEL_CALL_HELPERS"
bd dep add "$PACKAGE_DOCS" "$STREAMING_HELPERS"
bd dep add "$PACKAGE_DOCS" "$TOOL_HELPERS"
bd dep add "$EXAMPLES" "$NO_NETWORK_MODE"
bd dep add "$EXAMPLES" "$REAL_EXPORTER_CORE"
bd dep add "$CHANGELOG" "$TEST_CONFIG_FAILURES"
bd dep add "$PIN_RESPONSE" "$CHANGELOG"
bd dep add "$PIN_RESPONSE" "$RELEASE_PLAN"
bd dep add "$FINAL_VERIFY" "$SETUP_CI"
bd dep add "$FINAL_VERIFY" "$TEST_SHAPES"
bd dep add "$FINAL_VERIFY" "$TEST_REDACTION"
bd dep add "$FINAL_VERIFY" "$TEST_FAKE"
bd dep add "$FINAL_VERIFY" "$TEST_HELPERS"
bd dep add "$FINAL_VERIFY" "$TEST_CONFIG_FAILURES"
bd dep add "$FINAL_VERIFY" "$TEST_RACE"
bd dep add "$FINAL_VERIFY" "$README_DOCS"
bd dep add "$FINAL_VERIFY" "$PACKAGE_DOCS"
bd dep add "$FINAL_VERIFY" "$EXAMPLES"
bd dep add "$FINAL_VERIFY" "$PIN_RESPONSE"

echo ""
echo "Bead graph created."
echo "Epic: $EPIC"
echo "View with:"
echo "  bd show $EPIC"
echo "  bd children $EPIC"
echo "  bd ready"
