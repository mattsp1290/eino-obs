package exporter

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mattsp1290/eino-obs/internal/model"
)

type testRecorder struct {
	recorded []model.Span
}

func (t *testRecorder) Record(_ context.Context, span model.Span) error {
	t.recorded = append(t.recorded, span.Clone())
	return nil
}

func (t *testRecorder) Flush(context.Context) error {
	return nil
}

func (t *testRecorder) Shutdown(context.Context) error {
	return nil
}

func (t *testRecorder) Snapshot() Snapshot {
	return Snapshot{Recorded: append([]model.Span(nil), t.recorded...), RecordCount: int64(len(t.recorded))}
}

type testInternalExporter struct {
	exported []model.Span
}

func (t *testInternalExporter) Export(_ context.Context, batch []model.Span) error {
	for _, span := range batch {
		t.exported = append(t.exported, span.Clone())
	}
	return nil
}

func (t *testInternalExporter) Flush(context.Context) error {
	return nil
}

func (t *testInternalExporter) Shutdown(context.Context) error {
	return nil
}

func TestInternalInterfaces(t *testing.T) {
	var recorder Recorder = &testRecorder{}
	var snapshotter Snapshotter = recorder.(*testRecorder)
	var exporter Exporter = &testInternalExporter{}
	span := model.NewSpan(model.ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, model.SpanKindRun, "run", time.Now())

	if err := recorder.Record(context.Background(), span); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := exporter.Export(context.Background(), []model.Span{span}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if snapshot := snapshotter.Snapshot(); snapshot.RecordCount != 1 {
		t.Fatalf("RecordCount = %d, want 1", snapshot.RecordCount)
	}
}

func TestObservationError(t *testing.T) {
	cause := errors.New("cause")
	err := ObservationError{Operation: "flush", Classification: "timeout", Err: cause, Retryable: true}

	if got := err.Error(); got != "cause" {
		t.Fatalf("Error() = %q, want cause", got)
	}
	if !errors.Is(err, cause) {
		t.Fatalf("errors.Is did not match cause")
	}
}
