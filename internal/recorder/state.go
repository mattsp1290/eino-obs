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

type State struct {
	mu sync.Mutex

	epoch     int64
	redaction redaction.Options
	capacity  int
	failures  FailurePlan

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
		failures:         cloneFailurePlan(config.Failures),
		operationCounts:  map[string]int64{},
		errorHistorySize: 256,
	}
}

func (s *State) Record(ctx context.Context, span model.Span) error {
	epoch, _ := s.beginOperation("record")

	if err := ctx.Err(); err != nil {
		return s.recordErrorAtEpoch(epoch, "record", "canceled", err, true, false)
	}
	redacted, err := redaction.ApplySpan(span, s.redaction)
	if err != nil {
		return s.recordErrorAtEpoch(epoch, "redact", "redaction", err, false, true)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch {
		return nil
	}
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
	epoch, call := s.beginOperation("export")

	if err := ctx.Err(); err != nil {
		return s.recordErrorAtEpoch(epoch, "export", "canceled", err, true, false)
	}
	redacted := make([]model.Span, 0, len(batch))
	for _, span := range batch {
		item, err := redaction.ApplySpan(span, s.redaction)
		if err != nil {
			return s.recordErrorAtEpoch(epoch, "redact", "redaction", err, false, true)
		}
		redacted = append(redacted, item)
	}
	if failure := s.failure("export", call); failure != nil {
		return s.recordInjectedExportErrorAtEpoch(epoch, *failure, redacted)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch {
		return nil
	}
	if s.shutdown {
		obsErr := observationError("export", "exporter_closed", errors.New("fake exporter is shut down"), false, true)
		for _, span := range redacted {
			s.dropLocked(span, "shutdown", obsErr)
		}
		return obsErr
	}
	var exportErrs []error
	for _, span := range redacted {
		s.sequence++
		span = stampSpan(span, s.sequence)
		if s.shouldDropLocked() {
			obsErr := observationError("export", "capacity", errors.New("fake exporter capacity exceeded"), false, true)
			s.dropLocked(span, "capacity", obsErr)
			exportErrs = append(exportErrs, obsErr)
			continue
		}
		s.recorded = append(s.recorded, span)
	}
	return errors.Join(exportErrs...)
}

func (s *State) Flush(ctx context.Context) error {
	s.mu.Lock()
	epoch := s.epoch
	s.flushCount++
	call := s.incrementLocked("flush")
	shutdownErr := cloneErrorPtr(s.lastShutdownErr)
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		return s.recordFlushErrorAtEpoch(epoch, "canceled", err, true)
	}
	if failure := s.failure("flush", call); failure != nil {
		return s.recordInjectedErrorAtEpoch(epoch, "flush", *failure, true)
	}
	if shutdownErr != nil {
		return *shutdownErr
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch {
		return nil
	}
	if len(s.pending) > 0 {
		s.exportCount++
		exportCall := s.incrementLocked("export")
		if failure := cloneMatchingFailure(s.failures.Export, s.failures.ExportRules, exportCall); failure != nil {
			obsErr := injectedObservationError("export", *failure)
			if !obsErr.Retryable || obsErr.Dropped {
				for _, span := range s.pending {
					s.dropLocked(span, "injected_failure", obsErr)
				}
				s.pending = nil
			}
			s.lastFlushError = &obsErr
			s.rememberErrorLocked(obsErr)
			s.dirty = true
			return obsErr
		}
		var exportErrs []error
		var firstErr *exporter.ObservationError
		for _, span := range s.pending {
			if s.shouldDropLocked() {
				obsErr := observationError("flush", "capacity", errors.New("fake exporter capacity exceeded"), false, true)
				s.dropLocked(span, "capacity", obsErr)
				exportErrs = append(exportErrs, obsErr)
				if firstErr == nil {
					errCopy := obsErr
					firstErr = &errCopy
				}
				continue
			}
			s.recorded = append(s.recorded, span.Clone())
		}
		s.pending = nil
		if len(exportErrs) > 0 {
			s.lastFlushError = firstErr
			s.dirty = true
			return errors.Join(exportErrs...)
		}
		s.dirty = false
		s.lastFlushError = nil
		return errors.Join(exportErrs...)
	}
	s.pending = nil
	s.dirty = false
	s.lastFlushError = nil
	return nil
}

func (s *State) Shutdown(ctx context.Context) error {
	s.mu.Lock()
	epoch := s.epoch
	s.shutdownCount++
	call := s.incrementLocked("shutdown")
	alreadyShutdown := s.shutdown
	if !s.shutdown {
		s.shutdown = true
	}
	if alreadyShutdown && s.lastShutdownErr == nil {
		s.mu.Unlock()
		return nil
	}
	s.mu.Unlock()

	if err := ctx.Err(); err != nil {
		obsErr := observationError("shutdown", "canceled", err, true, false)
		s.mu.Lock()
		defer s.mu.Unlock()
		if epoch != s.epoch {
			return nil
		}
		s.lastShutdownErr = &obsErr
		s.rememberErrorLocked(obsErr)
		s.dirty = true
		return obsErr
	}
	if failure := s.failure("shutdown", call); failure != nil {
		return s.recordInjectedErrorAtEpoch(epoch, "shutdown", *failure, false)
	}

	s.mu.Lock()
	if epoch != s.epoch {
		s.mu.Unlock()
		return nil
	}
	s.shutdown = true
	s.pending = nil
	s.dirty = false
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
	s.epoch++
	s.shutdown = false
	s.droppedErrHist = 0
}

func (s *State) failure(operation string, call int64) *Failure {
	s.mu.Lock()
	defer s.mu.Unlock()
	switch operation {
	case "export":
		return cloneMatchingFailure(s.failures.Export, s.failures.ExportRules, call)
	case "flush":
		return cloneMatchingFailure(s.failures.Flush, s.failures.FlushRules, call)
	case "shutdown":
		return cloneMatchingFailure(s.failures.Shutdown, s.failures.ShutdownRules, call)
	default:
		return nil
	}
}

func cloneMatchingFailure(failure *Failure, rules []Failure, call int64) *Failure {
	for _, rule := range rules {
		if rule.AtCall <= 0 || rule.AtCall == call {
			out := rule
			return &out
		}
	}
	if failure == nil || (failure.AtCall > 0 && failure.AtCall != call) {
		return nil
	}
	return cloneFailurePtr(failure)
}

func (s *State) beginOperation(operation string) (int64, int64) {
	s.mu.Lock()
	defer s.mu.Unlock()
	epoch := s.epoch
	switch operation {
	case "record":
		s.recordCount++
	case "export":
		s.exportCount++
	}
	call := s.incrementLocked(operation)
	return epoch, call
}

func (s *State) recordFlushErrorAtEpoch(epoch int64, classification string, err error, retryable bool) error {
	obsErr := observationError("flush", classification, err, retryable, false)
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch {
		return nil
	}
	s.lastFlushError = &obsErr
	s.rememberErrorLocked(obsErr)
	s.dirty = true
	return obsErr
}

func (s *State) recordInjectedErrorAtEpoch(epoch int64, operation string, failure Failure, flush bool) error {
	obsErr := injectedObservationError(operation, failure)
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch {
		return nil
	}
	if flush {
		s.lastFlushError = &obsErr
	}
	if operation == "shutdown" {
		s.lastShutdownErr = &obsErr
	}
	s.rememberErrorLocked(obsErr)
	s.dirty = true
	return obsErr
}

func (s *State) recordInjectedExportErrorAtEpoch(epoch int64, failure Failure, spans []model.Span) error {
	obsErr := injectedObservationError("export", failure)
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch {
		return nil
	}
	if obsErr.Retryable && !obsErr.Dropped {
		for _, span := range spans {
			s.sequence++
			s.pending = append(s.pending, stampSpan(span, s.sequence))
		}
	} else {
		for _, span := range spans {
			s.sequence++
			s.dropLocked(stampSpan(span, s.sequence), "injected_failure", obsErr)
		}
	}
	s.rememberErrorLocked(obsErr)
	s.dirty = true
	return obsErr
}

func (s *State) recordErrorAtEpoch(epoch int64, operation string, classification string, err error, retryable bool, dropped bool) error {
	obsErr := observationError(operation, classification, err, retryable, dropped)
	s.mu.Lock()
	defer s.mu.Unlock()
	if epoch != s.epoch {
		return nil
	}
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

func (s *State) incrementLocked(operation string) int64 {
	if s.operationCounts == nil {
		s.operationCounts = map[string]int64{}
	}
	s.operationCounts[operation]++
	return s.operationCounts[operation]
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

func injectedObservationError(operation string, failure Failure) exporter.ObservationError {
	classification := failure.Classification
	if classification == "" {
		classification = "injected_failure"
	}
	message := failure.Message
	if message == "" {
		message = "fake recorder " + operation + " failure"
	}
	return exporter.ObservationError{
		Operation:      operation,
		Classification: classification,
		Type:           "error",
		Message:        message,
		Retryable:      failure.Retryable,
		Dropped:        failure.Dropped,
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

func cloneFailurePlan(plan FailurePlan) FailurePlan {
	return FailurePlan{
		Export:        cloneFailurePtr(plan.Export),
		Flush:         cloneFailurePtr(plan.Flush),
		Shutdown:      cloneFailurePtr(plan.Shutdown),
		ExportRules:   cloneFailures(plan.ExportRules),
		FlushRules:    cloneFailures(plan.FlushRules),
		ShutdownRules: cloneFailures(plan.ShutdownRules),
	}
}

func cloneFailurePtr(failure *Failure) *Failure {
	if failure == nil {
		return nil
	}
	out := *failure
	return &out
}

func cloneFailures(failures []Failure) []Failure {
	if failures == nil {
		return nil
	}
	out := make([]Failure, len(failures))
	copy(out, failures)
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

func boolPtr(value bool) *bool {
	return &value
}
