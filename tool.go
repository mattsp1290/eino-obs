package einoobs

import (
	"context"
	"time"

	"github.com/mattsp1290/eino-obs/internal/redaction"
)

type ToolRegistered struct {
	Correlation Correlation
	ToolName    string
	ToolCallID  string
	ToolKind    string
	Time        time.Time
	Metadata    Metadata
}

type ToolMaterialized struct {
	Correlation  Correlation
	ToolCallID   string
	ToolName     string
	ToolKind     string
	Time         time.Time
	InputSummary Summary
	Metadata     Metadata
}

func (o *Observer) ToolRegistered(ctx context.Context, event ToolRegistered) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	toolCallID := firstNonEmpty(event.ToolCallID, corr.ToolCallID)
	if toolCallID != "" {
		corr.ToolCallID = toolCallID
	}
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	addToolAttributes(attrs, event.ToolName, toolCallID, firstNonEmpty(event.ToolKind, "unknown"), "registered")
	observation := toolEventObservation(corr, "tool.registered", observationTime(event.Time), attrs, nil)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func (o *Observer) ToolMaterialized(ctx context.Context, event ToolMaterialized) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	toolCallID := firstNonEmpty(event.ToolCallID, corr.ToolCallID)
	if toolCallID != "" {
		corr.ToolCallID = toolCallID
	}
	if toolCallID == "" {
		if o != nil {
			o.handleError(ctx, ObservationError{Operation: "record", Classification: "invalid_schema", Dropped: true})
		}
		return
	}
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	addToolAttributes(attrs, event.ToolName, toolCallID, firstNonEmpty(event.ToolKind, "unknown"), "materialized")

	var records []RedactionRecord
	summaryAttrs, summaryRecords := toolInputSummaryAttributes(o, event.InputSummary)
	for key, value := range summaryAttrs {
		attrs[key] = value
	}
	records = append(records, summaryRecords...)

	observation := toolEventObservation(corr, "tool.materialized", observationTime(event.Time), attrs, records)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func addToolAttributes(attrs map[string]any, name string, callID string, kind string, status string) {
	addStringAttr(attrs, "tool.name", name)
	addStringAttr(attrs, "tool.call_id", callID)
	addStringAttr(attrs, "tool.kind", kind)
	addStringAttr(attrs, "tool.status", status)
}

func toolEventObservation(corr Correlation, name string, timestamp time.Time, attrs map[string]any, records []RedactionRecord) Observation {
	return Observation{
		ID:         corr.ObservationID,
		ParentID:   corr.ParentObservationID,
		TraceID:    corr.TraceID,
		Kind:       name,
		Name:       name,
		Status:     "ok",
		Timestamp:  timestamp,
		Attributes: attrs,
		Redaction:  clonePublicRedaction(records),
	}
}

func toolInputSummaryAttributes(observer *Observer, summary Summary) (map[string]any, []RedactionRecord) {
	if summary.Name == "" && summary.Text == "" && len(summary.Fields) == 0 {
		return nil, nil
	}
	opts := RedactionOptions{}
	if observer != nil {
		opts = observer.Config().Redaction
	}
	attrs, records, err := redaction.SummaryAttributes(
		"tool.input.summary",
		"tool.input.summary",
		redaction.InputSummary,
		redaction.Summary{Name: summary.Name, Text: summary.Text, Fields: cloneStringMap(summary.Fields)},
		redaction.Options{
			CaptureInputSummary:  opts.CaptureInputSummary,
			CaptureOutputSummary: opts.CaptureOutputSummary,
			MaxSummaryBytes:      opts.MaxSummaryBytes,
		},
	)
	if err != nil {
		return nil, nil
	}
	return cloneModelAttributes(attrs), modelRedactionToPublic(records)
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	out := make(map[string]string, len(values))
	for key, value := range values {
		out[key] = value
	}
	return out
}
