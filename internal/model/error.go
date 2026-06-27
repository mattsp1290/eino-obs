package model

import (
	"errors"
	"reflect"
)

const (
	ErrorClassificationUnknown  = "unknown"
	ErrorClassificationCanceled = "canceled"
)

type ErrorFields struct {
	Operation      string
	Type           string
	Code           string
	Message        string
	Classification string
	Cause          error
	Retryable      bool
	Canceled       bool
	Dropped        bool
}

func NewObservationError(fields ErrorFields) ObservationError {
	classification := fields.Classification
	if classification == "" {
		if fields.Canceled {
			classification = ErrorClassificationCanceled
		} else {
			classification = ErrorClassificationUnknown
		}
	}
	retryable := fields.Retryable
	if fields.Canceled {
		retryable = false
	}
	message := fields.Message
	if message == "" && fields.Cause != nil {
		message = fields.Cause.Error()
	}
	return ObservationError{
		Operation:      fields.Operation,
		Type:           firstNonEmpty(fields.Type, errorType(fields.Cause)),
		Code:           fields.Code,
		Message:        message,
		Classification: classification,
		Cause:          fields.Cause,
		Retryable:      errorBoolPtr(retryable),
		Canceled:       errorBoolPtr(fields.Canceled),
		Dropped:        errorBoolPtr(fields.Dropped),
	}
}

func NormalizeObservationError(operation string, classification string, cause error, retryable bool) ObservationError {
	var obsErr ObservationError
	if errors.As(cause, &obsErr) {
		return obsErr.Normalize(ErrorFields{
			Operation:      operation,
			Classification: classification,
			Cause:          cause,
			Retryable:      retryable,
		})
	}
	return NewObservationError(ErrorFields{
		Operation:      operation,
		Classification: classification,
		Cause:          cause,
		Retryable:      retryable,
	})
}

func CanceledObservationError(operation string, classification string, cause error) ObservationError {
	return NewObservationError(ErrorFields{
		Operation:      operation,
		Classification: firstNonEmpty(classification, ErrorClassificationCanceled),
		Cause:          cause,
		Canceled:       true,
	})
}

func DroppedObservationError(operation string, classification string, cause error, retryable bool) ObservationError {
	return NewObservationError(ErrorFields{
		Operation:      operation,
		Classification: classification,
		Cause:          cause,
		Retryable:      retryable,
		Dropped:        true,
	})
}

func (e ObservationError) Normalize(defaults ErrorFields) ObservationError {
	out := e
	if out.Operation == "" {
		out.Operation = defaults.Operation
	}
	if out.Type == "" {
		out.Type = firstNonEmpty(defaults.Type, errorType(firstNonNilError(out.Cause, defaults.Cause)))
	}
	if out.Code == "" {
		out.Code = defaults.Code
	}
	if out.Classification == "" {
		out.Classification = firstNonEmpty(defaults.Classification, ErrorClassificationUnknown)
	}
	if out.Cause == nil {
		out.Cause = defaults.Cause
	}
	if out.Message == "" {
		if defaults.Message != "" {
			out.Message = defaults.Message
		} else if out.Cause != nil {
			out.Message = out.Cause.Error()
		}
	}
	if out.Retryable == nil {
		retryable := defaults.Retryable
		out.Retryable = errorBoolPtr(retryable)
	}
	if out.Canceled == nil {
		out.Canceled = errorBoolPtr(defaults.Canceled)
	}
	if out.Dropped == nil {
		out.Dropped = errorBoolPtr(defaults.Dropped)
	}
	if out.Canceled != nil && *out.Canceled && out.Retryable != nil {
		*out.Retryable = false
	}
	return out
}

func (e ObservationError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	if e.Cause != nil {
		return e.Cause.Error()
	}
	if e.Code != "" {
		return e.Code
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
	return e.Cause
}

func (s *Span) RecordError(err ObservationError) {
	if s == nil {
		return
	}
	normalized := err.Normalize(ErrorFields{Operation: string(s.Kind)})
	s.Error = &normalized
	s.applyErrorAttributes(normalized)
	if s.Status == "" || s.Status == StatusOK {
		if normalized.Canceled != nil && *normalized.Canceled {
			s.Status = StatusCanceled
		} else {
			s.Status = StatusError
		}
	}
}

func (e *Event) RecordError(err ObservationError) {
	if e == nil {
		return
	}
	normalized := err.Normalize(ErrorFields{Operation: string(e.Name)})
	e.Error = &normalized
	e.applyErrorAttributes(normalized)
	if e.Status == "" || e.Status == StatusOK {
		if normalized.Canceled != nil && *normalized.Canceled {
			e.Status = StatusCanceled
		} else {
			e.Status = StatusError
		}
	}
}

func (s *Span) applyErrorAttributes(err ObservationError) {
	if s.Attributes == nil {
		s.Attributes = Attributes{}
	}
	RecordErrorAttributes(s.Attributes, err)
}

func (e *Event) applyErrorAttributes(err ObservationError) {
	if e.Attributes == nil {
		e.Attributes = Attributes{}
	}
	RecordErrorAttributes(e.Attributes, err)
}

func RecordErrorAttributes(attrs Attributes, err ObservationError) {
	if attrs == nil {
		return
	}
	setStringAttr(attrs, "error.operation", err.Operation)
	setStringAttr(attrs, "error.type", err.Type)
	setStringAttr(attrs, "error.code", err.Code)
	setStringAttr(attrs, "error.message", err.Message)
	setStringAttr(attrs, "error.classification", err.Classification)
	if err.Retryable != nil {
		attrs["error.retryable"] = *err.Retryable
	}
	if err.Canceled != nil {
		attrs["error.canceled"] = *err.Canceled
	}
	if err.Dropped != nil {
		attrs["error.dropped"] = *err.Dropped
	}
}

func setStringAttr(attrs Attributes, key string, value string) {
	if value != "" {
		attrs[key] = value
	}
}

func errorBoolPtr(value bool) *bool {
	return &value
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func firstNonNilError(values ...error) error {
	for _, value := range values {
		if value != nil {
			return value
		}
	}
	return nil
}

func errorType(err error) string {
	if err == nil {
		return ""
	}
	t := reflect.TypeOf(err)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	if t.PkgPath() == "" {
		return t.Name()
	}
	return t.PkgPath() + "." + t.Name()
}
