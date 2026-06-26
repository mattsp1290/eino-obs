package exporter

import (
	"context"

	"github.com/mattsp1290/eino-obs/internal/model"
)

type Recorder interface {
	Record(ctx context.Context, span model.Span) error
	Flush(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type Exporter interface {
	Export(ctx context.Context, batch []model.Span) error
	Flush(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type Snapshotter interface {
	Snapshot() Snapshot
}

type Snapshot struct {
	Recorded              []model.Span
	Pending               []model.Span
	Dropped               []DroppedObservation
	Errors                []ObservationError
	LastError             *ObservationError
	DroppedErrorHistory   int
	Dirty                 bool
	RecordCount           int64
	ExportCount           int64
	FlushCount            int64
	ShutdownCount         int64
	CredentialValidations int64
	ErrorHandlerCount     int64
	LastFlushError        *ObservationError
	LastShutdownError     *ObservationError
}

type DroppedObservation struct {
	Span   model.Span
	Reason string
	Error  ObservationError
}

type ObservationError struct {
	Operation      string
	Classification string
	Err            error
	Retryable      bool
	Dropped        bool
}

func (e ObservationError) Error() string {
	if e.Err != nil {
		return e.Err.Error()
	}
	if e.Classification != "" {
		return e.Classification
	}
	if e.Operation != "" {
		return e.Operation
	}
	return "observation error"
}

func (e ObservationError) Unwrap() error {
	return e.Err
}
