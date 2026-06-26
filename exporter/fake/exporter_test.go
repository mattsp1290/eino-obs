package fake

import (
	"context"
	"testing"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
)

func TestExporterPublicExportFlushShutdownAndReset(t *testing.T) {
	exp := New(Config{Redaction: einoobs.RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 4}})
	obs := einoobs.Observation{
		ID:        "obs-1",
		TraceID:   "trace-1",
		Kind:      "model_call",
		Name:      "model",
		Status:    "ok",
		Timestamp: time.Now(),
		Attributes: map[string]any{
			"genai.provider":        "openai",
			"genai.model":           "gpt-example",
			"tool.input.payload":    "raw",
			"genai.request.summary": "hello",
		},
	}

	if err := exp.Export(context.Background(), []einoobs.Observation{obs}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if err := exp.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := exp.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	snapshot := exp.Snapshot()
	if snapshot.ExportCount != 1 || snapshot.FlushCount != 1 || snapshot.ShutdownCount != 1 {
		t.Fatalf("counts = export:%d flush:%d shutdown:%d", snapshot.ExportCount, snapshot.FlushCount, snapshot.ShutdownCount)
	}
	if got := snapshot.Recorded[0].Attributes["genai.request.summary"]; got != "hell" {
		t.Fatalf("summary = %q, want hell", got)
	}
	if _, ok := snapshot.Recorded[0].Attributes["tool.input.payload"]; ok {
		t.Fatalf("raw tool input payload was retained")
	}

	exp.Reset()
	if len(exp.Snapshot().Recorded) != 0 {
		t.Fatalf("Reset did not clear recorded observations")
	}
}

func TestExporterInternalExportAndPostShutdownDrop(t *testing.T) {
	exp := New(Config{})
	obs := einoobs.Observation{ID: "span-1", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}
	if err := exp.Export(context.Background(), []einoobs.Observation{obs}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if err := exp.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	next := einoobs.Observation{ID: "span-2", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}
	if err := exp.Export(context.Background(), []einoobs.Observation{next}); err == nil {
		t.Fatalf("post-shutdown Export() error = nil")
	}
	snapshot := exp.Snapshot()
	if len(snapshot.Recorded) != 1 {
		t.Fatalf("recorded len = %d, want 1", len(snapshot.Recorded))
	}
	if len(snapshot.Dropped) != 1 {
		t.Fatalf("dropped len = %d, want 1", len(snapshot.Dropped))
	}
	if snapshot.LastError == nil || snapshot.LastError.Classification != "exporter_closed" {
		t.Fatalf("last error = %#v", snapshot.LastError)
	}
}

func TestExporterCapacityDropReturnsError(t *testing.T) {
	exp := New(Config{Capacity: 1})
	first := einoobs.Observation{ID: "span-1", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}
	second := einoobs.Observation{ID: "span-2", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}

	if err := exp.Export(context.Background(), []einoobs.Observation{first, second}); err == nil {
		t.Fatalf("Export() capacity drop error = nil")
	}
	snapshot := exp.Snapshot()
	if len(snapshot.Recorded) != 1 || len(snapshot.Dropped) != 1 || !snapshot.Dirty {
		t.Fatalf("snapshot after capacity drop = %#v", snapshot)
	}
}
