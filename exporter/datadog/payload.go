package datadog

import (
	"time"

	"github.com/mattsp1290/eino-obs/internal/model"
)

type payload struct {
	Spans []spanPayload `json:"spans"`
}

type spanPayload struct {
	TraceID  string         `json:"trace_id,omitempty"`
	SpanID   string         `json:"span_id,omitempty"`
	ParentID string         `json:"parent_id,omitempty"`
	Name     string         `json:"name"`
	StartNS  int64          `json:"start_ns"`
	Duration int64          `json:"duration"`
	MLApp    string         `json:"ml_app"`
	Meta     map[string]any `json:"meta,omitempty"`
	Metrics  map[string]any `json:"metrics,omitempty"`
	Events   []eventPayload `json:"events,omitempty"`
}

type eventPayload struct {
	EventID  string         `json:"event_id,omitempty"`
	ParentID string         `json:"parent_id,omitempty"`
	TraceID  string         `json:"trace_id,omitempty"`
	Name     string         `json:"name"`
	TimeNS   int64          `json:"time_ns"`
	Meta     map[string]any `json:"meta,omitempty"`
}

type redactionPayload struct {
	FieldPath     string `json:"field_path"`
	Reason        string `json:"reason"`
	OriginalBytes int    `json:"original_bytes,omitempty"`
	RetainedBytes int    `json:"retained_bytes,omitempty"`
}

func buildPayload(config Config, spans []model.Span) payload {
	out := payload{Spans: make([]spanPayload, 0, len(spans))}
	for _, span := range spans {
		item, ok := mapSpan(config, span)
		if !ok {
			continue
		}
		out.Spans = append(out.Spans, item)
	}
	return out
}

func mapSpan(config Config, span model.Span) (spanPayload, bool) {
	duration, ok := span.Duration()
	if !ok {
		return spanPayload{}, false
	}
	meta := map[string]any{
		"kind":     datadogKind(span.Kind),
		"obs.kind": string(span.Kind),
		"obs.name": span.Name,
		"status":   string(span.Status),
	}
	addStringMeta(meta, "service.name", firstConfigValue(config.Service, defaultService))
	addStringMeta(meta, "service.env", config.Env)
	addStringMeta(meta, "service.version", config.Version)
	metrics := map[string]any{}
	for key, value := range span.Attributes {
		if metricName, ok := metricNameForAttribute(key); ok {
			if safe, ok := jsonSafeValue(value); ok {
				metrics[metricName] = safe
			}
			continue
		}
		if safe, ok := jsonSafeValue(value); ok {
			meta[key] = safe
		}
	}
	addErrorMeta(meta, span.Error)
	addRedactionMeta(meta, span.Redaction)

	return spanPayload{
		TraceID:  span.Identity.TraceID,
		SpanID:   span.Identity.ID,
		ParentID: span.Identity.ParentID,
		Name:     span.Name,
		StartNS:  unixNano(span.StartTime),
		Duration: duration.Nanoseconds(),
		MLApp:    firstConfigValue(config.MLApp, config.Service, defaultMLApp),
		Meta:     nilIfEmpty(meta),
		Metrics:  nilIfEmpty(metrics),
		Events:   mapEvents(span.Events),
	}, true
}

func mapEvents(events []model.Event) []eventPayload {
	if len(events) == 0 {
		return nil
	}
	out := make([]eventPayload, 0, len(events))
	for _, event := range events {
		meta := map[string]any{
			"obs.name": string(event.Name),
			"obs.kind": event.Category,
		}
		addStringMeta(meta, "obs.status", string(event.Status))
		for key, value := range event.Attributes {
			if safe, ok := jsonSafeValue(value); ok {
				meta[key] = safe
			}
		}
		addErrorMeta(meta, event.Error)
		addRedactionMeta(meta, event.Redaction)
		out = append(out, eventPayload{
			EventID:  event.Identity.ID,
			ParentID: event.Identity.ParentID,
			TraceID:  event.Identity.TraceID,
			Name:     string(event.Name),
			TimeNS:   unixNano(event.Timestamp),
			Meta:     nilIfEmpty(meta),
		})
	}
	return out
}

func metricNameForAttribute(key string) (string, bool) {
	switch key {
	case "genai.usage.input_tokens":
		return "input_tokens", true
	case "genai.usage.output_tokens":
		return "output_tokens", true
	case "genai.usage.total_tokens":
		return "total_tokens", true
	default:
		return "", false
	}
}

func datadogKind(kind model.SpanKind) string {
	switch kind {
	case model.SpanKindRun:
		return "workflow"
	case model.SpanKindModelCall, model.SpanKindStream:
		return "llm"
	case model.SpanKindToolCall:
		return "tool"
	case model.SpanKindExportFlush, model.SpanKindExportShutdown:
		return "task"
	default:
		return string(kind)
	}
}

func addErrorMeta(meta map[string]any, err *model.ObservationError) {
	if err == nil {
		return
	}
	addStringMeta(meta, "error.operation", err.Operation)
	addStringMeta(meta, "error.type", err.Type)
	addStringMeta(meta, "error.code", err.Code)
	addStringMeta(meta, "error.message", err.Message)
	addStringMeta(meta, "error.classification", err.Classification)
	if err.Retryable != nil {
		meta["error.retryable"] = *err.Retryable
	}
	if err.Canceled != nil {
		meta["error.canceled"] = *err.Canceled
	}
	if err.Dropped != nil {
		meta["error.dropped"] = *err.Dropped
	}
}

func addRedactionMeta(meta map[string]any, records []model.RedactionRecord) {
	if len(records) == 0 {
		return
	}
	out := make([]redactionPayload, len(records))
	for i, record := range records {
		out[i] = redactionPayload{
			FieldPath:     record.FieldPath,
			Reason:        record.Reason,
			OriginalBytes: record.OriginalBytes,
			RetainedBytes: record.RetainedBytes,
		}
	}
	meta["metadata.redaction.records"] = out
}

func addStringMeta(meta map[string]any, key string, value string) {
	if value == "" {
		return
	}
	meta[key] = value
}

func nilIfEmpty[K comparable, V any](values map[K]V) map[K]V {
	if len(values) == 0 {
		return nil
	}
	return values
}

func jsonSafeValue(value any) (any, bool) {
	switch v := value.(type) {
	case string, bool, int, int64, float64:
		return v, true
	default:
		return nil, false
	}
}

func unixNano(timestamp time.Time) int64 {
	if timestamp.IsZero() {
		return 0
	}
	return timestamp.UTC().UnixNano()
}
