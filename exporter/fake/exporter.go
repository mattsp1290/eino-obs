package fake

import (
	"context"

	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/internal/model"
	internalrecorder "github.com/mattsp1290/eino-obs/internal/recorder"
	"github.com/mattsp1290/eino-obs/internal/redaction"
	"github.com/mattsp1290/eino-obs/recorder"
)

type Snapshot = recorder.Snapshot

type Config struct {
	Redaction einoobs.RedactionOptions
	Capacity  int
}

type Exporter struct {
	state *internalrecorder.State
}

func New(config Config) *Exporter {
	return &Exporter{state: internalrecorder.New(internalrecorder.Config{
		Redaction: redaction.Options{
			CaptureInputSummary:  config.Redaction.CaptureInputSummary,
			CaptureOutputSummary: config.Redaction.CaptureOutputSummary,
			MaxSummaryBytes:      config.Redaction.MaxSummaryBytes,
		},
		Capacity: config.Capacity,
	})}
}

func (e *Exporter) Export(ctx context.Context, batch []einoobs.Observation) error {
	spans := make([]model.Span, 0, len(batch))
	for _, obs := range batch {
		spans = append(spans, internalrecorder.PublicObservationToSpan(obs))
	}
	return e.state.Export(ctx, spans)
}

func (e *Exporter) Flush(ctx context.Context) error {
	return e.state.Flush(ctx)
}

func (e *Exporter) Shutdown(ctx context.Context) error {
	return e.state.Shutdown(ctx)
}

func (e *Exporter) Snapshot() Snapshot {
	return recorder.SnapshotFromInternal(e.state.Snapshot())
}

func (e *Exporter) Reset() {
	e.state.Reset()
}

var _ einoobs.Exporter = (*Exporter)(nil)
