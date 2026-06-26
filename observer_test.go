package einoobs

import (
	"context"
	"errors"
	"testing"
)

func TestNewAppliesOptionsAndReturnsDefensiveConfig(t *testing.T) {
	exporter := &testExporter{}
	observer := New(
		Config{Service: "initial"},
		WithService("svc"),
		WithEnv("dev"),
		WithVersion("v1"),
		WithRedaction(RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 8}),
		WithExporter(exporter),
	)

	config := observer.Config()
	if config.Service != "svc" || config.Env != "dev" || config.Version != "v1" {
		t.Fatalf("config identity fields = %#v", config)
	}
	if !config.Redaction.CaptureInputSummary || config.Redaction.MaxSummaryBytes != 8 {
		t.Fatalf("redaction = %#v", config.Redaction)
	}
	if observer.Exporter() != exporter {
		t.Fatalf("Exporter() did not return configured exporter")
	}

	config.Service = "mutated"
	if observer.Config().Service != "svc" {
		t.Fatalf("Config() did not return a defensive value")
	}
}

func TestNewRejectsInvalidRedactionConfig(t *testing.T) {
	var handled []ObservationError
	observer := New(Config{
		Redaction: RedactionOptions{MaxSummaryBytes: -1},
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})
	err := observer.Flush(context.Background())
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("Flush() error = %v, want ErrInvalidConfig", err)
	}
	var obsErr ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("Flush() error = %v, want ObservationError", err)
	}
	if obsErr.Operation != "redact" || obsErr.Classification != "invalid_config" || !obsErr.Dropped {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
	if len(handled) != 1 || handled[0].Classification != "invalid_config" {
		t.Fatalf("handled = %#v", handled)
	}
}

func TestObserverFlushShutdownNilExporterAreNoNetworkNoops(t *testing.T) {
	observer := New(Config{Service: "svc"})
	if err := observer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestObserverFlushShutdownDelegateToExporter(t *testing.T) {
	exporter := &testExporter{}
	observer := New(Config{Exporter: exporter})

	if err := observer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if exporter.flushes != 1 || exporter.shutdowns != 1 {
		t.Fatalf("counts = flush:%d shutdown:%d", exporter.flushes, exporter.shutdowns)
	}
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("second Shutdown() error = %v", err)
	}
	if exporter.shutdowns != 1 {
		t.Fatalf("second Shutdown delegated, count = %d", exporter.shutdowns)
	}
	if err := observer.Flush(context.Background()); err != nil {
		t.Fatalf("post-shutdown Flush() error = %v", err)
	}
	if exporter.flushes != 1 {
		t.Fatalf("post-shutdown Flush delegated, count = %d", exporter.flushes)
	}
}

func TestObserverLifecycleErrorsNormalizeAndInvokeHandler(t *testing.T) {
	cause := errors.New("flush failed")
	exporter := &testExporter{flushErr: cause}
	var handled []ObservationError
	observer := New(Config{
		Exporter: exporter,
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})

	err := observer.Flush(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("Flush() error = %v, want cause", err)
	}
	var obsErr ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("Flush() error = %v, want ObservationError", err)
	}
	if obsErr.Operation != "flush" || obsErr.Classification != "exporter_failure" || !obsErr.Retryable {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
	if len(handled) != 1 || handled[0].Operation != "flush" {
		t.Fatalf("handled = %#v", handled)
	}
}

func TestObserverShutdownFailureIsRetained(t *testing.T) {
	cause := errors.New("shutdown failed")
	exporter := &testExporter{shutdownErr: cause}
	var handled []ObservationError
	observer := New(Config{
		Exporter: exporter,
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})

	err := observer.Shutdown(context.Background())
	if !errors.Is(err, cause) {
		t.Fatalf("Shutdown() error = %v, want cause", err)
	}
	var obsErr ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("Shutdown() error = %v, want ObservationError", err)
	}
	if obsErr.Operation != "shutdown" || obsErr.Classification != "exporter_failure" || !obsErr.Retryable {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
	if len(handled) != 1 || handled[0].Operation != "shutdown" {
		t.Fatalf("handled = %#v", handled)
	}

	if err := observer.Shutdown(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("second Shutdown() error = %v, want retained cause", err)
	}
	if exporter.shutdowns != 1 {
		t.Fatalf("second Shutdown delegated, count = %d", exporter.shutdowns)
	}
	if err := observer.Flush(context.Background()); !errors.Is(err, cause) {
		t.Fatalf("post-failed-shutdown Flush() error = %v, want retained cause", err)
	}
	if exporter.flushes != 0 {
		t.Fatalf("post-failed-shutdown Flush delegated, count = %d", exporter.flushes)
	}
}
