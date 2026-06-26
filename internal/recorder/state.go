package recorder

import (
	"context"
	"errors"
	"sync"

	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/internal/exporter"
	"github.com/mattsp1290/eino-obs/internal/model"
	"github.com/mattsp1290/eino-obs/internal/redaction"
)

type Config struct {
	Redaction redaction.Options
	Capacity  int
}

type State struct {
	mu sync.Mutex

	redaction redaction.Options
	capacity  int

	recorded []model.Span
	pending  []model.Span
	dropped  []exporter.DroppedObservation
	errors   []exporter.ObservationError

	dirty            bool
	recordCount      int64
	exportCount      int64
	flushCount       int64
	shutdownCount    int64
	operationCounts  map[string]int64
	lastError        *exporter.ObservationError
	lastFlushError   *exporter.ObservationError
	lastShutdownErr  *exporter.ObservationError
	sequence         int64
	shutdown         bool
	droppedErrHist   int
	errorHistorySize int
}

func New(config Config) *State {
	return &State{
		redaction:        config.Redaction,
		capacity:         config.Capacity,
		operationCounts:  map[string]int64{},
		errorHistorySize: 256,
	}
}

func (s *State) Record(ctx context.Context, span model.Span) error {
	s.mu.Lock()
	s.recordCount++
	s.incrementLocked("record")
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return s.recordError("record", "canceled", err, true, false)
	}
	redacted, err := redaction.ApplySpan(span, s.redaction)
	if err != nil {
		return s.recordError("redact", "redaction", err, false, true)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.sequence++
	redacted = stampSpan(redacted, s.sequence)
	if s.shouldDropLocked() {
		obsErr := observationError("record", "capacity", errors.New("fake recorder capacity exceeded"), false, true)
		s.dropLocked(redacted, "capacity", obsErr)
		return obsErr
	}
	s.recorded = append(s.recorded, redacted)
	return nil
}

func (s *State) Export(ctx context.Context, batch []model.Span) error {
	s.mu.Lock()
	s.exportCount++
	s.incrementLocked("export")
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return s.recordError("export", "canceled", err, true, false)
	}
	redacted := make([]model.Span, 0, len(batch))
	for _, span := range batch {
		item, err := redaction.ApplySpan(span, s.redaction)
		if err != nil {
			return s.recordError("redact", "redaction", err, false, true)
		}
		redacted = append(redacted, item)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.shutdown {
		obsErr := observationError("export", "exporter_closed", errors.New("fake exporter is shut down"), false, true)
		for _, span := range redacted {
			s.dropLocked(span, "shutdown", obsErr)
		}
		return obsErr
	}
	for _, span := range redacted {
		s.sequence++
		span = stampSpan(span, s.sequence)
		if s.shouldDropLocked() {
			obsErr := observationError("export", "capacity", errors.New("fake exporter capacity exceeded"), false, true)
			s.dropLocked(span, "capacity", obsErr)
			continue
		}
		s.recorded = append(s.recorded, span)
	}
	return nil
}

func (s *State) Flush(ctx context.Context) error {
	s.mu.Lock()
	s.flushCount++
	s.incrementLocked("flush")
	shutdownErr := cloneErrorPtr(s.lastShutdownErr)
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return s.recordFlushError("canceled", err, true)
	}
	if shutdownErr != nil {
		return *shutdownErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.pending = nil
	s.dirty = false
	s.lastFlushError = nil
	return nil
}

func (s *State) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	s.shutdownCount++
	s.incrementLocked("shutdown")
	if s.shutdown {
		err := cloneErrorPtr(s.lastShutdownErr)
		s.mu.Unlock()
		if err != nil {
			return *err
		}
		return nil
	}
	s.shutdown = true
	s.mu.Unlock()

	if err := s.Flush(ctx); err != nil {
		obsErr := toObservationError("shutdown", err, true, false)
		s.mu.Lock()
		s.lastShutdownErr = &obsErr
		s.rememberErrorLocked(obsErr)
		s.dirty = true
		s.mu.Unlock()
		return obsErr
	}

	s.mu.Lock()
	s.lastShutdownErr = nil
	s.mu.Unlock()
	return nil
}

func (s *State) Snapshot() exporter.Snapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	return exporter.Snapshot{
		Recorded:              cloneSpans(s.recorded),
		Pending:               cloneSpans(s.pending),
		Dropped:               cloneDropped(s.dropped),
		Errors:                cloneErrors(s.errors),
		LastError:             cloneErrorPtr(s.lastError),
		DroppedErrorHistory:   s.droppedErrHist,
		Dirty:                 s.dirty,
		RecordCount:           s.recordCount,
		ExportCount:           s.exportCount,
		FlushCount:            s.flushCount,
		ShutdownCount:         s.shutdownCount,
		CredentialValidations: s.operationCounts["credential_validation"],
		ErrorHandlerCount:     s.operationCounts["error_handler"],
		OperationCounts:       cloneCounts(s.operationCounts),
		LastFlushError:        cloneErrorPtr(s.lastFlushError),
		LastShutdownError:     cloneErrorPtr(s.lastShutdownErr),
	}
}

func (s *State) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.recorded = nil
	s.pending = nil
	s.dropped = nil
	s.errors = nil
	s.dirty = false
	s.recordCount = 0
	s.exportCount = 0
	s.flushCount = 0
	s.shutdownCount = 0
	s.operationCounts = map[string]int64{}
	s.lastError = nil
	s.lastFlushError = nil
	s.lastShutdownErr = nil
	s.sequence = 0
	s.shutdown = false
	s.droppedErrHist = 0
}

