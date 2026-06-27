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

func TestToolHelpersIgnoreCanceledCallerContext(t *testing.T) {
	observer := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	observer.ToolRegistered(ctx, ToolRegistered{ToolName: "search"})
	observer.ToolMaterialized(ctx, ToolMaterialized{ToolCallID: "tool-call-1", ToolName: "search"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Observations) != 2 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 2/2", snapshot.ExportCount, len(snapshot.Observations))
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

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
		t.Fatalf("post-shutdown snapshot = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
	if len(handled) != 0 {
		t.Fatalf("post-shutdown handler calls = %#v", handled)
	}
}
