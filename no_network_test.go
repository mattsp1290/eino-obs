package einoobs

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestNewDefaultsToNoNetworkExporterWithSnapshot(t *testing.T) {
	observer := New(Config{
		Service: "svc",
		Redaction: RedactionOptions{
			CaptureInputSummary: true,
			MaxSummaryBytes:     4,
		},
	})
	if observer.Exporter() == nil {
		t.Fatalf("default exporter = nil")
	}

	err := observer.Exporter().Export(context.Background(), []Observation{{
		ID:        "obs-1",
		TraceID:   "trace-1",
		Kind:      "model_call",
		Name:      "model",
		Status:    "ok",
		Timestamp: time.Now(),
		Attributes: map[string]any{
			"genai.provider":        "openai",
			"genai.model":           "gpt-example",
			"prompt.text":           "raw prompt",
			"genai.request.summary": "hello",
		},
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !snapshot.Dirty {
		t.Fatalf("Dirty = false after accepted export")
	}
	got := snapshot.Observations[0]
	if _, ok := got.Attributes["prompt.text"]; ok {
		t.Fatalf("raw prompt was retained")
	}
	if got.Attributes["genai.request.summary"] != "hell" {
		t.Fatalf("summary = %q, want hell", got.Attributes["genai.request.summary"])
	}
	if len(got.Redaction) == 0 || got.Redaction[0].Reason != "summary_truncated" && got.Redaction[0].Reason != "default_omitted" {
		t.Fatalf("redaction records = %#v", got.Redaction)
	}

	snapshot.Observations[0].Attributes["genai.request.summary"] = "mutated"
	if observer.Snapshot().Observations[0].Attributes["genai.request.summary"] != "hell" {
		t.Fatalf("snapshot mutation changed exporter state")
	}
	if err := observer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if observer.Snapshot().Dirty {
		t.Fatalf("Dirty remained true after Flush")
	}

	observer.Reset()
	if afterReset := observer.Snapshot(); len(afterReset.Observations) != 0 || afterReset.ExportCount != 0 {
		t.Fatalf("snapshot after reset = %#v", afterReset)
	}
}

func TestWithNoNetworkUsesFinalRedactionOptions(t *testing.T) {
	for _, tc := range []struct {
		name string
		opts []Option
	}{
		{
			name: "redaction before no-network",
			opts: []Option{
				WithRedaction(RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 4}),
				WithNoNetwork(),
			},
		},
		{
			name: "redaction after no-network",
			opts: []Option{
				WithNoNetwork(),
				WithRedaction(RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 4}),
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			observer := New(Config{}, tc.opts...)
			err := observer.Exporter().Export(context.Background(), []Observation{{
				ID:        "obs-1",
				TraceID:   "trace-1",
				Kind:      "model_call",
				Name:      "model",
				Status:    "ok",
				Timestamp: time.Now(),
				Attributes: map[string]any{
					"genai.provider":        "openai",
					"genai.model":           "gpt-example",
					"genai.request.summary": "hello",
				},
			}})
			if err != nil {
				t.Fatalf("Export() error = %v", err)
			}
			if got := observer.Snapshot().Observations[0].Attributes["genai.request.summary"]; got != "hell" {
				t.Fatalf("summary = %q, want hell", got)
			}
		})
	}
}

func TestWithNoNetworkOverridesConfiguredExporter(t *testing.T) {
	custom := &testExporter{}
	observer := New(Config{Exporter: custom}, WithNoNetwork())

	if observer.Exporter() == nil || observer.Exporter() == custom {
		t.Fatalf("WithNoNetwork did not replace configured exporter")
	}
	if err := observer.Exporter().Export(context.Background(), []Observation{{
		ID:        "obs-1",
		TraceID:   "trace-1",
		Kind:      "run",
		Name:      "run",
		Status:    "ok",
		Timestamp: time.Now(),
	}}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(observer.Snapshot().Observations) != 1 {
		t.Fatalf("snapshot = %#v", observer.Snapshot())
	}
}

func TestObserverSnapshotWithCustomExporterIsEmpty(t *testing.T) {
	observer := New(Config{Exporter: &testExporter{}})

	if snapshot := observer.Snapshot(); len(snapshot.Observations) != 0 || snapshot.ExportCount != 0 {
		t.Fatalf("custom exporter snapshot = %#v, want empty no-network snapshot", snapshot)
	}
}

func TestNoNetworkSnapshotPreservesNestedEventNameAndError(t *testing.T) {
	observer := New(Config{})
	err := observer.Exporter().Export(context.Background(), []Observation{{
		ID:        "span-1",
		TraceID:   "trace-1",
		Kind:      "run",
		Name:      "run",
		Status:    "ok",
		Timestamp: time.Now(),
		Events: []Observation{{
			ID:        "event-1",
			ParentID:  "span-1",
			TraceID:   "trace-1",
			Kind:      "error",
			Name:      "error",
			Status:    "error",
			Timestamp: time.Now(),
			Attributes: map[string]any{
				"metadata.safe": "value",
			},
			Redaction: []RedactionRecord{{FieldPath: "metadata.api_key", Reason: "default_omitted"}},
			Error:     &ObservationError{Operation: "run", Classification: "runtime", Err: errSentinel{}, Retryable: false},
		}},
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	event := observer.Snapshot().Observations[0].Events[0]
	if event.Kind != "error" || event.Name != "error" {
		t.Fatalf("event identity = kind:%q name:%q, want error/error", event.Kind, event.Name)
	}
	if event.Error == nil || event.Error.Operation != "run" || event.Error.Classification != "runtime" {
		t.Fatalf("event error = %#v", event.Error)
	}
	if event.Redaction[0].FieldPath != "metadata.api_key" {
		t.Fatalf("event redaction = %#v", event.Redaction)
	}
}

func TestNoNetworkSnapshotExposesRedactionMetadataWithoutCredentials(t *testing.T) {
	const rawPrompt = "RAW_PROMPT_DO_NOT_LEAK_6on33"
	const apiSecret = "API_SECRET_DO_NOT_LEAK_6on33"
	const toolPayload = "TOOL_PAYLOAD_DO_NOT_LEAK_6on33"
	const attachmentPayload = "ATTACHMENT_DO_NOT_LEAK_6on33"
	const reasoningPayload = "REASONING_DO_NOT_LEAK_6on33"
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 3},
	})
	err := observer.Exporter().Export(context.Background(), []Observation{{
		ID:        "obs-1",
		TraceID:   "trace-1",
		Kind:      "model_call",
		Name:      "model",
		Status:    "ok",
		Timestamp: time.Now(),
		Attributes: map[string]any{
			"genai.provider":        "openai",
			"genai.model":           "gpt-example",
			"prompt.text":           rawPrompt,
			"genai.request.summary": "hello",
			"metadata.api_key":      apiSecret,
			"tool.input.payload":    toolPayload,
			"attachment.bytes":      attachmentPayload,
			"reasoning.text":        reasoningPayload,
		},
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	obs := observer.Snapshot().Observations[0]
	if _, ok := obs.Attributes["prompt.text"]; ok {
		t.Fatalf("raw prompt leaked into no-network snapshot")
	}
	if _, ok := obs.Attributes["metadata.api_key"]; ok {
		t.Fatalf("sensitive metadata leaked into no-network snapshot")
	}
	if got := obs.Attributes["genai.request.summary"]; got != "hel" {
		t.Fatalf("summary = %q, want hel", got)
	}
	assertPublicRedactionRecord(t, obs.Redaction, "prompt.text", "default_omitted")
	assertPublicRedactionRecord(t, obs.Redaction, "metadata.api_key", "default_omitted")
	assertPublicRedactionRecord(t, obs.Redaction, "genai.request.summary", "summary_truncated")
	assertPublicRedactionRecord(t, obs.Redaction, "tool.input.payload", "default_omitted")
	assertPublicRedactionRecord(t, obs.Redaction, "attachment.bytes", "default_omitted")
	assertPublicRedactionRecord(t, obs.Redaction, "reasoning.text", "default_omitted")
	assertNoSnapshotLeak(t, observer.Snapshot(), rawPrompt, apiSecret, toolPayload, attachmentPayload, reasoningPayload)
}

func TestNoNetworkSnapshotDoesNotRestoreRedactedErrorCause(t *testing.T) {
	const secret = "ENCRYPTED_REASONING_ERROR_DO_NOT_LEAK_6on18"
	observer := New(Config{})
	err := observer.Exporter().Export(context.Background(), []Observation{{
		ID:        "span-1",
		TraceID:   "trace-1",
		Kind:      "run",
		Name:      "run",
		Status:    "error",
		Timestamp: time.Now(),
		Error: &ObservationError{
			Operation:      "encrypted_reasoning",
			Classification: "runtime",
			Err:            secretErr(secret),
			Retryable:      false,
		},
		Events: []Observation{{
			ID:        "event-1",
			ParentID:  "span-1",
			TraceID:   "trace-1",
			Kind:      "error",
			Name:      "error",
			Status:    "error",
			Timestamp: time.Now(),
			Error: &ObservationError{
				Operation:      "encrypted_reasoning",
				Classification: "runtime",
				Err:            secretErr(secret),
				Retryable:      false,
			},
		}},
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}

	snapshot := observer.Snapshot()
	spanErr := snapshot.Observations[0].Error
	eventErr := snapshot.Observations[0].Events[0].Error
	if spanErr == nil || spanErr.Err != nil || strings.Contains(spanErr.Error(), secret) {
		t.Fatalf("span error restored redacted cause: %#v", spanErr)
	}
	if eventErr == nil || eventErr.Err != nil || strings.Contains(eventErr.Error(), secret) {
		t.Fatalf("event error restored redacted cause: %#v", eventErr)
	}
	assertNoSnapshotLeak(t, snapshot, secret)
	assertPublicRedactionRecord(t, snapshot.Observations[0].Redaction, "error.message", "encrypted_reasoning_forbidden")
	assertPublicRedactionRecord(t, snapshot.Observations[0].Events[0].Redaction, "error.message", "encrypted_reasoning_forbidden")
}

type errSentinel struct{}

func (errSentinel) Error() string {
	return "sentinel"
}

type secretErr string

func (e secretErr) Error() string {
	return string(e)
}

func assertPublicRedactionRecord(t *testing.T, records []RedactionRecord, path string, reason string) {
	t.Helper()
	for _, record := range records {
		if record.FieldPath == path && record.Reason == reason {
			return
		}
	}
	t.Fatalf("missing public redaction record path=%s reason=%s in %#v", path, reason, records)
}

func assertNoSnapshotLeak(t *testing.T, snapshot NoNetworkSnapshot, forbidden ...string) {
	t.Helper()
	visited := map[uintptr]bool{}
	assertNoValueLeak(t, reflect.ValueOf(snapshot), "snapshot", visited, forbidden...)
}

func assertNoValueLeak(t *testing.T, value reflect.Value, path string, visited map[uintptr]bool, forbidden ...string) {
	t.Helper()
	if !value.IsValid() {
		return
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return
		}
		if value.Kind() == reflect.Pointer {
			ptr := value.Pointer()
			if visited[ptr] {
				return
			}
			visited[ptr] = true
		}
		assertNoValueLeak(t, value.Elem(), path, visited, forbidden...)
		return
	}
	switch value.Kind() {
	case reflect.String:
		got := value.String()
		for _, needle := range forbidden {
			if strings.Contains(got, needle) {
				t.Fatalf("snapshot leak at %s: %q contains %q", path, got, needle)
			}
		}
	case reflect.Slice, reflect.Array:
		if value.Type().Elem().Kind() == reflect.Uint8 {
			got := string(value.Bytes())
			for _, needle := range forbidden {
				if strings.Contains(got, needle) {
					t.Fatalf("snapshot byte leak at %s: %q contains %q", path, got, needle)
				}
			}
			return
		}
		for i := 0; i < value.Len(); i++ {
			assertNoValueLeak(t, value.Index(i), fmt.Sprintf("%s[%d]", path, i), visited, forbidden...)
		}
	case reflect.Map:
		for _, key := range value.MapKeys() {
			assertNoValueLeak(t, key, path+".<key>", visited, forbidden...)
			assertNoValueLeak(t, value.MapIndex(key), path+"["+fmt.Sprint(key.Interface())+"]", visited, forbidden...)
		}
	case reflect.Struct:
		for i := 0; i < value.NumField(); i++ {
			assertNoValueLeak(t, value.Field(i), path+"."+value.Type().Field(i).Name, visited, forbidden...)
		}
	}
}
