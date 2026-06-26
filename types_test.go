package einoobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

type testExporter struct {
	exported []Observation
}

func (t *testExporter) Export(_ context.Context, batch []Observation) error {
	t.exported = append(t.exported, batch...)
	return nil
}

func (t *testExporter) Flush(context.Context) error {
	return nil
}

func (t *testExporter) Shutdown(context.Context) error {
	return nil
}

func TestExporterInterface(t *testing.T) {
	var exporter Exporter = &testExporter{}
	err := exporter.Export(context.Background(), []Observation{{
		ID:            "obs-1",
		TraceID:       "trace-1",
		Kind:          "run",
		Name:          "run",
		Status:        "ok",
		Timestamp:     time.Now().UTC(),
		DurationKnown: true,
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
}

func TestObservationCloneDeepCopiesMutableFields(t *testing.T) {
	originalErr := errors.New("boom")
	obs := Observation{
		ID:         "obs-1",
		TraceID:    "trace-1",
		Attributes: map[string]any{"bytes": []byte("secret")},
		Events: []Observation{{
			ID:         "event-1",
			TraceID:    "trace-1",
			Attributes: map[string]any{"field": "value"},
		}},
		Redaction: []RedactionRecord{{FieldPath: "metadata.api_key", Reason: "default_omitted"}},
		Error:     &ObservationError{Operation: "export", Classification: "timeout", Err: originalErr, Retryable: true},
	}

	clone := obs.Clone()
	clone.Attributes["bytes"].([]byte)[0] = 'x'
	clone.Events[0].Attributes["field"] = "changed"
	clone.Redaction[0].Reason = "changed"
	clone.Error.Classification = "changed"

	if string(obs.Attributes["bytes"].([]byte)) != "secret" {
		t.Fatalf("original attribute was mutated")
	}
	if obs.Events[0].Attributes["field"] != "value" {
		t.Fatalf("original event was mutated")
	}
	if obs.Redaction[0].Reason != "default_omitted" {
		t.Fatalf("original redaction was mutated")
	}
	if obs.Error.Classification != "timeout" {
		t.Fatalf("original error was mutated")
	}
	if !errors.Is(clone.Error, originalErr) {
		t.Fatalf("clone error does not wrap original")
	}
}

func TestObservationError(t *testing.T) {
	cause := errors.New("cause")
	err := ObservationError{Operation: "export", Classification: "timeout", Err: cause}

	if got := err.Error(); got != "cause" {
		t.Fatalf("Error() = %q, want cause", got)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is did not match cause")
	}
}
