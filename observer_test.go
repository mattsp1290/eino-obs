package einoobs

import (
	"context"
	"errors"
	"testing"
)

func TestNewAppliesOptionsAndReturnsDefensiveConfig(t *testing.T) {
	exporter := &testExporter{}
	observer, err := New(
		Config{Service: "initial"},
		WithService("svc"),
		WithEnv("dev"),
		WithVersion("v1"),
		WithRedaction(RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 8}),
		WithExporter(exporter),
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

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
	_, err := New(Config{Redaction: RedactionOptions{MaxSummaryBytes: -1}})
	if err == nil {
		t.Fatalf("New() error = nil")
	}
	if !errors.Is(err, ErrInvalidConfig) {
		t.Fatalf("New() error = %v, want ErrInvalidConfig", err)
	}
}

func TestObserverFlushShutdownNilExporterAreNoNetworkNoops(t *testing.T) {
	observer, err := New(Config{Service: "svc"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := observer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestObserverFlushShutdownDelegateToExporter(t *testing.T) {
	exporter := &testExporter{}
	observer, err := New(Config{Exporter: exporter})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := observer.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if exporter.flushes != 1 || exporter.shutdowns != 1 {
		t.Fatalf("counts = flush:%d shutdown:%d", exporter.flushes, exporter.shutdowns)
	}
}
