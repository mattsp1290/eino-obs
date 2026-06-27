package einoobs

import (
	"context"
	"testing"
	"time"
)

func TestToolRegisteredExportsEventWithCorrelation(t *testing.T) {
	observer := New(Config{Service: "svc"})
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		ToolCallID:          "ctx-tool-call",
		TraceID:             "trace-1",
		ParentObservationID: "run-obs",
	})
	at := time.Date(2026, 6, 26, 12, 0, 0, 0, time.FixedZone("offset", -4*60*60))

	observer.ToolRegistered(ctx, ToolRegistered{
		Correlation: Correlation{ObservationID: "tool-event"},
		ToolName:    "search",
		ToolCallID:  "registered-call",
		ToolKind:    "server",
		Time:        at,
		Metadata:    Metadata{"source": "registry"},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.ID != "tool-event" || got.ParentID != "run-obs" || got.TraceID != "trace-1" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Kind != "tool.registered" || got.Name != "tool.registered" || got.Status != "ok" {
		t.Fatalf("shape = kind:%q name:%q status:%q", got.Kind, got.Name, got.Status)
	}
	if !got.Timestamp.Equal(at.UTC()) {
		t.Fatalf("timestamp = %s, want %s", got.Timestamp, at.UTC())
	}
	if got.Attributes["tool.name"] != "search" ||
		got.Attributes["tool.call_id"] != "registered-call" ||
		got.Attributes["tool.kind"] != "server" ||
		got.Attributes["tool.status"] != "registered" {
		t.Fatalf("tool attributes = %#v", got.Attributes)
	}
	if got.Attributes["correlation.session_id"] != "session-1" ||
		got.Attributes["correlation.run_id"] != "run-1" ||
		got.Attributes["correlation.tool_call_id"] != "registered-call" ||
		got.Attributes["metadata.source"] != "registry" {
		t.Fatalf("correlation/metadata attributes = %#v", got.Attributes)
	}
}

func TestToolMaterializedUsesExplicitToolCallAndRedactsInputSummary(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 4},
	})
	metadata := Metadata{"source": "web"}

	observer.ToolMaterialized(context.Background(), ToolMaterialized{
		Correlation:  Correlation{ObservationID: "tool-event", TraceID: "trace-1", ParentObservationID: "run-obs"},
		ToolCallID:   "tool-call-1",
		ToolName:     "search",
		ToolKind:     "client_proposed",
		InputSummary: Summary{Name: "query", Text: "hello world", Fields: map[string]string{"kind": "web"}},
		Metadata:     metadata,
	})
	metadata["source"] = "mutated"

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "tool.materialized" || got.Name != "tool.materialized" {
		t.Fatalf("shape = kind:%q name:%q", got.Kind, got.Name)
	}
	if got.Attributes["tool.call_id"] != "tool-call-1" ||
		got.Attributes["correlation.tool_call_id"] != "tool-call-1" ||
		got.Attributes["tool.kind"] != "client_proposed" ||
		got.Attributes["tool.status"] != "materialized" {
		t.Fatalf("tool attributes = %#v", got.Attributes)
	}
	if got.Attributes["metadata.source"] != "web" {
		t.Fatalf("metadata.source = %q, want web", got.Attributes["metadata.source"])
	}
	if got.Attributes["tool.input.summary"] != "hell" {
		t.Fatalf("tool.input.summary = %q, want hell", got.Attributes["tool.input.summary"])
	}
	if got.Attributes["tool.input.summary.name"] != "query" {
		t.Fatalf("tool.input.summary.name = %q, want query", got.Attributes["tool.input.summary.name"])
	}
	if got.Attributes["tool.input.summary.fields.kind"] != "web" {
		t.Fatalf("tool.input.summary.fields.kind = %q, want web", got.Attributes["tool.input.summary.fields.kind"])
	}
	assertPublicRedactionRecord(t, got.Redaction, "tool.input.summary.text", "summary_truncated")
}

func TestToolMaterializedDisabledSummaryRecordsRedaction(t *testing.T) {
	observer := New(Config{})

	observer.ToolMaterialized(context.Background(), ToolMaterialized{
		ToolCallID:   "tool-call-1",
		ToolName:     "search",
		InputSummary: Summary{Name: "query", Text: "hello"},
	})

	got := observer.Snapshot().Observations[0]
	if _, ok := got.Attributes["tool.input.summary"]; ok {
		t.Fatalf("tool.input.summary was retained while capture disabled")
	}
	assertPublicRedactionRecord(t, got.Redaction, "tool.input.summary", "summary_disabled")
}

