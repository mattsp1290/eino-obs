package model

import (
	"errors"
	"fmt"
	"time"
)

type SpanKind string

const (
	SpanKindSession        SpanKind = "session"
	SpanKindRun            SpanKind = "run"
	SpanKindModelCall      SpanKind = "model_call"
	SpanKindStream         SpanKind = "stream"
	SpanKindToolCall       SpanKind = "tool_call"
	SpanKindExportFlush    SpanKind = "export_flush"
	SpanKindExportShutdown SpanKind = "export_shutdown"
)

type Status string

const (
	StatusOK       Status = "ok"
	StatusError    Status = "error"
	StatusCanceled Status = "canceled"
)

type EventName string

const (
	EventStreamChunk     EventName = "stream.chunk"
	EventStreamFirstTok  EventName = "stream.first_token"
	EventToolRegistered  EventName = "tool.registered"
	EventToolMaterialize EventName = "tool.materialized"
	EventToolSettled     EventName = "tool.settled"
	EventRetry           EventName = "retry"
	EventCompaction      EventName = "compaction"
	EventInterrupt       EventName = "interrupt"
	EventResume          EventName = "resume"
	EventCancellation    EventName = "cancellation"
	EventError           EventName = "error"
	EventObservationErr  EventName = "observation.error"
	EventRedaction       EventName = "redaction.applied"
)

type AttributeValue interface {
	~string | ~bool | ~int | ~int64 | ~float64
}

type Attributes map[string]any

type ObservationIdentity struct {
	ID       string
	ParentID string
	TraceID  string
}

type RedactionRecord struct {
	FieldPath     string
	Reason        string
	OriginalBytes int
	RetainedBytes int
}

type ObservationError struct {
	Operation      string
	Type           string
	Message        string
	Classification string
	Retryable      bool
	Canceled       bool
	Dropped        bool
}

type Span struct {
	Identity  ObservationIdentity
	Kind      SpanKind
	Name      string
	Status    Status
	StartTime time.Time
	EndTime   time.Time

	Attributes Attributes
	Events     []Event
	Redaction  []RedactionRecord
	Error      *ObservationError
}

type Event struct {
	Identity  ObservationIdentity
	Name      EventName
	Category  string
	Status    Status
	Timestamp time.Time

	Attributes Attributes
	Redaction  []RedactionRecord
	Error      *ObservationError
}

func NewSpan(id ObservationIdentity, kind SpanKind, name string, start time.Time) Span {
	return Span{
		Identity:   id,
		Kind:       kind,
		Name:       name,
		Status:     StatusOK,
		StartTime:  start.UTC(),
		Attributes: Attributes{},
	}
}

func NewEvent(id ObservationIdentity, name EventName, at time.Time) Event {
	return Event{
		Identity:   id,
		Name:       name,
		Category:   eventCategory(name),
		Timestamp:  at.UTC(),
		Attributes: Attributes{},
	}
}

func (s Span) Duration() (time.Duration, bool) {
	if s.EndTime.IsZero() {
		return 0, false
	}
	return s.EndTime.Sub(s.StartTime), true
}

func (s Span) DurationMS() (int64, bool) {
	d, ok := s.Duration()
	if !ok {
		return 0, false
	}
	return int64(d / time.Millisecond), true
}

func (s *Span) End(end time.Time, status Status) {
	s.EndTime = end.UTC()
	s.Status = status
}

func (s *Span) AddEvent(event Event) {
	s.Events = append(s.Events, event)
}

func (s *Span) SetAttr(key string, value any) {
	if s.Attributes == nil {
		s.Attributes = Attributes{}
	}
	s.Attributes[key] = value
}

func (e *Event) SetAttr(key string, value any) {
	if e.Attributes == nil {
		e.Attributes = Attributes{}
	}
	e.Attributes[key] = value
}

func (s Span) Validate() error {
	var errs []error
	errs = append(errs, validateIdentity(s.Identity)...)
	if !validSpanKind(s.Kind) {
		errs = append(errs, fmt.Errorf("invalid span kind %q", s.Kind))
	}
	if s.Name == "" {
		errs = append(errs, errors.New("span name is required"))
	}
	if !validStatus(s.Status) {
		errs = append(errs, fmt.Errorf("invalid span status %q", s.Status))
	}
	if s.StartTime.IsZero() {
		errs = append(errs, errors.New("span start time is required"))
	}
	if !s.EndTime.IsZero() && s.EndTime.Before(s.StartTime) {
		errs = append(errs, errors.New("span end time is before start time"))
	}
	if s.Status == StatusError || s.Status == StatusCanceled {
		if s.Error == nil {
			errs = append(errs, errors.New("terminal error/canceled span requires error fields"))
		}
	}
	if s.Error != nil {
		errs = append(errs, s.Error.validate("span")...)
	}
	errs = append(errs, validateSpanRequiredAttributes(s)...)
	for i, event := range s.Events {
		if err := event.Validate(); err != nil {
			errs = append(errs, fmt.Errorf("event %d: %w", i, err))
		}
	}
	return errors.Join(errs...)
}

