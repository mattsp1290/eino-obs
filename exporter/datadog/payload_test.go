package datadog

import (
	"testing"
	"time"

	"github.com/mattsp1290/eino-obs/internal/model"
)

func TestBuildPayloadMapsEndedSpanFieldsMetricsAndMetadata(t *testing.T) {
	start := time.Date(2026, 6, 26, 12, 0, 0, 0, time.FixedZone("offset", -4*60*60))
	span := model.NewSpan(
		model.ObservationIdentity{ID: "span-1", ParentID: "run-1", TraceID: "trace-1"},
		model.SpanKindModelCall,
		"openai.chat",
		start,
	)
	span.End(start.Add(250*time.Millisecond), model.StatusOK)
	span.SetAttr("genai.provider", "openai")
	span.SetAttr("genai.model", "gpt-example")
	span.SetAttr("genai.usage.input_tokens", int64(0))
	span.SetAttr("genai.usage.output_tokens", int64(12))
	span.SetAttr("genai.usage.total_tokens", int64(12))
	span.SetAttr("genai.usage.reasoning_tokens", int64(2))
	span.SetAttr("correlation.session_id", "session-1")
	span.SetAttr("metadata.unsupported", []string{"drop"})
	span.Redaction = []model.RedactionRecord{{
		FieldPath:     "genai.request.summary",
		Reason:        "summary_truncated",
		OriginalBytes: 12,
		RetainedBytes: 4,
	}}

	got := buildPayload(Config{MLApp: "ml-app", Service: "svc", Env: "prod", Version: "v1"}, []model.Span{span})
	if len(got.Spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(got.Spans))
	}
	item := got.Spans[0]
	if item.TraceID != "trace-1" || item.SpanID != "span-1" || item.ParentID != "run-1" {
		t.Fatalf("identity = %#v", item)
	}
	if item.Name != "openai.chat" || item.StartNS != start.UTC().UnixNano() || item.Duration != int64(250*time.Millisecond) {
		t.Fatalf("timing/name = %#v", item)
	}
	if item.MLApp != "ml-app" || item.Meta["kind"] != "llm" || item.Meta["status"] != "ok" {
		t.Fatalf("meta identity = %#v", item.Meta)
	}
	if item.Meta["service.name"] != "svc" || item.Meta["service.env"] != "prod" || item.Meta["service.version"] != "v1" {
		t.Fatalf("service meta = %#v", item.Meta)
	}
	if item.Meta["genai.provider"] != "openai" ||
		item.Meta["genai.model"] != "gpt-example" ||
		item.Meta["genai.usage.reasoning_tokens"] != int64(2) ||
		item.Meta["correlation.session_id"] != "session-1" {
		t.Fatalf("attribute meta = %#v", item.Meta)
	}
	if _, ok := item.Meta["metadata.unsupported"]; ok {
		t.Fatalf("unsupported attribute was retained: %#v", item.Meta)
	}
	if item.Metrics["input_tokens"] != int64(0) ||
		item.Metrics["output_tokens"] != int64(12) ||
		item.Metrics["total_tokens"] != int64(12) {
		t.Fatalf("metrics = %#v", item.Metrics)
	}
	records := item.Meta["metadata.redaction.records"].([]redactionPayload)
	if len(records) != 1 || records[0].FieldPath != "genai.request.summary" || records[0].RetainedBytes != 4 {
		t.Fatalf("redaction records = %#v", records)
	}
}

func TestBuildPayloadSkipsActiveSpansAndMapsKinds(t *testing.T) {
	start := time.Now().UTC()
	active := model.NewSpan(model.ObservationIdentity{ID: "active", TraceID: "trace-1"}, model.SpanKindRun, "active", start)
	session := model.NewSpan(model.ObservationIdentity{ID: "session", TraceID: "trace-1"}, model.SpanKindSession, "session", start)
	session.End(start, model.StatusOK)
	run := model.NewSpan(model.ObservationIdentity{ID: "run", TraceID: "trace-1"}, model.SpanKindRun, "run", start)
	run.End(start, model.StatusOK)
	tool := model.NewSpan(model.ObservationIdentity{ID: "tool", TraceID: "trace-1"}, model.SpanKindToolCall, "tool", start)
	tool.End(start, model.StatusOK)
	flush := model.NewSpan(model.ObservationIdentity{ID: "flush", TraceID: "trace-1"}, model.SpanKindExportFlush, "flush", start)
	flush.End(start, model.StatusOK)

	got := buildPayload(Config{}, []model.Span{active, session, run, tool, flush})
	if len(got.Spans) != 4 {
		t.Fatalf("spans = %d, want 4", len(got.Spans))
	}
	wantKinds := []string{"session", "workflow", "tool", "task"}
	for i, want := range wantKinds {
		if got.Spans[i].Meta["kind"] != want {
			t.Fatalf("span %d kind = %q, want %q", i, got.Spans[i].Meta["kind"], want)
		}
	}
}

