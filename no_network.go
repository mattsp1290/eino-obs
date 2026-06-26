package einoobs

import (
	"context"
	"sync"

	"github.com/mattsp1290/eino-obs/internal/model"
	"github.com/mattsp1290/eino-obs/internal/redaction"
)

type NoNetworkSnapshot struct {
	Observations  []Observation
	Dirty         bool
	ExportCount   int64
	FlushCount    int64
	ShutdownCount int64
}

type NoNetworkExporter struct {
	mu        sync.Mutex
	redaction RedactionOptions

	observations  []Observation
	dirty         bool
	exportCount   int64
	flushCount    int64
	shutdownCount int64
	shutdown      bool
}

func NewNoNetworkExporter(redaction RedactionOptions) *NoNetworkExporter {
	return &NoNetworkExporter{redaction: redaction}
}

func (e *NoNetworkExporter) Export(ctx context.Context, batch []Observation) error {
	if err := ctx.Err(); err != nil {
		return ObservationError{Operation: "export", Classification: "canceled", Err: err, Retryable: true}
	}
	redacted := make([]Observation, 0, len(batch))
	for _, observation := range batch {
		item, err := redactPublicObservation(observation, e.redaction)
		if err != nil {
			return normalizeObservationError("redact", "redaction", err, false, true)
		}
		redacted = append(redacted, item)
	}

	e.mu.Lock()
	defer e.mu.Unlock()
	e.exportCount++
	if e.shutdown {
		return ObservationError{Operation: "export", Classification: "exporter_closed", Retryable: false, Dropped: true}
	}
	if len(redacted) > 0 {
		e.dirty = true
	}
	e.observations = append(e.observations, redacted...)
	return nil
}

func (e *NoNetworkExporter) Flush(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return ObservationError{Operation: "flush", Classification: "canceled", Err: err, Retryable: true}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	e.flushCount++
	e.dirty = false
	return nil
}

func (e *NoNetworkExporter) Shutdown(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return ObservationError{Operation: "shutdown", Classification: "canceled", Err: err, Retryable: true}
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.shutdown {
		e.shutdownCount++
		e.shutdown = true
		e.dirty = false
	}
	return nil
}

func (e *NoNetworkExporter) Snapshot() NoNetworkSnapshot {
	e.mu.Lock()
	defer e.mu.Unlock()
	return NoNetworkSnapshot{
		Observations:  cloneObservationSlice(e.observations),
		Dirty:         e.dirty,
		ExportCount:   e.exportCount,
		FlushCount:    e.flushCount,
		ShutdownCount: e.shutdownCount,
	}
}

func (e *NoNetworkExporter) Reset() {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.observations = nil
	e.dirty = false
	e.exportCount = 0
	e.flushCount = 0
	e.shutdownCount = 0
	e.shutdown = false
}

func WithNoNetwork() Option {
	return func(config *Config) error {
		config.Exporter = noNetworkMarker{}
		return nil
	}
}

func redactPublicObservation(observation Observation, opts RedactionOptions) (Observation, error) {
	span, err := publicObservationToSpan(observation)
	if err != nil {
		return Observation{}, err
	}
	redacted, err := redaction.ApplySpan(span, redaction.Options{
		CaptureInputSummary:  opts.CaptureInputSummary,
		CaptureOutputSummary: opts.CaptureOutputSummary,
		MaxSummaryBytes:      opts.MaxSummaryBytes,
	})
	if err != nil {
		return Observation{}, err
	}
	return spanToPublicObservation(redacted), nil
}

func publicObservationToSpan(observation Observation) (model.Span, error) {
	span := model.NewSpan(
		model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
		model.SpanKind(observation.Kind),
		observation.Name,
		observation.Timestamp,
	)
	span.Status = model.Status(observation.Status)
	if observation.DurationKnown {
		span.EndTime = observation.Timestamp.Add(observation.Duration).UTC()
	}
	span.Attributes = model.Attributes(cloneObservationAttributes(observation.Attributes))
	span.Redaction = publicRedactionToModel(observation.Redaction)
	if observation.Error != nil {
		span.Error = &model.ObservationError{
			Operation:      observation.Error.Operation,
			Classification: observation.Error.Classification,
			Message:        observation.Error.Error(),
			Retryable:      boolPtr(observation.Error.Retryable),
			Dropped:        boolPtr(observation.Error.Dropped),
		}
	}
	for _, event := range observation.Events {
		span.Events = append(span.Events, publicObservationToEvent(event))
	}
	return span, nil
}

