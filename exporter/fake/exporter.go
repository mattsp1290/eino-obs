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
	Failures  FailurePlan
}

type FailurePlan struct {
	Export        *Failure
	Flush         *Failure
	Shutdown      *Failure
	ExportRules   []Failure
	FlushRules    []Failure
	ShutdownRules []Failure
}

type Failure struct {
	AtCall         int64
	Classification string
	Message        string
	Retryable      bool
	Dropped        bool
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
		Failures: internalrecorder.FailurePlan{
			Export:        internalFailure(config.Failures.Export),
			Flush:         internalFailure(config.Failures.Flush),
			Shutdown:      internalFailure(config.Failures.Shutdown),
			ExportRules:   internalFailures(config.Failures.ExportRules),
			FlushRules:    internalFailures(config.Failures.FlushRules),
			ShutdownRules: internalFailures(config.Failures.ShutdownRules),
		},
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

func internalFailure(failure *Failure) *internalrecorder.Failure {
	if failure == nil {
		return nil
	}
	return &internalrecorder.Failure{
		AtCall:         failure.AtCall,
		Classification: failure.Classification,
		Message:        failure.Message,
		Retryable:      failure.Retryable,
		Dropped:        failure.Dropped,
	}
}

func internalFailures(failures []Failure) []internalrecorder.Failure {
	if failures == nil {
		return nil
	}
	out := make([]internalrecorder.Failure, len(failures))
	for i := range failures {
		out[i] = internalrecorder.Failure{
			AtCall:         failures[i].AtCall,
			Classification: failures[i].Classification,
			Message:        failures[i].Message,
			Retryable:      failures[i].Retryable,
			Dropped:        failures[i].Dropped,
		}
	}
	return out
}
