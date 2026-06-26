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

	span.Error = &ObservationError{Operation: "stream", Classification: "timeout", Retryable: boolPtr(true)}
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

func TestEventValidateRejectsMismatchedCategory(t *testing.T) {
	event := NewEvent(ObservationIdentity{ID: "event-1", TraceID: "trace-1"}, EventStreamChunk, time.Now())
	event.Category = "tool"
	event.SetAttr("stream.chunk.index", int64(0))

	if err := event.Validate(); !containsError(err, "does not match event name") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestEventValidateRequiredAttributes(t *testing.T) {
	firstToken := NewEvent(ObservationIdentity{ID: "event-1", TraceID: "trace-1"}, EventStreamFirstTok, time.Now())
	if err := firstToken.Validate(); !containsError(err, `required attribute "genai.latency.first_token_ms" is missing`) {
		t.Fatalf("Validate() = %v", err)
	}
	firstToken.SetAttr("genai.latency.first_token_ms", "slow")
	if err := firstToken.Validate(); !containsError(err, `required attribute "genai.latency.first_token_ms" must be int64`) {
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

func TestValidateRejectsUnsupportedAttributeValues(t *testing.T) {
	event := NewEvent(ObservationIdentity{ID: "event-1", TraceID: "trace-1"}, EventStreamChunk, time.Now())
	event.SetAttr("stream.chunk.index", nil)
	event.SetAttr("metadata.mutable", []string{"bad"})

	err := event.Validate()
	if !containsError(err, `attribute "stream.chunk.index" has nil value`) {
		t.Fatalf("Validate() = %v", err)
	}
	if !containsError(err, `attribute "metadata.mutable" has unsupported value type []string`) {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestToolLifecycleStatusValidation(t *testing.T) {
	registered := NewEvent(ObservationIdentity{ID: "event-1", TraceID: "trace-1"}, EventToolRegistered, time.Now())
	registered.SetAttr("tool.name", "search")
	registered.SetAttr("tool.kind", "server")
	registered.SetAttr("tool.status", "succeeded")
	if err := registered.Validate(); !containsError(err, "tool.registered requires tool.status=registered") {
		t.Fatalf("Validate() = %v", err)
	}

	settled := NewEvent(ObservationIdentity{ID: "event-2", TraceID: "trace-1"}, EventToolSettled, time.Now())
	settled.SetAttr("tool.name", "search")
	settled.SetAttr("tool.call_id", "tool-call-1")
	settled.SetAttr("tool.kind", "server")
	settled.SetAttr("tool.status", "failed")
	if err := settled.Validate(); !containsError(err, "terminal tool failure/cancellation requires error fields") {
		t.Fatalf("Validate() = %v", err)
	}

	settled.Error = &ObservationError{Operation: "tool_call", Classification: "tool_timeout", Retryable: boolPtr(false)}
	if err := settled.Validate(); err != nil {
		t.Fatalf("Validate() with error fields = %v", err)
	}
}

func TestCloneDeepCopiesMutableFields(t *testing.T) {
	span := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindToolCall, "tool", time.Now())
	span.SetAttr("tool.name", "search")
	span.SetAttr("metadata.bytes", []byte("secret"))
	span.Redaction = []RedactionRecord{{FieldPath: "metadata.api_key", Reason: "default_omitted"}}
	span.Error = &ObservationError{Operation: "tool_call", Classification: "tool_timeout", Retryable: boolPtr(false)}
	event := NewEvent(ObservationIdentity{ID: "event-1", ParentID: "span-1", TraceID: "trace-1"}, EventToolSettled, time.Now())
	event.SetAttr("tool.status", "failed")
	span.Events = []Event{event}

	clone := span.Clone()
	clone.Attributes["tool.name"] = "other"
	clone.Attributes["metadata.bytes"].([]byte)[0] = 'x'
	clone.Redaction[0].Reason = "changed"
	clone.Events[0].Attributes["tool.status"] = "succeeded"
	clone.Error.Classification = "changed"

	if span.Attributes["tool.name"] != "search" {
		t.Fatalf("original attributes were mutated")
	}
	if string(span.Attributes["metadata.bytes"].([]byte)) != "secret" {
		t.Fatalf("original byte attribute was mutated")
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
		Canceled:       boolPtr(true),
		Retryable:      boolPtr(true),
	}

	if err := span.Validate(); !containsError(err, "canceled error cannot be retryable") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestCanceledSpanRequiresCanceledTrue(t *testing.T) {
	span := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindRun, "run", time.Now())
	span.End(time.Now().Add(time.Second), StatusCanceled)
	span.Error = &ObservationError{
		Operation:      "run",
		Classification: "canceled",
		Retryable:      boolPtr(false),
	}

	if err := span.Validate(); !containsError(err, "canceled status requires error.canceled=true") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestErrorSpanRequiresExplicitRetryable(t *testing.T) {
	span := NewSpan(ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, SpanKindRun, "run", time.Now())
	span.End(time.Now().Add(time.Second), StatusError)
	span.Error = &ObservationError{Operation: "run", Classification: "runtime"}

	if err := span.Validate(); !containsError(err, "span error retryable flag is required") {
		t.Fatalf("Validate() = %v", err)
	}
}

func TestValidateRequiresUTCTimestamps(t *testing.T) {
	nonUTC := time.Date(2026, 6, 26, 12, 0, 0, 0, time.FixedZone("test", -4*60*60))
	span := Span{
		Identity:  ObservationIdentity{ID: "span-1", TraceID: "trace-1"},
		Kind:      SpanKindRun,
		Name:      "run",
		Status:    StatusOK,
		StartTime: nonUTC,
	}
	if err := span.Validate(); !containsError(err, "span start time must be UTC") {
		t.Fatalf("Validate() = %v", err)
	}

	event := Event{
		Identity:  ObservationIdentity{ID: "event-1", TraceID: "trace-1"},
		Name:      EventRetry,
		Category:  "lifecycle",
		Timestamp: nonUTC,
		Attributes: Attributes{
			"retry.attempt": int64(1),
			"retry.reason":  "rate_limit",
		},
	}
	if err := event.Validate(); !containsError(err, "event timestamp must be UTC") {
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

func boolPtr(value bool) *bool {
	return &value
}