func (e Event) Validate() error {
	var errs []error
	errs = append(errs, validateIdentity(e.Identity)...)
	if !validEventName(e.Name) {
		errs = append(errs, fmt.Errorf("invalid event name %q", e.Name))
	}
	if e.Timestamp.IsZero() {
		errs = append(errs, errors.New("event timestamp is required"))
	}
	if e.Status != "" && !validStatus(e.Status) {
		errs = append(errs, fmt.Errorf("invalid event status %q", e.Status))
	}
	if e.Category == "" {
		errs = append(errs, errors.New("event category is required"))
	}
	if e.Error != nil {
		errs = append(errs, e.Error.validate("event")...)
	}
	errs = append(errs, validateEventRequiredAttributes(e)...)
	return errors.Join(errs...)
}

func (e ObservationError) validate(scope string) []error {
	var errs []error
	if e.Operation == "" {
		errs = append(errs, fmt.Errorf("%s error operation is required", scope))
	}
	if e.Classification == "" {
		errs = append(errs, fmt.Errorf("%s error classification is required", scope))
	}
	if e.Canceled && e.Retryable {
		errs = append(errs, fmt.Errorf("%s canceled error cannot be retryable", scope))
	}
	return errs
}

func validateIdentity(id ObservationIdentity) []error {
	var errs []error
	if id.ID == "" {
		errs = append(errs, errors.New("observation id is required"))
	}
	if id.TraceID == "" {
		errs = append(errs, errors.New("trace id is required"))
	}
	return errs
}

func validateSpanRequiredAttributes(span Span) []error {
	switch span.Kind {
	case SpanKindModelCall, SpanKindStream:
		return missingAttrs(span.Attributes, "genai.provider", "genai.model")
	case SpanKindToolCall:
		return missingAttrs(span.Attributes, "tool.name", "tool.call_id", "tool.kind", "tool.status")
	default:
		return nil
	}
}

func validateEventRequiredAttributes(event Event) []error {
	switch event.Name {
	case EventStreamChunk:
		return missingAttrs(event.Attributes, "stream.chunk.index")
	case EventStreamFirstTok:
		return missingAttrs(event.Attributes, "genai.latency.first_token_ms")
	case EventToolRegistered:
		return missingAttrs(event.Attributes, "tool.name", "tool.kind", "tool.status")
	case EventToolMaterialize:
		return missingAttrs(event.Attributes, "tool.name", "tool.call_id", "tool.kind", "tool.status")
	case EventToolSettled:
		return missingAttrs(event.Attributes, "tool.name", "tool.call_id", "tool.kind", "tool.status")
	case EventRetry:
		return missingAttrs(event.Attributes, "retry.attempt", "retry.reason")
	case EventCompaction:
		return missingAttrs(event.Attributes, "compaction.reason")
	case EventInterrupt:
		return missingAttrs(event.Attributes, "interrupt.reason")
	case EventResume:
		return missingAttrs(event.Attributes, "resume.reason")
	case EventCancellation:
		return missingAttrs(event.Attributes, "cancellation.reason", "error.canceled")
	case EventError:
		return missingAttrs(event.Attributes, "error.operation", "error.classification", "error.retryable")
	case EventObservationErr:
		return missingAttrs(event.Attributes, "error.operation", "error.classification", "error.retryable", "error.dropped")
	case EventRedaction:
		if len(event.Redaction) == 0 {
			return []error{errors.New("redaction.applied event requires redaction records")}
		}
		return nil
	default:
		return nil
	}
}

func missingAttrs(attrs Attributes, required ...string) []error {
	var errs []error
	for _, key := range required {
		if _, ok := attrs[key]; !ok {
			errs = append(errs, fmt.Errorf("required attribute %q is missing", key))
		}
	}
	return errs
}

func validSpanKind(kind SpanKind) bool {
	switch kind {
	case SpanKindSession, SpanKindRun, SpanKindModelCall, SpanKindStream,
		SpanKindToolCall, SpanKindExportFlush, SpanKindExportShutdown:
		return true
	default:
		return false
	}
}

func validStatus(status Status) bool {
	switch status {
	case StatusOK, StatusError, StatusCanceled:
		return true
	default:
		return false
	}
}

func validEventName(name EventName) bool {
	switch name {
	case EventStreamChunk, EventStreamFirstTok, EventToolRegistered,
		EventToolMaterialize, EventToolSettled, EventRetry, EventCompaction,
		EventInterrupt, EventResume, EventCancellation, EventError,
		EventObservationErr, EventRedaction:
		return true
	default:
		return false
	}
}

func eventCategory(name EventName) string {
	switch name {
	case EventStreamChunk, EventStreamFirstTok:
		return "stream"
	case EventToolRegistered, EventToolMaterialize, EventToolSettled:
		return "tool"
	case EventError, EventObservationErr:
		return "error"
	case EventRedaction:
		return "redaction"
	default:
		return "lifecycle"
	}
}