func TestBuildPayloadMapsEventsAndErrors(t *testing.T) {
	start := time.Now().UTC()
	span := model.NewSpan(model.ObservationIdentity{ID: "run-obs", TraceID: "trace-1"}, model.SpanKindRun, "run", start)
	span.End(start.Add(time.Millisecond), model.StatusError)
	retryable := false
	dropped := true
	span.Error = &model.ObservationError{
		Operation:      "run",
		Type:           "runtime",
		Code:           "runtime.failure",
		Message:        "safe",
		Classification: "runtime",
		Retryable:      &retryable,
		Dropped:        &dropped,
	}
	event := model.NewEvent(model.ObservationIdentity{ID: "event-1", ParentID: "run-obs", TraceID: "trace-1"}, model.EventRetry, start.Add(500*time.Microsecond))
	event.SetAttr("retry.attempt", int64(2))
	event.SetAttr("retry.reason", "rate_limit")
	event.SetAttr("metadata.unsupported", map[string]any{"drop": true})
	event.Status = model.StatusError
	event.Redaction = []model.RedactionRecord{{FieldPath: "metadata.api_key", Reason: "default_omitted", OriginalBytes: 6}}
	span.Events = []model.Event{event}

	got := buildPayload(Config{MLApp: "app"}, []model.Span{span})
	item := got.Spans[0]
	if item.Meta["error.operation"] != "run" ||
		item.Meta["error.type"] != "runtime" ||
		item.Meta["error.code"] != "runtime.failure" ||
		item.Meta["error.message"] != "safe" ||
		item.Meta["error.classification"] != "runtime" ||
		item.Meta["error.retryable"] != false ||
		item.Meta["error.dropped"] != true {
		t.Fatalf("error meta = %#v", item.Meta)
	}
	if len(item.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(item.Events))
	}
	gotEvent := item.Events[0]
	if gotEvent.EventID != "event-1" || gotEvent.ParentID != "run-obs" || gotEvent.TraceID != "trace-1" || gotEvent.Name != "retry" {
		t.Fatalf("event identity = %#v", gotEvent)
	}
	if gotEvent.TimeNS != start.Add(500*time.Microsecond).UnixNano() ||
		gotEvent.Meta["retry.attempt"] != int64(2) ||
		gotEvent.Meta["retry.reason"] != "rate_limit" ||
		gotEvent.Meta["obs.status"] != "error" {
		t.Fatalf("event payload = %#v", gotEvent)
	}
	if _, ok := gotEvent.Meta["metadata.unsupported"]; ok {
		t.Fatalf("unsupported event attribute was retained: %#v", gotEvent.Meta)
	}
	records := gotEvent.Meta["metadata.redaction.records"].([]redactionPayload)
	if len(records) != 1 || records[0].FieldPath != "metadata.api_key" {
		t.Fatalf("event redaction = %#v", records)
	}
}

func TestBuildPayloadMapsStreamEvents(t *testing.T) {
	start := time.Now().UTC()
	span := model.NewSpan(model.ObservationIdentity{ID: "stream-obs", ParentID: "run-obs", TraceID: "trace-1"}, model.SpanKindStream, "chat.stream", start)
	span.SetAttr("genai.provider", "openai")
	span.SetAttr("genai.model", "gpt-example")
	span.SetAttr("genai.latency.first_token_ms", int64(125))
	span.SetAttr("genai.latency.total_ms", int64(1000))
	span.SetAttr("genai.usage.input_tokens", int64(0))
	span.End(start.Add(time.Second), model.StatusCanceled)
	retryable := false
	canceled := true
	span.Error = &model.ObservationError{
		Operation:      "stream",
		Classification: "canceled",
		Retryable:      &retryable,
		Canceled:       &canceled,
	}
	chunk := model.NewEvent(model.ObservationIdentity{ID: "stream-obs.chunk.0", ParentID: "stream-obs", TraceID: "trace-1"}, model.EventStreamChunk, start.Add(100*time.Millisecond))
	chunk.SetAttr("stream.chunk.index", int64(0))
	chunk.SetAttr("stream.chunk.summary", "delta")
	firstToken := model.NewEvent(model.ObservationIdentity{ID: "stream-obs.first_token", ParentID: "stream-obs", TraceID: "trace-1"}, model.EventStreamFirstTok, start.Add(125*time.Millisecond))
	firstToken.SetAttr("genai.latency.first_token_ms", int64(125))
	cancellation := model.NewEvent(model.ObservationIdentity{ID: "stream-obs.cancellation", ParentID: "stream-obs", TraceID: "trace-1"}, model.EventCancellation, start.Add(time.Second))
	cancellation.Status = model.StatusCanceled
	cancellation.SetAttr("cancellation.reason", "canceled")
	cancellation.Error = span.Error
	span.Events = []model.Event{chunk, firstToken, cancellation}

	got := buildPayload(Config{MLApp: "app"}, []model.Span{span})
	if len(got.Spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(got.Spans))
	}
	item := got.Spans[0]
	if item.Meta["kind"] != "llm" ||
		item.Meta["genai.latency.first_token_ms"] != int64(125) ||
		item.Metrics["input_tokens"] != int64(0) {
		t.Fatalf("stream payload meta/metrics = meta:%#v metrics:%#v", item.Meta, item.Metrics)
	}
	if len(item.Events) != 3 {
		t.Fatalf("events = %d, want 3", len(item.Events))
	}
	if item.Events[0].Name != "stream.chunk" ||
		item.Events[0].Meta["stream.chunk.index"] != int64(0) ||
		item.Events[1].Name != "stream.first_token" ||
		item.Events[1].Meta["genai.latency.first_token_ms"] != int64(125) ||
		item.Events[2].Name != "cancellation" ||
		item.Events[2].Meta["error.canceled"] != true ||
		item.Events[2].Meta["error.retryable"] != false {
		t.Fatalf("stream events payload = %#v", item.Events)
	}
}
