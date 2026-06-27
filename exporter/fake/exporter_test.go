package fake

import (
	"context"
	"errors"
	"testing"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
	internalexporter "github.com/mattsp1290/eino-obs/internal/exporter"
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

func TestExporterSnapshotInspectionIsDefensiveCopy(t *testing.T) {
	exp := New(Config{Capacity: 1})
	first := einoobs.Observation{ID: "span-1", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now(), Attributes: map[string]any{"metadata.value": "original"}}
	second := einoobs.Observation{ID: "span-2", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}

	if err := exp.Export(context.Background(), []einoobs.Observation{first, second}); err == nil {
		t.Fatalf("Export() capacity drop error = nil")
	}
	snapshot := exp.Snapshot()
	if len(snapshot.Recorded) != 1 || len(snapshot.Dropped) != 1 || len(snapshot.Errors) == 0 {
		t.Fatalf("snapshot before mutation = %#v", snapshot)
	}

	snapshot.Recorded[0].Attributes["metadata.value"] = "mutated"
	snapshot.Dropped[0].Reason = "mutated"
	snapshot.Dropped[0].Error.Classification = "mutated"
	snapshot.Errors[0].Classification = "mutated"
	snapshot.OperationCounts["export"] = 99
	if snapshot.LastError != nil {
		snapshot.LastError.Classification = "mutated"
	}

	again := exp.Snapshot()
	if again.Recorded[0].Attributes["metadata.value"] != "original" ||
		again.Dropped[0].Reason != "capacity" ||
		again.Dropped[0].Error.Classification != "capacity" ||
		again.Errors[0].Classification != "capacity" ||
		again.OperationCounts["export"] != 1 ||
		again.LastError == nil ||
		again.LastError.Classification != "capacity" {
		t.Fatalf("snapshot mutation changed exporter state: %#v", again)
	}
}

func TestExporterFailureInjectionForExportFlushAndShutdown(t *testing.T) {
	exp := New(Config{Failures: FailurePlan{
		Export: &Failure{
			Classification: "export_injected",
			Message:        "forced export failure",
			Retryable:      true,
			Dropped:        true,
		},
		Flush: &Failure{
			Classification: "flush_injected",
			Message:        "forced flush failure",
			Retryable:      true,
		},
		Shutdown: &Failure{
			Classification: "shutdown_injected",
			Message:        "forced shutdown failure",
			Dropped:        true,
		},
	}})
	obs := einoobs.Observation{ID: "span-1", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}

	if err := exp.Export(context.Background(), []einoobs.Observation{obs}); err == nil || err.Error() != "forced export failure" {
		t.Fatalf("Export() error = %v, want forced export failure", err)
	}
	snapshot := exp.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Recorded) != 0 || !snapshot.Dirty {
		t.Fatalf("snapshot after export injection = %#v", snapshot)
	}
	if len(snapshot.Dropped) != 1 ||
		snapshot.Dropped[0].Reason != "injected_failure" ||
		snapshot.Dropped[0].Observation.ID != "span-1" ||
		snapshot.Dropped[0].Error.Classification != "export_injected" {
		t.Fatalf("dropped export snapshot = %#v", snapshot.Dropped)
	}
	if snapshot.LastError == nil ||
		snapshot.LastError.Operation != "export" ||
		snapshot.LastError.Classification != "export_injected" ||
		!snapshot.LastError.Retryable ||
		!snapshot.LastError.Dropped {
		t.Fatalf("last export error = %#v", snapshot.LastError)
	}

	if err := exp.Flush(context.Background()); err == nil || err.Error() != "forced flush failure" {
		t.Fatalf("Flush() error = %v, want forced flush failure", err)
	}
	snapshot = exp.Snapshot()
	if snapshot.FlushCount != 1 || snapshot.LastFlushError == nil ||
		snapshot.LastFlushError.Operation != "flush" ||
		snapshot.LastFlushError.Classification != "flush_injected" ||
		!snapshot.LastFlushError.Retryable ||
		snapshot.LastFlushError.Dropped {
		t.Fatalf("last flush error = %#v snapshot=%#v", snapshot.LastFlushError, snapshot)
	}

	if err := exp.Shutdown(context.Background()); err == nil || err.Error() != "forced shutdown failure" {
		t.Fatalf("Shutdown() error = %v, want forced shutdown failure", err)
	}
	snapshot = exp.Snapshot()
	if snapshot.ShutdownCount != 1 || snapshot.LastShutdownError == nil ||
		snapshot.LastShutdownError.Operation != "shutdown" ||
		snapshot.LastShutdownError.Classification != "shutdown_injected" ||
		snapshot.LastShutdownError.Retryable ||
		!snapshot.LastShutdownError.Dropped {
		t.Fatalf("last shutdown error = %#v snapshot=%#v", snapshot.LastShutdownError, snapshot)
	}
	if len(snapshot.Errors) != 4 {
		t.Fatalf("errors len = %d, want 4", len(snapshot.Errors))
	}
}

func TestExporterResetClearsInjectedErrorsButKeepsFailurePlan(t *testing.T) {
	exp := New(Config{Failures: FailurePlan{
		Export: &Failure{Classification: "export_injected", Message: "forced export failure"},
	}})
	obs := einoobs.Observation{ID: "span-1", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}

	if err := exp.Export(context.Background(), []einoobs.Observation{obs}); err == nil {
		t.Fatalf("Export() error = nil")
	}
	exp.Reset()
	snapshot := exp.Snapshot()
	if snapshot.LastError != nil || len(snapshot.Errors) != 0 || snapshot.ExportCount != 0 {
		t.Fatalf("snapshot after reset = %#v", snapshot)
	}

	if err := exp.Export(context.Background(), []einoobs.Observation{obs}); err == nil || err.Error() != "forced export failure" {
		t.Fatalf("Export() after reset error = %v, want forced export failure", err)
	}
	snapshot = exp.Snapshot()
	if snapshot.ExportCount != 1 || snapshot.LastError == nil || snapshot.LastError.Classification != "export_injected" {
		t.Fatalf("snapshot after reinjected export = %#v", snapshot)
	}
}

