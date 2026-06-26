package recorder

import (
	"context"

	einoobs "github.com/mattsp1290/eino-obs"
	internalrecorder "github.com/mattsp1290/eino-obs/internal/recorder"
	"github.com/mattsp1290/eino-obs/internal/redaction"
)

type Config struct {
	Redaction einoobs.RedactionOptions
	Capacity  int
}

type Recorder struct {
	state *internalrecorder.State
}

func New(config Config) *Recorder {
	return &Recorder{state: internalrecorder.New(internalrecorder.Config{
		Redaction: redaction.Options{
			CaptureInputSummary:  config.Redaction.CaptureInputSummary,
			CaptureOutputSummary: config.Redaction.CaptureOutputSummary,
			MaxSummaryBytes:      config.Redaction.MaxSummaryBytes,
		},
		Capacity: config.Capacity,
	})}
}

func (r *Recorder) Record(ctx context.Context, observation einoobs.Observation) error {
	return r.state.Record(ctx, internalrecorder.PublicObservationToSpan(observation))
}

func (r *Recorder) Flush(ctx context.Context) error {
	return r.state.Flush(ctx)
}

func (r *Recorder) Shutdown(ctx context.Context) error {
	return r.state.Shutdown(ctx)
}

func (r *Recorder) Snapshot() Snapshot {
	return SnapshotFromInternal(r.state.Snapshot())
}

func (r *Recorder) Reset() {
	r.state.Reset()
}
