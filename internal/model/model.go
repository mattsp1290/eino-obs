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
	Code           string
	Message        string
	Classification string
	Cause          error
	Retryable      *bool
	Canceled       *bool
	Dropped        *bool
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
	errs = append(errs, validateAttributes(s.Attributes)...)
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
	} else if s.StartTime.Location() != time.UTC {
		errs = append(errs, errors.New("span start time must be UTC"))
	}
	if !s.EndTime.IsZero() && s.EndTime.Before(s.StartTime) {
		errs = append(errs, errors.New("span end time is before start time"))
	}
	if !s.EndTime.IsZero() && s.EndTime.Location() != time.UTC {
		errs = append(errs, errors.New("span end time must be UTC"))
	}
	if s.Status == StatusError || s.Status == StatusCanceled {
		if s.Error == nil {
			errs = append(errs, errors.New("terminal error/canceled span requires error fields"))
		}
	}
	if s.Error != nil {
		errs = append(errs, s.Error.validate("span")...)
		errs = append(errs, validateTerminalErrorFlags("span", s.Status, *s.Error)...)
	}
	errs = append(errs, validateSpanRequiredAttributes(s)...)
	errs = append(errs, validateToolSpanStatus(s)...)
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
	errs = append(errs, validateAttributes(e.Attributes)...)
	if !validEventName(e.Name) {
		errs = append(errs, fmt.Errorf("invalid event name %q", e.Name))
	}
	if e.Timestamp.IsZero() {
		errs = append(errs, errors.New("event timestamp is required"))
	} else if e.Timestamp.Location() != time.UTC {
		errs = append(errs, errors.New("event timestamp must be UTC"))
	}
	if e.Status != "" && !validStatus(e.Status) {
		errs = append(errs, fmt.Errorf("invalid event status %q", e.Status))
	}
	if e.Category == "" {
		errs = append(errs, errors.New("event category is required"))
	} else if want := eventCategory(e.Name); e.Category != want {
		errs = append(errs, fmt.Errorf("event category %q does not match event name %q", e.Category, e.Name))
	}
	if e.Error != nil {
		errs = append(errs, e.Error.validate("event")...)
	}
	errs = append(errs, validateEventRequiredAttributes(e)...)
	errs = append(errs, validateToolEventStatus(e)...)
	errs = append(errs, validateEventErrorFields(e)...)
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
	if e.Retryable == nil {
		errs = append(errs, fmt.Errorf("%s error retryable flag is required", scope))
	}
	if e.Canceled != nil && e.Retryable != nil && *e.Canceled && *e.Retryable {
		errs = append(errs, fmt.Errorf("%s canceled error cannot be retryable", scope))
	}
	return errs
}