func TestExporterCallIndexedFailureInjection(t *testing.T) {
	exp := New(Config{Failures: FailurePlan{
		ExportRules: []Failure{
			{
				AtCall:         1,
				Classification: "export_retry",
				Message:        "retry later",
				Retryable:      true,
			},
			{
				AtCall:         3,
				Classification: "export_retry_again",
				Message:        "retry export later",
				Retryable:      true,
			},
		},
		Flush: &Failure{
			AtCall:         1,
			Classification: "flush_retry",
			Message:        "flush later",
			Retryable:      true,
		},
		Shutdown: &Failure{
			AtCall:         1,
			Classification: "shutdown_retry",
			Message:        "shutdown later",
			Retryable:      true,
		},
	}})
	obs := einoobs.Observation{ID: "span-1", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}

	err := exp.Export(context.Background(), []einoobs.Observation{obs})
	var obsErr internalexporter.ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("Export() error = %T, want ObservationError", err)
	}
	if obsErr.Classification != "export_retry" || !obsErr.Retryable || obsErr.Dropped {
		t.Fatalf("Export() ObservationError = %#v", obsErr)
	}
	snapshot := exp.Snapshot()
	if len(snapshot.Pending) != 1 || len(snapshot.Recorded) != 0 || len(snapshot.Dropped) != 0 {
		t.Fatalf("snapshot after retryable export injection = %#v", snapshot)
	}

	if err := exp.Export(context.Background(), []einoobs.Observation{obs}); err != nil {
		t.Fatalf("second Export() error = %v", err)
	}
	snapshot = exp.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Pending) != 1 || len(snapshot.Recorded) != 1 {
		t.Fatalf("snapshot after second export = %#v", snapshot)
	}

	if err := exp.Flush(context.Background()); err == nil || err.Error() != "flush later" {
		t.Fatalf("first Flush() error = %v, want flush later", err)
	}
	err = exp.Flush(context.Background())
	if !errors.As(err, &obsErr) || obsErr.Classification != "export_retry_again" {
		t.Fatalf("second Flush() error = %#v, want export_retry_again", err)
	}
	snapshot = exp.Snapshot()
	if snapshot.ExportCount != 3 || len(snapshot.Pending) != 1 || !snapshot.Dirty || snapshot.LastFlushError == nil {
		t.Fatalf("snapshot after retry export failure during flush = %#v", snapshot)
	}
	if err := exp.Flush(context.Background()); err != nil {
		t.Fatalf("third Flush() error = %v", err)
	}
	snapshot = exp.Snapshot()
	if snapshot.FlushCount != 3 ||
		snapshot.ExportCount != 4 ||
		len(snapshot.Pending) != 0 ||
		len(snapshot.Recorded) != 2 ||
		snapshot.Dirty {
		t.Fatalf("snapshot after third flush = %#v", snapshot)
	}

	if err := exp.Shutdown(context.Background()); err == nil || err.Error() != "shutdown later" {
		t.Fatalf("first Shutdown() error = %v, want shutdown later", err)
	}
	err = exp.Export(context.Background(), []einoobs.Observation{obs})
	if !errors.As(err, &obsErr) || obsErr.Classification != "exporter_closed" {
		t.Fatalf("Export() after failed shutdown = %#v, want exporter_closed", err)
	}
	if err := exp.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	snapshot = exp.Snapshot()
	if snapshot.ShutdownCount != 2 || snapshot.LastShutdownError != nil {
		t.Fatalf("snapshot after second shutdown = %#v", snapshot)
	}
	if err := exp.Export(context.Background(), []einoobs.Observation{obs}); err == nil {
		t.Fatalf("post-shutdown Export() error = nil")
	}
}

func TestExporterFlushPendingCapacityErrorStaysDirty(t *testing.T) {
	exp := New(Config{
		Capacity: 1,
		Failures: FailurePlan{
			Export: &Failure{
				AtCall:         1,
				Classification: "export_retry",
				Message:        "retry later",
				Retryable:      true,
			},
		},
	})
	pending := einoobs.Observation{ID: "pending", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}
	recorded := einoobs.Observation{ID: "recorded", TraceID: "trace-1", Kind: "run", Name: "run", Status: "ok", Timestamp: time.Now()}

	if err := exp.Export(context.Background(), []einoobs.Observation{pending}); err == nil {
		t.Fatalf("first Export() error = nil")
	}
	if err := exp.Export(context.Background(), []einoobs.Observation{recorded}); err != nil {
		t.Fatalf("second Export() error = %v", err)
	}
	err := exp.Flush(context.Background())
	var obsErr internalexporter.ObservationError
	if !errors.As(err, &obsErr) || obsErr.Operation != "flush" || obsErr.Classification != "capacity" {
		t.Fatalf("Flush() error = %#v, want flush capacity ObservationError", err)
	}
	snapshot := exp.Snapshot()
	if !snapshot.Dirty ||
		snapshot.LastFlushError == nil ||
		snapshot.LastFlushError.Classification != "capacity" ||
		len(snapshot.Dropped) != 1 ||
		snapshot.Dropped[0].Observation.ID != "pending" ||
		len(snapshot.Pending) != 0 {
		t.Fatalf("snapshot after flush capacity error = %#v", snapshot)
	}
}
