package model

import (
	"strings"
	"testing"
	"time"
)

func TestSpanDurationAndValidate(t *testing.T) {
	start := time.Date(2026, 6, 26, 12, 0, 0, 0, time.FixedZone("test", -4*60*60))
	span := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindModelCall, "model", start)
	span.SetAttr("genai.provider", "openai")
	span.SetAttr("genai.model", "gpt-example")
	span.End(start.Add(1500*time.Millisecond), StatusOK)

	if err := span.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if got := span.StartTime.Location(); got != time.UTC {
		t.Fatalf("StartTime location = %v, want UTC", got)
	}
	duration, ok := span.DurationMS()
	if !ok {
		t.Fatalf("DurationMS ok = false")
	}
	if duration != 1500 {
		t.Fatalf("DurationMS = %d, want 1500", duration)
	}
}

func TestSpanValidateRequiresIdentityKindNameStatusAndStart(t *testing.T) {
	err := (Span{}).Validate()
	for _, want := range []string{
		"observation id is required",
		"trace id is required",
		"invalid span kind",
		"span name is required",
		"invalid span status",
		"span start time is required",
	} {
		if !containsError(err, want) {
			t.Fatalf("Validate() = %v, want substring %q", err, want)
		}
	}
}

func TestSpanValidateTerminalErrorRequiresErrorFields(t *testing.T) {
	span := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindStream, "stream", time.Now())
	span.SetAttr("genai.provider", "openai")
	span.SetAttr("genai.model", "gpt-example")
	span.End(time.Now().Add(time.Second), StatusError)

	if err := span.Validate(); !containsError(err, "terminal error/canceled span requires error fields") {
		t.Fatalf("Validate() = %v", err)
	}

	span.Error = &ObservationError{Operation: "stream", Classification: "timeout", Retryable: true}
	if err := span.Validate(); err != nil {
		t.Fatalf("Validate() with error fields = %v", err)
	}
}

func TestSpanValidateRequiredAttributes(t *testing.T) {
	modelSpan := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindModelCall, "model", time.Now())
	if err := modelSpan.Validate(); !containsError(err, `required attribute "genai.provider" is missing`) {
		t.Fatalf("Validate() = %v", err)
	}
	if err := modelSpan.Validate(); !containsError(err, `required attribute "genai.model" is missing`) {
		t.Fatalf("Validate() = %v", err)
	}

	toolSpan := NewSpan(ObservationIdentity{ID: "span-2", TraceID: "trace-1"}, SpanKindToolCall, "tool", time.Now())
	toolSpan.SetAttr("tool.name", "search")
	toolSpan.SetAttr("tool.call_id", "tool-call-1")
	if err := toolSpan.Validate(); !containsError(err, `required attribute "tool.kind" is missing`) {
		t.Fatalf("Validate() = %v", err)
	}
	if err := toolSpan.Validate(); !containsError(err, `required attribute "tool.status" is missing`) {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestEventValidate(t *testing.T) {
	event := NewEvent(ObservationIdentity{ID: "event-1", ParentID: "span-1", TraceID: "trace-1"}, EventStreamChunk, time.Now())
	event.SetAttr("stream.chunk.index", int64(0))

	if err := event.Validate(); err != nil {
		t.Fatalf("Validate() error = %v", err)
	}
	if event.Category != "stream" {
		t.Fatalf("Category = %q, want stream", event.Category)
	}
}

func TestEventValidateRejectsBadStatusAndName(t *testing.T) {
	event := Event{
		Identity:  ObservationIdentity{ID: "event-1", TraceID: "trace-1"},
		Name:      "not-real",
		Category:  "custom",
		Status:    "bad",
		Timestamp: time.Now(),
	}

	err := event.Validate()
	if !containsError(err, "invalid event name") {
		t.Fatalf("Validate() = %v", err)
	}
	if !containsError(err, "invalid event status") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestEventValidateRequiredAttributes(t *testing.T) {
	firstToken := NewEvent(ObservationIdentity{ID: "event-1", TraceID: "trace-1"}, EventStreamFirstTok, time.Now())
	if err := firstToken.Validate(); !containsError(err, `required attribute "genai.latency.first_token_ms" is missing`) {
		t.Fatalf("Validate() = %v", err)
	}

	redaction := NewEvent(ObservationIdentity{ID: "event-2", TraceID: "trace-1"}, EventRedaction, time.Now())
	if err := redaction.Validate(); !containsError(err, "redaction.applied event requires redaction records") {
		t.Fatalf("Validate() = %v", err)
	}
	redaction.Redaction = []RedactionRecord{{FieldPath: "metadata.api_key", Reason: "default_omitted"}}
	if err := redaction.Validate(); err != nil {
		t.Fatalf("Validate() with records = %v", err)
	}
}

func TestCloneDeepCopiesMutableFields(t *testing.T) {
	span := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindToolCall, "tool", time.Now())
	span.SetAttr("tool.name", "search")
	span.Redaction = []RedactionRecord{{FieldPath: "metadata.api_key", Reason: "default_omitted"}}
	span.Error = &ObservationError{Operation: "tool_call", Classification: "tool_timeout"}
	event := NewEvent(ObservationIdentity{ID: "event-1", ParentID: "span-1", TraceID: "trace-1"}, EventToolSettled, time.Now())
	event.SetAttr("tool.status", "failed")
	span.Events = []Event{event}

	clone := span.Clone()
	clone.Attributes["tool.name"] = "other"
	clone.Redaction[0].Reason = "changed"
	clone.Events[0].Attributes["tool.status"] = "succeeded"
	clone.Error.Classification = "changed"

	if span.Attributes["tool.name"] != "search" {
		t.Fatalf("original attributes were mutated")
	}
	if span.Redaction[0].Reason != "default_omitted" {
		t.Fatalf("original redaction was mutated")
	}
	if span.Events[0].Attributes["tool.status"] != "failed" {
		t.Fatalf("original event attributes were mutated")
	}
	if span.Error.Classification != "tool_timeout" {
		t.Fatalf("original error was mutated")
	}
}

func TestObservationErrorValidateCanceledRetryable(t *testing.T) {
	span := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindRun, "run", time.Now())
	span.End(time.Now().Add(time.Second), StatusCanceled)
	span.Error = &ObservationError{
		Operation:      "run",
		Classification: "canceled",
		Canceled:       true,
		Retryable:      true,
	}

	if err := span.Validate(); !containsError(err, "canceled error cannot be retryable") {
		t.Fatalf("Validate() = %v", err)
	}
}

func containsError(err error, want string) bool {
	if err == nil {
		return false
	}
	for _, child := range unwrapAll(err) {
		if child != nil && strings.Contains(child.Error(), want) {
			return true
		}
	}
	return strings.Contains(err.Error(), want)
}

func unwrapAll(err error) []error {
	type unwrapper interface {
		Unwrap() []error
	}
	if joined, ok := err.(unwrapper); ok {
		return joined.Unwrap()
	}
	return nil
}