func validateTerminalErrorFlags(scope string, status Status, err ObservationError) []error {
	var errs []error
	if status == StatusCanceled {
		if err.Canceled == nil || !*err.Canceled {
			errs = append(errs, fmt.Errorf("%s canceled status requires error.canceled=true", scope))
		}
		if err.Retryable == nil || *err.Retryable {
			errs = append(errs, fmt.Errorf("%s canceled status requires error.retryable=false", scope))
		}
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

func validateAttributes(attrs Attributes) []error {
	var errs []error
	for key, value := range attrs {
		if value == nil {
			errs = append(errs, fmt.Errorf("attribute %q has nil value", key))
			continue
		}
		switch value.(type) {
		case string, bool, int, int64, float64:
		default:
			errs = append(errs, fmt.Errorf("attribute %q has unsupported value type %T", key, value))
		}
	}
	return errs
}

func validateSpanRequiredAttributes(span Span) []error {
	switch span.Kind {
	case SpanKindModelCall, SpanKindStream:
		return append(
			requireStringAttrs(span.Attributes, "genai.provider", "genai.model"),
			validateOptionalInt64Attrs(span.Attributes,
				"genai.usage.input_tokens",
				"genai.usage.output_tokens",
				"genai.usage.total_tokens",
				"genai.usage.reasoning_tokens",
				"genai.usage.cached_input_tokens",
				"genai.latency.first_token_ms",
				"genai.latency.total_ms",
				"genai.retry.attempt",
			)...,
		)
	case SpanKindToolCall:
		errs := requireStringAttrs(span.Attributes, "tool.name", "tool.call_id", "tool.kind", "tool.status")
		if !span.EndTime.IsZero() {
			errs = append(errs, requireInt64Attrs(span.Attributes, "tool.latency.ms")...)
		}
		return errs
	default:
		return nil
	}
}

func validateEventRequiredAttributes(event Event) []error {
	switch event.Name {
	case EventStreamChunk:
		return requireInt64Attrs(event.Attributes, "stream.chunk.index")
	case EventStreamFirstTok:
		return requireInt64Attrs(event.Attributes, "genai.latency.first_token_ms")
	case EventToolRegistered:
		return requireStringAttrs(event.Attributes, "tool.name", "tool.kind", "tool.status")
	case EventToolMaterialize:
		return requireStringAttrs(event.Attributes, "tool.name", "tool.call_id", "tool.kind", "tool.status")
	case EventToolSettled:
		return requireStringAttrs(event.Attributes, "tool.name", "tool.call_id", "tool.kind", "tool.status")
	case EventRetry:
		return append(requireInt64Attrs(event.Attributes, "retry.attempt"), requireStringAttrs(event.Attributes, "retry.reason")...)
	case EventCompaction:
		return requireStringAttrs(event.Attributes, "compaction.reason")
	case EventInterrupt:
		return requireStringAttrs(event.Attributes, "interrupt.reason")
	case EventResume:
		return requireStringAttrs(event.Attributes, "resume.reason")
	case EventCancellation:
		return requireStringAttrs(event.Attributes, "cancellation.reason")
	case EventError:
		return nil
	case EventObservationErr:
		return nil
	case EventRedaction:
		if len(event.Redaction) == 0 {
			return []error{errors.New("redaction.applied event requires redaction records")}
		}
		return nil
	default:
		return nil
	}
}

func validateToolSpanStatus(span Span) []error {
	if span.Kind != SpanKindToolCall {
		return nil
	}
	status, _ := span.Attributes["tool.status"].(string)
	if status == "" {
		return nil
	}
	if span.EndTime.IsZero() {
		if status != "started" {
			return []error{fmt.Errorf("active tool_call span requires tool.status=started, got %q", status)}
		}
		return nil
	}
	switch status {
	case "succeeded":
		return nil
	case "failed":
		return validateToolTerminalError(span.Error, false)
	case "canceled":
		return validateToolTerminalError(span.Error, true)
	default:
		return []error{fmt.Errorf("terminal tool_call span has invalid tool.status %q", status)}
	}
}

func validateToolEventStatus(event Event) []error {
	status, _ := event.Attributes["tool.status"].(string)
	switch event.Name {
	case EventToolRegistered:
		if status != "" && status != "registered" {
			return []error{fmt.Errorf("tool.registered requires tool.status=registered, got %q", status)}
		}
	case EventToolMaterialize:
		if status != "" && status != "materialized" {
			return []error{fmt.Errorf("tool.materialized requires tool.status=materialized, got %q", status)}
		}
	case EventToolSettled:
		switch status {
		case "succeeded":
			return nil
		case "failed":
			return validateToolTerminalError(event.Error, false)
		case "canceled":
			return validateToolTerminalError(event.Error, true)
		case "":
			return nil
		default:
			return []error{fmt.Errorf("tool.settled has invalid tool.status %q", status)}
		}
	}
	return nil
}

func validateToolTerminalError(err *ObservationError, canceled bool) []error {
	if err == nil {
		return []error{errors.New("terminal tool failure/cancellation requires error fields")}
	}
	var errs []error
	if err.Operation != "tool_call" {
		errs = append(errs, fmt.Errorf("terminal tool error operation = %q, want tool_call", err.Operation))
	}
	errs = append(errs, err.validate("tool")...)
	if canceled {
		errs = append(errs, validateTerminalErrorFlags("tool", StatusCanceled, *err)...)
	}
	return errs
}

func validateEventErrorFields(event Event) []error {
	switch event.Name {
	case EventError, EventObservationErr:
		if event.Error == nil {
			return []error{fmt.Errorf("%s event requires error fields", event.Name)}
		}
		var errs []error
		if event.Name == EventObservationErr {
			if event.Error.Dropped == nil {
				errs = append(errs, errors.New("observation.error event requires error.dropped"))
			}
		}
		return errs
	case EventCancellation:
		if event.Error == nil {
			return []error{errors.New("cancellation event requires error fields")}
		}
		return validateTerminalErrorFlags("cancellation event", StatusCanceled, *event.Error)
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

func requireStringAttrs(attrs Attributes, required ...string) []error {
	return requireTypedAttrs(attrs, "string", func(value any) bool {
		_, ok := value.(string)
		return ok
	}, required...)
}

func requireInt64Attrs(attrs Attributes, required ...string) []error {
	return requireTypedAttrs(attrs, "int64", isIntValue, required...)
}

func validateOptionalInt64Attrs(attrs Attributes, keys ...string) []error {
	var errs []error
	for _, key := range keys {
		if value, ok := attrs[key]; ok && !isIntValue(value) {
			errs = append(errs, fmt.Errorf("attribute %q must be int64-compatible, got %T", key, value))
		}
	}
	return errs
}

func requireTypedAttrs(attrs Attributes, typeName string, valid func(any) bool, required ...string) []error {
	var errs []error
	errs = append(errs, missingAttrs(attrs, required...)...)
	for _, key := range required {
		value, ok := attrs[key]
		if !ok {
			continue
		}
		if !valid(value) {
			errs = append(errs, fmt.Errorf("required attribute %q must be %s, got %T", key, typeName, value))
		}
	}
	return errs
}

func isIntValue(value any) bool {
	switch value.(type) {
	case int, int64:
		return true
	default:
		return false
	}
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