func (s *State) recordFlushError(classification string, err error, retryable bool) error {
	obsErr := observationError("flush", classification, err, retryable, false)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastFlushError = &obsErr
	s.rememberErrorLocked(obsErr)
	s.dirty = true
	return obsErr
}

func (s *State) recordError(operation string, classification string, err error, retryable bool, dropped bool) error {
	obsErr := observationError(operation, classification, err, retryable, dropped)
	s.mu.Lock()
	defer s.mu.Unlock()
	s.rememberErrorLocked(obsErr)
	s.dirty = true
	return obsErr
}

func (s *State) shouldDropLocked() bool {
	return s.capacity > 0 && len(s.recorded) >= s.capacity
}

func (s *State) dropLocked(span model.Span, reason string, obsErr exporter.ObservationError) {
	s.dropped = append(s.dropped, exporter.DroppedObservation{Span: span.Clone(), Reason: reason, Error: obsErr})
	s.rememberErrorLocked(obsErr)
	s.dirty = true
}

func (s *State) rememberErrorLocked(err exporter.ObservationError) {
	errCopy := err
	s.lastError = &errCopy
	if s.errorHistorySize > 0 && len(s.errors) >= s.errorHistorySize {
		copy(s.errors, s.errors[1:])
		s.errors[len(s.errors)-1] = err
		s.droppedErrHist++
		return
	}
	s.errors = append(s.errors, err)
}

func (s *State) incrementLocked(operation string) {
	if s.operationCounts == nil {
		s.operationCounts = map[string]int64{}
	}
	s.operationCounts[operation]++
}

func stampSpan(span model.Span, sequence int64) model.Span {
	span = span.Clone()
	span.SetAttr("sequence", sequence)
	for i := range span.Events {
		span.Events[i].SetAttr("span_event_sequence", int64(i+1))
	}
	return span
}

func observationError(operation string, classification string, err error, retryable bool, dropped bool) exporter.ObservationError {
	return exporter.ObservationError{
		Operation:      operation,
		Classification: classification,
		Type:           errorType(err),
		Message:        safeErrorMessage(err),
		Retryable:      retryable,
		Dropped:        dropped,
	}
}

func toObservationError(operation string, err error, retryable bool, dropped bool) exporter.ObservationError {
	var obsErr exporter.ObservationError
	if errors.As(err, &obsErr) {
		return obsErr
	}
	return observationError(operation, "unknown", err, retryable, dropped)
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	return "error"
}

func safeErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func PublicObservationToSpan(obs einoobs.Observation) model.Span {
	span := model.NewSpan(
		model.ObservationIdentity{ID: obs.ID, ParentID: obs.ParentID, TraceID: obs.TraceID},
		model.SpanKind(obs.Kind),
		obs.Name,
		obs.Timestamp,
	)
	span.Status = model.Status(obs.Status)
	if obs.DurationKnown {
		span.EndTime = obs.Timestamp.Add(obs.Duration).UTC()
	}
	span.Attributes = publicAttributesToModel(obs.Attributes)
	span.Redaction = publicRedactionToModel(obs.Redaction)
	if obs.Error != nil {
		span.Error = &model.ObservationError{
			Operation:      obs.Error.Operation,
			Classification: obs.Error.Classification,
			Message:        obs.Error.Error(),
			Retryable:      boolPtr(obs.Error.Retryable),
			Dropped:        boolPtr(obs.Error.Dropped),
		}
	}
	for _, event := range obs.Events {
		span.Events = append(span.Events, publicObservationToEvent(event))
	}
	return span
}

func publicObservationToEvent(obs einoobs.Observation) model.Event {
	event := model.NewEvent(
		model.ObservationIdentity{ID: obs.ID, ParentID: obs.ParentID, TraceID: obs.TraceID},
		model.EventName(obs.Kind),
		obs.Timestamp,
	)
	event.Status = model.Status(obs.Status)
	event.Attributes = publicAttributesToModel(obs.Attributes)
	event.Redaction = publicRedactionToModel(obs.Redaction)
	return event
}

func publicAttributesToModel(attrs map[string]any) model.Attributes {
	if attrs == nil {
		return nil
	}
	out := make(model.Attributes, len(attrs))
	for key, value := range attrs {
		out[key] = value
	}
	return model.CloneAttributes(out)
}

func publicRedactionToModel(records []einoobs.RedactionRecord) []model.RedactionRecord {
	if records == nil {
		return nil
	}
	out := make([]model.RedactionRecord, len(records))
	for i, record := range records {
		out[i] = model.RedactionRecord{
			FieldPath:     record.FieldPath,
			Reason:        record.Reason,
			OriginalBytes: record.OriginalBytes,
			RetainedBytes: record.RetainedBytes,
		}
	}
	return out
}

func cloneSpans(spans []model.Span) []model.Span {
	if spans == nil {
		return nil
	}
	out := make([]model.Span, len(spans))
	for i, span := range spans {
		out[i] = span.Clone()
	}
	return out
}

func cloneDropped(dropped []exporter.DroppedObservation) []exporter.DroppedObservation {
	if dropped == nil {
		return nil
	}
	out := make([]exporter.DroppedObservation, len(dropped))
	for i, item := range dropped {
		out[i] = exporter.DroppedObservation{
			Span:   item.Span.Clone(),
			Reason: item.Reason,
			Error:  item.Error,
		}
	}
	return out
}

func cloneErrors(errors []exporter.ObservationError) []exporter.ObservationError {
	if errors == nil {
		return nil
	}
	out := make([]exporter.ObservationError, len(errors))
	copy(out, errors)
	return out
}

func cloneErrorPtr(err *exporter.ObservationError) *exporter.ObservationError {
	if err == nil {
		return nil
	}
	out := *err
	return &out
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

func boolPtr(value bool) *bool {
	return &value
}
