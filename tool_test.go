package einoobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mattsp1290/eino-obs/internal/model"
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

func TestToolInstrumentationCohesiveCorrelationRedactionAndErrors(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true, CaptureOutputSummary: true, MaxSummaryBytes: 4},
	})
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		ThreadID:            "thread-1",
		AGUIRunID:           "agui-run-1",
		TraceID:             "trace-1",
		ParentObservationID: "run-obs",
	})
	start := time.Date(2026, 6, 27, 2, 0, 0, 0, time.UTC)

	observer.ToolRegistered(ctx, ToolRegistered{
		Correlation: Correlation{ObservationID: "registered"},
		ToolKind:    "server",
		Time:        start,
	})
	observer.ToolMaterialized(ctx, ToolMaterialized{
		Correlation:  Correlation{ObservationID: "materialized"},
		ToolCallID:   "tool-call-1",
		InputSummary: Summary{Name: "input", Text: "hello world"},
		Time:         start.Add(time.Millisecond),
	})
	server := observer.StartToolCall(ctx, ToolCallStart{
		Correlation:  Correlation{ObservationID: "server-call"},
		ToolCallID:   "tool-call-1",
		InputSummary: Summary{Name: "input", Text: "hello world"},
		StartTime:    start.Add(2 * time.Millisecond),
	})
	server.Error(ToolCallError{
		Err:            errSentinel{},
		Classification: "tool_error",
		Retryable:      true,
		OutputSummary:  Summary{Name: "output", Text: "failed output"},
		EndTime:        start.Add(1502 * time.Millisecond),
	})
	observer.ToolSettled(ctx, ToolSettled{
		Correlation:    Correlation{ObservationID: "server-settled"},
		ToolCallID:     "tool-call-1",
		Status:         "failed",
		Latency:        1500 * time.Millisecond,
		LatencyKnown:   true,
		OutputSummary:  Summary{Name: "output", Text: "failed output"},
		Classification: "tool_error",
		Retryable:      true,
		Time:           start.Add(1503 * time.Millisecond),
	})
	observer.AGUIToolMaterialized(ctx, AGUIToolMaterialized{
		Correlation:  Correlation{ObservationID: "agui-materialized"},
		ToolCallID:   "agui-tool-call",
		InputSummary: Summary{Name: "proposal", Text: "client payload"},
		Time:         start.Add(2 * time.Second),
	})
	observer.AGUIToolSettled(ctx, AGUIToolSettled{
		Correlation:    Correlation{ObservationID: "agui-settled"},
		ToolCallID:     "agui-tool-call",
		Status:         "canceled",
		Classification: "canceled",
		Retryable:      true,
		Time:           start.Add(3 * time.Second),
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 6 || len(snapshot.Observations) != 6 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 6/6", snapshot.ExportCount, len(snapshot.Observations))
	}
	for _, observation := range snapshot.Observations {
		validateNormalizedToolObservation(t, observation)
		if observation.Attributes["correlation.session_id"] != "session-1" ||
			observation.Attributes["correlation.run_id"] != "run-1" ||
			observation.Attributes["correlation.thread_id"] != "thread-1" ||
			observation.Attributes["correlation.agui_run_id"] != "agui-run-1" {
			t.Fatalf("%s correlation attrs = %#v", observation.ID, observation.Attributes)
		}
		if observation.Attributes["tool.name"] == "" {
			t.Fatalf("%s missing fallback tool.name: %#v", observation.ID, observation.Attributes)
		}
		if observation.Attributes["tool.name"] != "tool_call" {
			t.Fatalf("%s fallback tool.name = %q, want tool_call", observation.ID, observation.Attributes["tool.name"])
		}
	}

	materialized := snapshot.Observations[1]
	if materialized.Attributes["tool.input.summary"] != "hell" {
		t.Fatalf("materialized input summary = %#v", materialized.Attributes)
	}
	assertPublicRedactionRecord(t, materialized.Redaction, "tool.input.summary.text", "summary_truncated")

	serverCall := snapshot.Observations[2]
	if serverCall.Status != "error" || serverCall.Attributes["tool.status"] != "failed" ||
		serverCall.Attributes["tool.latency.ms"] != int64(1500) ||
		serverCall.Attributes["error.classification"] != "tool_error" ||
		serverCall.Attributes["tool.output.summary"] != "fail" {
		t.Fatalf("server call attrs = status:%q attrs:%#v", serverCall.Status, serverCall.Attributes)
	}
	assertPublicRedactionRecord(t, serverCall.Redaction, "tool.output.summary.text", "summary_truncated")

	serverSettled := snapshot.Observations[3]
	if serverSettled.Status != "error" ||
		serverSettled.Attributes["tool.status"] != "failed" ||
		serverSettled.Attributes["tool.kind"] != "server" ||
		serverSettled.Attributes["error.retryable"] != true {
		t.Fatalf("server settled = status:%q attrs:%#v", serverSettled.Status, serverSettled.Attributes)
	}

	aguiMaterialized := snapshot.Observations[4]
	if aguiMaterialized.Attributes["tool.kind"] != "client_proposed" ||
		aguiMaterialized.Attributes["tool.call_id"] != "agui-tool-call" {
		t.Fatalf("agui materialized attrs = %#v", aguiMaterialized.Attributes)
	}

	aguiSettled := snapshot.Observations[5]
	if aguiSettled.Status != "canceled" ||
		aguiSettled.Attributes["tool.status"] != "canceled" ||
		aguiSettled.Attributes["tool.kind"] != "client_proposed" ||
		aguiSettled.Attributes["error.retryable"] != false ||
		aguiSettled.Attributes["error.canceled"] != true {
		t.Fatalf("agui settled = status:%q attrs:%#v", aguiSettled.Status, aguiSettled.Attributes)
	}
}

