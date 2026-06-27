package exporter

import (
	"context"

	einoobs "github.com/mattsp1290/eino-obs"
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

type PublicAdapter interface {
	einoobs.Exporter
	ExportInternal(ctx context.Context, batch []model.Span) error
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
	OperationCounts       map[string]int64
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
	Type           string
	Code           string
	Message        string
	Retryable      bool
	Dropped        bool
}

func (e ObservationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Classification != "" {
		return e.Classification
	}
	if e.Operation != "" {
		return e.Operation
	}
	return "observation error"
}