func publicObservationToEvent(observation Observation) model.Event {
	name := observation.Name
	if name == "" {
		name = observation.Kind
	}
	event := model.NewEvent(
		model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
		model.EventName(name),
		observation.Timestamp,
	)
	event.Status = model.Status(observation.Status)
	event.Attributes = model.Attributes(cloneObservationAttributes(observation.Attributes))
	event.Redaction = publicRedactionToModel(observation.Redaction)
	if observation.Error != nil {
		event.Error = &model.ObservationError{
			Operation:      observation.Error.Operation,
			Classification: observation.Error.Classification,
			Message:        observation.Error.Error(),
			Retryable:      boolPtr(observation.Error.Retryable),
			Dropped:        boolPtr(observation.Error.Dropped),
		}
	}
	return event
}

func spanToPublicObservation(span model.Span) Observation {
	out := Observation{
		ID:         span.Identity.ID,
		ParentID:   span.Identity.ParentID,
		TraceID:    span.Identity.TraceID,
		Kind:       string(span.Kind),
		Name:       span.Name,
		Status:     string(span.Status),
		Timestamp:  span.StartTime,
		Attributes: cloneModelAttributes(span.Attributes),
		Redaction:  modelRedactionToPublic(span.Redaction),
	}
	if !span.EndTime.IsZero() {
		out.Duration = span.EndTime.Sub(span.StartTime)
		out.DurationKnown = true
	}
	if span.Error != nil {
		out.Error = modelErrorToPublic(span.Error)
	}
	for _, event := range span.Events {
		out.Events = append(out.Events, eventToPublicObservation(event))
	}
	return out
}

func eventToPublicObservation(event model.Event) Observation {
	out := Observation{
		ID:         event.Identity.ID,
		ParentID:   event.Identity.ParentID,
		TraceID:    event.Identity.TraceID,
		Kind:       string(event.Name),
		Name:       string(event.Name),
		Status:     string(event.Status),
		Timestamp:  event.Timestamp,
		Attributes: cloneModelAttributes(event.Attributes),
		Redaction:  modelRedactionToPublic(event.Redaction),
	}
	if event.Error != nil {
		out.Error = modelErrorToPublic(event.Error)
	}
	return out
}

func publicRedactionToModel(records []RedactionRecord) []model.RedactionRecord {
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

func modelRedactionToPublic(records []model.RedactionRecord) []RedactionRecord {
	if records == nil {
		return nil
	}
	out := make([]RedactionRecord, len(records))
	for i, record := range records {
		out[i] = RedactionRecord{
			FieldPath:     record.FieldPath,
			Reason:        record.Reason,
			OriginalBytes: record.OriginalBytes,
			RetainedBytes: record.RetainedBytes,
		}
	}
	return out
}

func cloneModelAttributes(attrs model.Attributes) map[string]any {
	return map[string]any(model.CloneAttributes(attrs))
}

func modelErrorToPublic(err *model.ObservationError) *ObservationError {
	if err == nil {
		return nil
	}
	out := &ObservationError{
		Operation:      err.Operation,
		Classification: err.Classification,
		Err:            nil,
	}
	if err.Retryable != nil {
		out.Retryable = *err.Retryable
	}
	if err.Dropped != nil {
		out.Dropped = *err.Dropped
	}
	return out
}

func cloneObservationSlice(observations []Observation) []Observation {
	if observations == nil {
		return nil
	}
	out := make([]Observation, len(observations))
	for i, observation := range observations {
		out[i] = observation.Clone()
	}
	return out
}

func boolPtr(value bool) *bool {
	return &value
}

type noNetworkMarker struct{}

func (noNetworkMarker) Export(context.Context, []Observation) error {
	return nil
}

func (noNetworkMarker) Flush(context.Context) error {
	return nil
}

func (noNetworkMarker) Shutdown(context.Context) error {
	return nil
}
