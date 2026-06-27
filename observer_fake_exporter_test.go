package einoobs_test

import (
	"context"
	"errors"
	"testing"

	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/exporter/fake"
)

func TestObserverFlushFailureThroughFakeExporterSurface(t *testing.T) {
	fakeExporter := fake.New(fake.Config{Failures: fake.FailurePlan{
		Flush: &fake.Failure{
			Classification: "flush_injected",
			Message:        "forced flush failure",
			Retryable:      true,
		},
	}})
	var handled []einoobs.ObservationError
	observer := einoobs.New(einoobs.Config{
		Exporter: fakeExporter,
		ErrorHandler: func(_ context.Context, err einoobs.ObservationError) {
			handled = append(handled, err)
		},
	})

	err := observer.Flush(context.Background())
	var publicErr einoobs.ObservationError
	if !errors.As(err, &publicErr) {
		t.Fatalf("Flush() error = %T, want public ObservationError", err)
	}
	if publicErr.Operation != "flush" || publicErr.Classification != "exporter_failure" || !publicErr.Retryable || publicErr.Dropped {
		t.Fatalf("public ObservationError = %#v", publicErr)
	}
	if len(handled) != 1 || handled[0].Classification != "exporter_failure" {
		t.Fatalf("handled = %#v", handled)
	}
	snapshot := fakeExporter.Snapshot()
	if snapshot.LastFlushError == nil ||
		snapshot.LastFlushError.Operation != "flush" ||
		snapshot.LastFlushError.Classification != "flush_injected" ||
		!snapshot.LastFlushError.Retryable ||
		snapshot.LastFlushError.Dropped {
		t.Fatalf("fake snapshot after flush failure = %#v", snapshot)
	}
}

func TestObserverShutdownFailureThroughFakeExporterSurface(t *testing.T) {
	fakeExporter := fake.New(fake.Config{Failures: fake.FailurePlan{
		Shutdown: &fake.Failure{
			AtCall:         1,
			Classification: "shutdown_injected",
			Message:        "forced shutdown failure",
			Retryable:      true,
		},
	}})
	var handled []einoobs.ObservationError
	observer := einoobs.New(einoobs.Config{
		Exporter: fakeExporter,
		ErrorHandler: func(_ context.Context, err einoobs.ObservationError) {
			handled = append(handled, err)
		},
	})

	err := observer.Shutdown(context.Background())
	var publicErr einoobs.ObservationError
	if !errors.As(err, &publicErr) {
		t.Fatalf("Shutdown() error = %T, want public ObservationError", err)
	}
	if publicErr.Operation != "shutdown" || publicErr.Classification != "exporter_failure" || !publicErr.Retryable || publicErr.Dropped {
		t.Fatalf("public ObservationError = %#v", publicErr)
	}
	if len(handled) != 1 ||
		handled[0].Operation != "shutdown" ||
		handled[0].Classification != "exporter_failure" ||
		!handled[0].Retryable ||
		handled[0].Dropped {
		t.Fatalf("handled = %#v", handled)
	}
	snapshot := fakeExporter.Snapshot()
	if snapshot.LastShutdownError == nil ||
		snapshot.LastShutdownError.Operation != "shutdown" ||
		snapshot.LastShutdownError.Classification != "shutdown_injected" ||
		!snapshot.LastShutdownError.Retryable ||
		snapshot.LastShutdownError.Dropped {
		t.Fatalf("fake snapshot after shutdown failure = %#v", snapshot)
	}
	if err := observer.Flush(context.Background()); !errors.As(err, &publicErr) || publicErr.Classification != "exporter_failure" {
		t.Fatalf("Flush() after failed shutdown error = %#v, want retained shutdown failure", err)
	}
	if len(handled) != 1 {
		t.Fatalf("retained shutdown Flush invoked handler again: %#v", handled)
	}
}