func TestToolMaterializedFieldsOnlySummaryAndSensitiveFields(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true},
	})

	observer.ToolMaterialized(context.Background(), ToolMaterialized{
		ToolCallID: "tool-call-1",
		ToolName:   "search",
		InputSummary: Summary{
			Name: "query",
			Fields: map[string]string{
				"kind":         "web",
				"access_token": "secret",
			},
		},
	})

	got := observer.Snapshot().Observations[0]
	if got.Attributes["tool.input.summary.name"] != "query" {
		t.Fatalf("summary name = %q, want query", got.Attributes["tool.input.summary.name"])
	}
	if got.Attributes["tool.input.summary.fields.kind"] != "web" {
		t.Fatalf("summary field = %q, want web", got.Attributes["tool.input.summary.fields.kind"])
	}
	if _, ok := got.Attributes["tool.input.summary.fields.access_token"]; ok {
		t.Fatalf("sensitive summary field was retained")
	}
	assertPublicRedactionRecord(t, got.Redaction, "tool.input.summary.fields.access_token", "default_omitted")
}

func TestToolMaterializedRequiresToolCallID(t *testing.T) {
	var handled []ObservationError
	observer := New(Config{
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})

	observer.ToolMaterialized(context.Background(), ToolMaterialized{ToolName: "search"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
		t.Fatalf("invalid materialized snapshot = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
	if len(handled) != 1 || handled[0].Operation != "record" || handled[0].Classification != "invalid_schema" || !handled[0].Dropped {
		t.Fatalf("handled = %#v", handled)
	}

	var nilObserver *Observer
	nilObserver.ToolMaterialized(context.Background(), ToolMaterialized{ToolName: "search"})
}

func TestAGUIToolMaterializedCarriesPrimitiveIDsAndRedactsSummary(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 5},
	})
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		ThreadID:            "ctx-thread",
		AGUIRunID:           "ctx-agui-run",
		ToolCallID:          "ctx-tool-call",
		TraceID:             "trace-1",
		ParentObservationID: "run-obs",
	})

	observer.AGUIToolMaterialized(ctx, AGUIToolMaterialized{
		Correlation:  Correlation{ObservationID: "agui-materialized"},
		ThreadID:     "thread-1",
		AGUIRunID:    "agui-run-1",
		ToolCallID:   "tool-call-1",
		ToolName:     "client-search",
		InputSummary: Summary{Name: "proposal", Text: "hello world"},
		Metadata:     Metadata{"source": "agui"},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "tool.materialized" || got.Name != "tool.materialized" || got.Status != "ok" {
		t.Fatalf("shape = kind:%q name:%q status:%q", got.Kind, got.Name, got.Status)
	}
	if got.ID != "agui-materialized" || got.ParentID != "run-obs" || got.TraceID != "trace-1" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Attributes["tool.name"] != "client-search" ||
		got.Attributes["tool.call_id"] != "tool-call-1" ||
		got.Attributes["tool.kind"] != "client_proposed" ||
		got.Attributes["tool.status"] != "materialized" {
		t.Fatalf("tool attributes = %#v", got.Attributes)
	}
	if got.Attributes["correlation.thread_id"] != "thread-1" ||
		got.Attributes["correlation.agui_run_id"] != "agui-run-1" ||
		got.Attributes["correlation.tool_call_id"] != "tool-call-1" ||
		got.Attributes["correlation.session_id"] != "session-1" ||
		got.Attributes["correlation.run_id"] != "run-1" ||
		got.Attributes["metadata.source"] != "agui" {
		t.Fatalf("correlation/metadata attributes = %#v", got.Attributes)
	}
	if got.Attributes["tool.input.summary"] != "hello" {
		t.Fatalf("tool.input.summary = %q, want hello", got.Attributes["tool.input.summary"])
	}
	assertPublicRedactionRecord(t, got.Redaction, "tool.input.summary.text", "summary_truncated")
}

func TestAGUIToolSettledExportsSucceededFailedAndCanceledEvents(t *testing.T) {
	observer := New(Config{})

	observer.AGUIToolSettled(context.Background(), AGUIToolSettled{
		Correlation: Correlation{ObservationID: "settled-ok", TraceID: "trace-1"},
		ThreadID:    "thread-1",
		AGUIRunID:   "agui-run-1",
		ToolCallID:  "tool-call-1",
		ToolName:    "search",
		Status:      "succeeded",
	})
	observer.AGUIToolSettled(context.Background(), AGUIToolSettled{
		Correlation: Correlation{ObservationID: "settled-failed", TraceID: "trace-1"},
		ToolCallID:  "tool-call-2",
		ToolName:    "search",
		Status:      "failed",
		Error: ObservationError{
			Operation:      "tool_call",
			Classification: "tool_error",
			Err:            errSentinel{},
			Retryable:      true,
		},
	})
	observer.AGUIToolSettled(context.Background(), AGUIToolSettled{
		Correlation:    Correlation{ObservationID: "settled-canceled", TraceID: "trace-1"},
		ToolCallID:     "tool-call-3",
		ToolName:       "search",
		Status:         "canceled",
		Classification: "canceled",
		Retryable:      true,
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 3 || len(snapshot.Observations) != 3 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 3/3", snapshot.ExportCount, len(snapshot.Observations))
	}
	ok := snapshot.Observations[0]
	if ok.Kind != "tool.settled" || ok.Status != "ok" || ok.Attributes["tool.status"] != "succeeded" || ok.Error != nil {
		t.Fatalf("succeeded settlement = %#v", ok)
	}
	if ok.Attributes["correlation.thread_id"] != "thread-1" || ok.Attributes["correlation.agui_run_id"] != "agui-run-1" {
		t.Fatalf("succeeded correlation = %#v", ok.Attributes)
	}

	failed := snapshot.Observations[1]
	if failed.Status != "error" || failed.Attributes["tool.status"] != "failed" {
		t.Fatalf("failed settlement status = %#v", failed)
	}
	if failed.Error == nil || failed.Error.Operation != "tool_call" || failed.Error.Classification != "tool_error" || !failed.Error.Retryable {
		t.Fatalf("failed error = %#v", failed.Error)
	}
	if failed.Attributes["error.operation"] != "tool_call" ||
		failed.Attributes["error.classification"] != "tool_error" ||
		failed.Attributes["error.retryable"] != true {
		t.Fatalf("failed error attrs = %#v", failed.Attributes)
	}

	canceled := snapshot.Observations[2]
	if canceled.Status != "canceled" || canceled.Attributes["tool.status"] != "canceled" {
		t.Fatalf("canceled settlement status = %#v", canceled)
	}
	if canceled.Error == nil || canceled.Error.Operation != "tool_call" || canceled.Error.Classification != "canceled" || canceled.Error.Retryable {
		t.Fatalf("canceled error = %#v", canceled.Error)
	}
	if canceled.Attributes["error.retryable"] != false || canceled.Attributes["error.canceled"] != true {
		t.Fatalf("canceled error attrs = %#v", canceled.Attributes)
	}
}

func TestAGUIToolHelpersRejectMissingToolCallIDAndInvalidStatus(t *testing.T) {
	var handled []ObservationError
	observer := New(Config{
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})

	observer.AGUIToolMaterialized(context.Background(), AGUIToolMaterialized{ToolName: "search"})
	observer.AGUIToolSettled(context.Background(), AGUIToolSettled{ToolName: "search"})
	observer.AGUIToolSettled(context.Background(), AGUIToolSettled{ToolCallID: "tool-call-1", Status: "pending"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
		t.Fatalf("invalid AG-UI helper snapshot = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
	if len(handled) != 3 {
		t.Fatalf("handled len = %d, want 3: %#v", len(handled), handled)
	}
	for _, err := range handled {
		if err.Operation != "record" || err.Classification != "invalid_schema" || !err.Dropped {
			t.Fatalf("handled error = %#v", err)
		}
	}

	var nilObserver *Observer
	nilObserver.AGUIToolMaterialized(context.Background(), AGUIToolMaterialized{ToolName: "search"})
	nilObserver.AGUIToolSettled(context.Background(), AGUIToolSettled{ToolName: "search"})
}

func TestToolHelpersIgnoreCanceledCallerContext(t *testing.T) {
	observer := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	observer.ToolRegistered(ctx, ToolRegistered{ToolName: "search"})
	observer.ToolMaterialized(ctx, ToolMaterialized{ToolCallID: "tool-call-1", ToolName: "search"})
	observer.AGUIToolMaterialized(ctx, AGUIToolMaterialized{ToolCallID: "tool-call-2", ToolName: "search"})
	observer.AGUIToolSettled(ctx, AGUIToolSettled{ToolCallID: "tool-call-2", ToolName: "search"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 4 || len(snapshot.Observations) != 4 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 4/4", snapshot.ExportCount, len(snapshot.Observations))
	}
}

func TestToolHelpersAfterShutdownAreInert(t *testing.T) {
	var handled []ObservationError
	observer := New(Config{
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	observer.ToolRegistered(context.Background(), ToolRegistered{ToolName: "search"})
	observer.ToolMaterialized(context.Background(), ToolMaterialized{ToolCallID: "tool-call-1", ToolName: "search"})
	observer.AGUIToolMaterialized(context.Background(), AGUIToolMaterialized{ToolCallID: "tool-call-2", ToolName: "search"})
	observer.AGUIToolSettled(context.Background(), AGUIToolSettled{ToolCallID: "tool-call-2", ToolName: "search"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
		t.Fatalf("post-shutdown snapshot = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
	if len(handled) != 0 {
		t.Fatalf("post-shutdown handler calls = %#v", handled)
	}
}
