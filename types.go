package einoobs

import (
	"context"
	"time"
)

type Config struct {
	Service      string
	Env          string
	Version      string
	Redaction    RedactionOptions
	Exporter     Exporter
	ErrorHandler ErrorHandler
}

type Exporter interface {
	Export(ctx context.Context, batch []Observation) error
	Flush(ctx context.Context) error
	Shutdown(ctx context.Context) error
}

type ProviderModel struct {
	Provider string
	Model    string
}

type TokenUsage struct {
	InputTokens       int64
	OutputTokens      int64
	TotalTokens       int64
	ReasoningTokens   int64
	CachedInputTokens int64
}

type Summary struct {
	Name   string
	Text   string
	Fields map[string]string
}

type Metadata map[string]string

type RedactionOptions struct {
	CaptureInputSummary  bool
	CaptureOutputSummary bool
	MaxSummaryBytes      int
}

type RedactionRecord struct {
	FieldPath     string
	Reason        string
	OriginalBytes int
	RetainedBytes int
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

type ErrorHandler func(context.Context, ObservationError)

type Observation struct {
	ID            string
	ParentID      string
	TraceID       string
	Kind          string
	Name          string
	Status        string
	Timestamp     time.Time
	Duration      time.Duration
	DurationKnown bool
	Attributes    map[string]any
	Events        []Observation
	Redaction     []RedactionRecord
	Error         *ObservationError
}

func (o Observation) Clone() Observation {
	out := o
	out.Attributes = cloneObservationAttributes(o.Attributes)
	out.Events = cloneObservations(o.Events)
	out.Redaction = clonePublicRedaction(o.Redaction)
	if o.Error != nil {
		errCopy := *o.Error
		out.Error = &errCopy
	}
	return out
}

func cloneObservations(observations []Observation) []Observation {
	if observations == nil {
		return nil
	}
	out := make([]Observation, len(observations))
	for i, observation := range observations {
		out[i] = observation.Clone()
	}
	return out
}

func clonePublicRedaction(records []RedactionRecord) []RedactionRecord {
	if records == nil {
		return nil
	}
	out := make([]RedactionRecord, len(records))
	copy(out, records)
	return out
}

func cloneObservationAttributes(attrs map[string]any) map[string]any {
	if attrs == nil {
		return nil
	}
	out := make(map[string]any, len(attrs))
	for key, value := range attrs {
		out[key] = cloneObservationAttribute(value)
	}
	return out
}

func cloneObservationAttribute(value any) any {
	switch v := value.(type) {
	case Metadata:
		out := make(Metadata, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	case map[string]string:
		out := make(map[string]string, len(v))
		for key, item := range v {
			out[key] = item
		}
		return out
	case Summary:
		out := v
		if v.Fields != nil {
			out.Fields = make(map[string]string, len(v.Fields))
			for key, item := range v.Fields {
				out.Fields[key] = item
			}
		}
		return out
	case []byte:
		out := make([]byte, len(v))
		copy(out, v)
		return out
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneObservationAttribute(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneObservationAttribute(item)
		}
		return out
	default:
		return value
	}
}
