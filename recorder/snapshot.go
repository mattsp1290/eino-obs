package recorder

import (
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/internal/exporter"
	"github.com/mattsp1290/eino-obs/internal/model"
)

type Snapshot struct {
	Recorded              []ObservationSnapshot
	Pending               []ObservationSnapshot
	Dropped               []DroppedObservationSnapshot
	Errors                []ObservationErrorSnapshot
	LastError             *ObservationErrorSnapshot
	DroppedErrorHistory   int
	Dirty                 bool
	RecordCount           int64
	ExportCount           int64
	FlushCount            int64
	ShutdownCount         int64
	CredentialValidations int64
	ErrorHandlerCount     int64
	OperationCounts       map[string]int64
	LastFlushError        *ObservationErrorSnapshot
	LastShutdownError     *ObservationErrorSnapshot
}

type ObservationSnapshot struct {
	ID         string
	ParentID   string
	TraceID    string
	Kind       string
	Name       string
	Status     string
	StartTime  time.Time
	EndTime    time.Time
	Attributes map[string]any
	Events     []EventSnapshot
	Redaction  []einoobs.RedactionRecord
	Error      *ObservationErrorSnapshot
}

type EventSnapshot struct {
	ID         string
	ParentID   string
	TraceID    string
	Name       string
	Category   string
	Status     string
	Timestamp  time.Time
	Attributes map[string]any
	Redaction  []einoobs.RedactionRecord
	Error      *ObservationErrorSnapshot
}

type DroppedObservationSnapshot struct {
	Observation ObservationSnapshot
	Reason      string
	Error       ObservationErrorSnapshot
}

type ObservationErrorSnapshot struct {
	Operation      string
	Classification string
	Type           string
	Message        string
	Retryable      bool
	Dropped        bool
}

func SnapshotFromInternal(snapshot exporter.Snapshot) Snapshot {
	return Snapshot{
		Recorded:              observationSnapshots(snapshot.Recorded),
		Pending:               observationSnapshots(snapshot.Pending),
		Dropped:               droppedSnapshots(snapshot.Dropped),
		Errors:                errorSnapshots(snapshot.Errors),
		LastError:             errorSnapshotPtr(snapshot.LastError),
		DroppedErrorHistory:   snapshot.DroppedErrorHistory,
		Dirty:                 snapshot.Dirty,
		RecordCount:           snapshot.RecordCount,
		ExportCount:           snapshot.ExportCount,
		FlushCount:            snapshot.FlushCount,
		ShutdownCount:         snapshot.ShutdownCount,
		CredentialValidations: snapshot.CredentialValidations,
		ErrorHandlerCount:     snapshot.ErrorHandlerCount,
		OperationCounts:       cloneCounts(snapshot.OperationCounts),
		LastFlushError:        errorSnapshotPtr(snapshot.LastFlushError),
		LastShutdownError:     errorSnapshotPtr(snapshot.LastShutdownError),
	}
}

func observationSnapshots(spans []model.Span) []ObservationSnapshot {
	if spans == nil {
		return nil
	}
	out := make([]ObservationSnapshot, len(spans))
	for i, span := range spans {
		out[i] = observationSnapshot(span)
	}
	return out
}

func observationSnapshot(span model.Span) ObservationSnapshot {
	return ObservationSnapshot{
		ID:         span.Identity.ID,
		ParentID:   span.Identity.ParentID,
		TraceID:    span.Identity.TraceID,
		Kind:       string(span.Kind),
		Name:       span.Name,
		Status:     string(span.Status),
		StartTime:  span.StartTime,
		EndTime:    span.EndTime,
		Attributes: model.CloneAttributes(span.Attributes),
		Events:     eventSnapshots(span.Events),
		Redaction:  redactionSnapshots(span.Redaction),
		Error:      modelErrorSnapshotPtr(span.Error),
	}
}

func eventSnapshots(events []model.Event) []EventSnapshot {
	if events == nil {
		return nil
	}
	out := make([]EventSnapshot, len(events))
	for i, event := range events {
		out[i] = EventSnapshot{
			ID:         event.Identity.ID,
			ParentID:   event.Identity.ParentID,
			TraceID:    event.Identity.TraceID,
			Name:       string(event.Name),
			Category:   event.Category,
			Status:     string(event.Status),
			Timestamp:  event.Timestamp,
			Attributes: model.CloneAttributes(event.Attributes),
			Redaction:  redactionSnapshots(event.Redaction),
			Error:      modelErrorSnapshotPtr(event.Error),
		}
	}
	return out
}

func droppedSnapshots(dropped []exporter.DroppedObservation) []DroppedObservationSnapshot {
	if dropped == nil {
		return nil
	}
	out := make([]DroppedObservationSnapshot, len(dropped))
	for i, item := range dropped {
		out[i] = DroppedObservationSnapshot{
			Observation: observationSnapshot(item.Span),
			Reason:      item.Reason,
			Error:       errorSnapshot(item.Error),
		}
	}
	return out
}

func errorSnapshots(errors []exporter.ObservationError) []ObservationErrorSnapshot {
	if errors == nil {
		return nil
	}
	out := make([]ObservationErrorSnapshot, len(errors))
	for i, err := range errors {
		out[i] = errorSnapshot(err)
	}
	return out
}

func errorSnapshotPtr(err *exporter.ObservationError) *ObservationErrorSnapshot {
	if err == nil {
		return nil
	}
	out := errorSnapshot(*err)
	return &out
}

func errorSnapshot(err exporter.ObservationError) ObservationErrorSnapshot {
	return ObservationErrorSnapshot{
		Operation:      err.Operation,
		Classification: err.Classification,
		Type:           err.Type,
		Message:        err.Message,
		Retryable:      err.Retryable,
		Dropped:        err.Dropped,
	}
}

func modelErrorSnapshotPtr(err *model.ObservationError) *ObservationErrorSnapshot {
	if err == nil {
		return nil
	}
	out := ObservationErrorSnapshot{
		Operation:      err.Operation,
		Classification: err.Classification,
		Type:           err.Type,
		Message:        err.Message,
	}
	if err.Retryable != nil {
		out.Retryable = *err.Retryable
	}
	if err.Dropped != nil {
		out.Dropped = *err.Dropped
	}
	return &out
}

func redactionSnapshots(records []model.RedactionRecord) []einoobs.RedactionRecord {
	if records == nil {
		return nil
	}
	out := make([]einoobs.RedactionRecord, len(records))
	for i, record := range records {
		out[i] = einoobs.RedactionRecord{
			FieldPath:     record.FieldPath,
			Reason:        record.Reason,
			OriginalBytes: record.OriginalBytes,
			RetainedBytes: record.RetainedBytes,
		}
	}
	return out
}

func cloneCounts(counts map[string]int64) map[string]int64 {
	if counts == nil {
		return nil
	}
	out := make(map[string]int64, len(counts))
	for key, value := range counts {
		out[key] = value
	}
	return out
}
