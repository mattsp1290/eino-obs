package exporter

import (
	"context"
	"errors"
	"testing"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
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
	return t.ExportInternal(context.Background(), batch)
}

func (t *testInternalExporter) ExportInternal(_ context.Context, batch []model.Span) error {
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

func (t *testInternalExporter) ExportPublic(_ context.Context, batch []einoobs.Observation) error {
	for _, obs := range batch {
		t.exported = append(t.exported, model.NewSpan(
			model.ObservationIdentity{ID: obs.ID, ParentID: obs.ParentID, TraceID: obs.TraceID},
			model.SpanKind(obs.Kind),
			obs.Name,
			obs.Timestamp,
		))
	}
	return nil
}

func TestInternalInterfaces(t *testing.T) {
	var recorder Recorder = &testRecorder{}
	var snapshotter Snapshotter = recorder.(*testRecorder)
	var exporter Exporter = &testInternalExporter{}
	var publicAdapter PublicAdapter = &publicAdapterFixture{testInternalExporter: &testInternalExporter{}}
	span := model.NewSpan(model.ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, model.SpanKindRun, "run", time.Now())

	if err := recorder.Record(context.Background(), span); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	if err := exporter.Export(context.Background(), []model.Span{span}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if err := publicAdapter.Export(context.Background(), []einoobs.Observation{{ID: "span-2", TraceID: "trace-1", Kind: "run", Name: "run", Timestamp: time.Now().UTC()}}); err != nil {
		t.Fatalf("public adapter Export() error = %v", err)
	}
	if err := publicAdapter.ExportInternal(context.Background(), []model.Span{span}); err != nil {
		t.Fatalf("public adapter ExportInternal() error = %v", err)
	}
	if snapshot := snapshotter.Snapshot(); snapshot.RecordCount != 1 {
		t.Fatalf("RecordCount = %d, want 1", snapshot.RecordCount)
	}
}

type publicAdapterFixture struct {
	*testInternalExporter
}

func (p *publicAdapterFixture) Export(ctx context.Context, batch []einoobs.Observation) error {
	for _, obs := range batch {
		span := model.NewSpan(
			model.ObservationIdentity{ID: obs.ID, ParentID: obs.ParentID, TraceID: obs.TraceID},
			model.SpanKind(obs.Kind),
			obs.Name,
			obs.Timestamp,
		)
		if err := p.ExportInternal(ctx, []model.Span{span}); err != nil {
			return err
		}
	}
	return nil
}

func TestObservationError(t *testing.T) {
	err := ObservationError{Operation: "flush", Classification: "timeout", Message: "safe message", Retryable: true}

	if got := err.Error(); got != "safe message" {
		t.Fatalf("Error() = %q, want safe message", got)
	}
	if errors.Is(err, errors.New("cause")) {
		t.Fatalf("snapshot error unexpectedly unwraps raw cause")
	}
}