func TestStartToolCallEndExportsLatencySummariesAndCorrelation(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true, CaptureOutputSummary: true, MaxSummaryBytes: 4},
	})
	start := time.Date(2026, 6, 26, 12, 0, 0, 0, time.FixedZone("offset", -4*60*60))
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		TraceID:             "trace-1",
		ParentObservationID: "run-obs",
	})

	call := observer.StartToolCall(ctx, ToolCallStart{
		Correlation:  Correlation{ObservationID: "tool-span"},
		ToolCallID:   "tool-call-1",
		ToolName:     "search",
		StartTime:    start,
		InputSummary: Summary{Name: "query", Text: "hello world"},
		Metadata:     Metadata{"phase": "start"},
	})
	call.End(ToolCallEnd{
		EndTime:       start.Add(1500 * time.Millisecond),
		OutputSummary: Summary{Name: "result", Text: "found documents"},
		Metadata:      Metadata{"phase": "end"},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.ID != "tool-span" || got.ParentID != "run-obs" || got.TraceID != "trace-1" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Kind != "tool_call" || got.Name != "search" || got.Status != "ok" {
		t.Fatalf("shape = kind:%q name:%q status:%q", got.Kind, got.Name, got.Status)
	}
	if !got.Timestamp.Equal(start.UTC()) || got.Duration != 1500*time.Millisecond || !got.DurationKnown {
		t.Fatalf("timing = timestamp:%s duration:%s known:%v", got.Timestamp, got.Duration, got.DurationKnown)
	}
	if got.Attributes["tool.name"] != "search" ||
		got.Attributes["tool.call_id"] != "tool-call-1" ||
		got.Attributes["tool.kind"] != "server" ||
		got.Attributes["tool.status"] != "succeeded" ||
		got.Attributes["tool.latency.ms"] != int64(1500) {
		t.Fatalf("tool attributes = %#v", got.Attributes)
	}
	if got.Attributes["correlation.session_id"] != "session-1" ||
		got.Attributes["correlation.run_id"] != "run-1" ||
		got.Attributes["correlation.tool_call_id"] != "tool-call-1" ||
		got.Attributes["metadata.phase"] != "end" {
		t.Fatalf("correlation/metadata attributes = %#v", got.Attributes)
	}
	if got.Attributes["tool.input.summary"] != "hell" ||
		got.Attributes["tool.input.summary.name"] != "query" ||
		got.Attributes["tool.output.summary"] != "foun" {
		t.Fatalf("summary attributes = %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "tool.input.summary.text", "summary_truncated")
	assertPublicRedactionRecord(t, got.Redaction, "tool.output.summary.text", "summary_truncated")

	call.End(ToolCallEnd{EndTime: start.Add(time.Hour)})
	if snapshot := observer.Snapshot(); snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("second End exported again: %#v", snapshot)
	}
}

func validateNormalizedToolObservation(t *testing.T, observation Observation) {
	t.Helper()
	switch observation.Kind {
	case "tool.registered", "tool.materialized", "tool.settled":
		modelEvent := model.NewEvent(
			model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
			model.EventName(observation.Kind),
			observation.Timestamp,
		)
		modelEvent.Status = model.Status(observation.Status)
		modelEvent.Attributes = model.Attributes(observation.Attributes)
		if observation.Error != nil {
			retryable := observation.Error.Retryable
			dropped := observation.Error.Dropped
			canceled, _ := observation.Attributes["error.canceled"].(bool)
			modelEvent.Error = &model.ObservationError{
				Operation:      observation.Error.Operation,
				Classification: observation.Error.Classification,
				Message:        observation.Error.Error(),
				Retryable:      &retryable,
				Canceled:       &canceled,
				Dropped:        &dropped,
			}
		}
		if err := modelEvent.Validate(); err != nil {
			t.Fatalf("normalized event validation for %s failed: %v", observation.Kind, err)
		}
	case "tool_call":
		modelSpan := model.NewSpan(
			model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
			model.SpanKind(observation.Kind),
			observation.Name,
			observation.Timestamp,
		)
		modelSpan.Status = model.Status(observation.Status)
		modelSpan.Attributes = model.Attributes(observation.Attributes)
		if observation.DurationKnown {
			modelSpan.EndTime = observation.Timestamp.Add(observation.Duration).UTC()
		}
		if observation.Error != nil {
			retryable := observation.Error.Retryable
			dropped := observation.Error.Dropped
			canceled, _ := observation.Attributes["error.canceled"].(bool)
			modelSpan.Error = &model.ObservationError{
				Operation:      observation.Error.Operation,
				Classification: observation.Error.Classification,
				Message:        observation.Error.Error(),
				Retryable:      &retryable,
				Canceled:       &canceled,
				Dropped:        &dropped,
			}
		}
		if err := modelSpan.Validate(); err != nil {
			t.Fatalf("normalized span validation for %s failed: %v", observation.Kind, err)
		}
	default:
		t.Fatalf("unexpected tool observation kind %q", observation.Kind)
	}
}

func TestToolCallErrorExportsFailedAndCanceledSpans(t *testing.T) {
	observer := New(Config{})
	start := time.Now()

	failed := observer.StartToolCall(context.Background(), ToolCallStart{
		Correlation: Correlation{ObservationID: "failed-tool", TraceID: "trace-1"},
		ToolCallID:  "tool-call-1",
		ToolName:    "search",
		StartTime:   start,
	})
	failed.Error(ToolCallError{
		Err:            errSentinel{},
		Classification: "tool_timeout",
		Retryable:      true,
		EndTime:        start.Add(time.Second),
	})

	canceled := observer.StartToolCall(context.Background(), ToolCallStart{
		Correlation: Correlation{ObservationID: "canceled-tool", TraceID: "trace-1"},
		ToolCallID:  "tool-call-2",
		ToolName:    "lookup",
		StartTime:   start,
	})
	canceled.Error(ToolCallError{
		Err:            context.Canceled,
		Classification: "canceled",
		Canceled:       true,
		Retryable:      true,
		EndTime:        start.Add(2 * time.Second),
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Observations) != 2 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 2/2", snapshot.ExportCount, len(snapshot.Observations))
	}
	gotFailed := snapshot.Observations[0]
	if gotFailed.Status != "error" || gotFailed.Attributes["tool.status"] != "failed" {
		t.Fatalf("failed span status = %#v", gotFailed)
	}
	if gotFailed.Error == nil || gotFailed.Error.Operation != "tool_call" || gotFailed.Error.Classification != "tool_timeout" || !gotFailed.Error.Retryable {
		t.Fatalf("failed error = %#v", gotFailed.Error)
	}
	if gotFailed.Attributes["error.operation"] != "tool_call" ||
		gotFailed.Attributes["error.classification"] != "tool_timeout" ||
		gotFailed.Attributes["error.retryable"] != true {
		t.Fatalf("failed attrs = %#v", gotFailed.Attributes)
	}

	gotCanceled := snapshot.Observations[1]
	if gotCanceled.Status != "canceled" || gotCanceled.Attributes["tool.status"] != "canceled" {
		t.Fatalf("canceled span status = %#v", gotCanceled)
	}
	if gotCanceled.Error == nil || gotCanceled.Error.Operation != "tool_call" || gotCanceled.Error.Classification != "canceled" || gotCanceled.Error.Retryable {
		t.Fatalf("canceled error = %#v", gotCanceled.Error)
	}
	if gotCanceled.Attributes["error.retryable"] != false || gotCanceled.Attributes["error.canceled"] != true {
		t.Fatalf("canceled attrs = %#v", gotCanceled.Attributes)
	}
}

func TestToolSettledExportsLatencyOutputAndErrors(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureOutputSummary: true, MaxSummaryBytes: 6},
	})

	observer.ToolSettled(context.Background(), ToolSettled{
		Correlation:   Correlation{ObservationID: "settled-ok", TraceID: "trace-1"},
		ToolCallID:    "tool-call-1",
		ToolName:      "search",
		Status:        "succeeded",
		Latency:       1200 * time.Millisecond,
		LatencyKnown:  true,
		OutputSummary: Summary{Name: "result", Text: "documents found"},
	})
	observer.ToolSettled(context.Background(), ToolSettled{
		Correlation: Correlation{ObservationID: "settled-failed", TraceID: "trace-1"},
		ToolCallID:  "tool-call-2",
		ToolName:    "search",
		Status:      "failed",
		Error: ObservationError{
			Classification: "tool_error",
			Err:            errSentinel{},
			Retryable:      true,
		},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Observations) != 2 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 2/2", snapshot.ExportCount, len(snapshot.Observations))
	}
	ok := snapshot.Observations[0]
	if ok.Kind != "tool.settled" || ok.Status != "ok" || ok.Attributes["tool.status"] != "succeeded" {
		t.Fatalf("settled ok = %#v", ok)
	}
	if ok.Attributes["tool.latency.ms"] != int64(1200) ||
		ok.Attributes["tool.output.summary"] != "docume" {
		t.Fatalf("settled ok attrs = %#v", ok.Attributes)
	}
	assertPublicRedactionRecord(t, ok.Redaction, "tool.output.summary.text", "summary_truncated")

	failed := snapshot.Observations[1]
	if failed.Status != "error" || failed.Attributes["tool.status"] != "failed" {
		t.Fatalf("settled failed = %#v", failed)
	}
	if failed.Error == nil || failed.Error.Operation != "tool_call" || failed.Error.Classification != "tool_error" || !failed.Error.Retryable {
		t.Fatalf("settled failed error = %#v", failed.Error)
	}
}

