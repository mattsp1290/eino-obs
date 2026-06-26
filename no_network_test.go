package einoobs

import (
	"context"
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

type errSentinel struct{}

func (errSentinel) Error() string {
	return "sentinel"
}