func TestServerToolHelpersRejectMissingToolCallIDAndInvalidSettlementStatus(t *testing.T) {
	var handled []ObservationError
	observer := New(Config{
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})

	observer.StartToolCall(context.Background(), ToolCallStart{ToolName: "search"}).End(ToolCallEnd{})
	observer.ToolSettled(context.Background(), ToolSettled{ToolName: "search"})
	observer.ToolSettled(context.Background(), ToolSettled{ToolCallID: "tool-call-1", Status: "pending"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
		t.Fatalf("invalid server tool snapshot = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
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
	nilObserver.StartToolCall(context.Background(), ToolCallStart{ToolName: "search"}).End(ToolCallEnd{})
	nilObserver.ToolSettled(context.Background(), ToolSettled{ToolName: "search"})
}

func TestServerToolHelpersDefaultEmptyToolNameClampLatencyAndValidateModel(t *testing.T) {
	observer := New(Config{})

	observer.StartToolCall(context.Background(), ToolCallStart{
		Correlation: Correlation{ObservationID: "tool-span", TraceID: "trace-1"},
		ToolCallID:  "tool-call-1",
	}).End(ToolCallEnd{})
	observer.ToolSettled(context.Background(), ToolSettled{
		Correlation:  Correlation{ObservationID: "settled-event", TraceID: "trace-1"},
		ToolCallID:   "tool-call-2",
		Status:       "succeeded",
		Latency:      -time.Millisecond,
		LatencyKnown: true,
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Observations) != 2 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 2/2", snapshot.ExportCount, len(snapshot.Observations))
	}
	span := snapshot.Observations[0]
	if span.Name != "tool_call" || span.Attributes["tool.name"] != "tool_call" {
		t.Fatalf("span tool name = name:%q attrs:%#v", span.Name, span.Attributes)
	}
	event := snapshot.Observations[1]
	if event.Attributes["tool.name"] != "tool_call" || event.Attributes["tool.latency.ms"] != int64(0) {
		t.Fatalf("settled event attrs = %#v", event.Attributes)
	}
	for _, observation := range snapshot.Observations {
		if observation.Kind == "tool.settled" {
			modelEvent := model.NewEvent(
				model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
				model.EventName(observation.Kind),
				observation.Timestamp,
			)
			modelEvent.Status = model.Status(observation.Status)
			modelEvent.Attributes = model.Attributes(observation.Attributes)
			if err := modelEvent.Validate(); err != nil {
				t.Fatalf("normalized event validation for %s failed: %v", observation.Kind, err)
			}
			continue
		}
		modelSpan := model.NewSpan(
			model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
			model.SpanKind(observation.Kind),
			observation.Name,
			observation.Timestamp,
		)
		modelSpan.Status = model.Status(observation.Status)
		modelSpan.Attributes = model.Attributes(observation.Attributes)
		if observation.DurationKnown {
			modelSpan.EndTime = observation.Timestamp.Add(observation.Duration).UTC()
		}
		if err := modelSpan.Validate(); err != nil {
			t.Fatalf("normalized validation for %s failed: %v", observation.Kind, err)
		}
	}
}

func TestToolCallConcurrentTerminalMethodsExportOnce(t *testing.T) {
	observer := New(Config{})
	call := observer.StartToolCall(context.Background(), ToolCallStart{
		Correlation: Correlation{ObservationID: "tool-span", TraceID: "trace-1"},
		ToolCallID:  "tool-call-1",
		ToolName:    "search",
	})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				call.End(ToolCallEnd{})
				return
			}
			call.Error(ToolCallError{Err: errSentinel{}, Classification: "tool_error", Retryable: true})
		}(i)
	}
	wg.Wait()

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("concurrent terminal exports = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
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
	observer.StartToolCall(ctx, ToolCallStart{ToolCallID: "tool-call-3", ToolName: "search"}).End(ToolCallEnd{})
	observer.ToolSettled(ctx, ToolSettled{ToolCallID: "tool-call-3", ToolName: "search"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 6 || len(snapshot.Observations) != 6 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 6/6", snapshot.ExportCount, len(snapshot.Observations))
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
	observer.StartToolCall(context.Background(), ToolCallStart{ToolCallID: "tool-call-3", ToolName: "search"}).End(ToolCallEnd{})
	observer.ToolSettled(context.Background(), ToolSettled{ToolCallID: "tool-call-3", ToolName: "search"})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
		t.Fatalf("post-shutdown snapshot = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
	if len(handled) != 0 {
		t.Fatalf("post-shutdown handler calls = %#v", handled)
	}
}
