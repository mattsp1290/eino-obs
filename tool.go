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

type AGUIToolMaterialized struct {
	Correlation  Correlation
	ThreadID     string
	AGUIRunID    string
	ToolCallID   string
	ToolName     string
	Time         time.Time
	InputSummary Summary
	Metadata     Metadata
}

type AGUIToolSettled struct {
	Correlation    Correlation
	ThreadID       string
	AGUIRunID      string
	ToolCallID     string
	ToolName       string
	Status         string
	Time           time.Time
	Error          ObservationError
	Classification string
	Retryable      bool
	Metadata       Metadata
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

func (o *Observer) AGUIToolMaterialized(ctx context.Context, event AGUIToolMaterialized) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := aguiToolCorrelation(ctx, event.Correlation, event.ThreadID, event.AGUIRunID, event.ToolCallID)
	o.ToolMaterialized(ctx, ToolMaterialized{
		Correlation:  corr,
		ToolCallID:   firstNonEmpty(event.ToolCallID, corr.ToolCallID),
		ToolName:     event.ToolName,
		ToolKind:     "client_proposed",
		Time:         event.Time,
		InputSummary: event.InputSummary,
		Metadata:     event.Metadata,
	})
}

func (o *Observer) AGUIToolSettled(ctx context.Context, event AGUIToolSettled) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := aguiToolCorrelation(ctx, event.Correlation, event.ThreadID, event.AGUIRunID, event.ToolCallID)
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
	status := firstNonEmpty(event.Status, "succeeded")
	if !validToolSettledStatus(status) {
		if o != nil {
			o.handleError(ctx, ObservationError{Operation: "record", Classification: "invalid_schema", Dropped: true})
		}
		return
	}
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	addToolAttributes(attrs, event.ToolName, toolCallID, "client_proposed", status)

	observationStatus := "ok"
	var obsErr *ObservationError
	if status == "failed" || status == "canceled" {
		observationStatus = "error"
		retryable := event.Error.Retryable || event.Retryable
		if status == "canceled" {
			observationStatus = "canceled"
			retryable = false
		}
		err := event.Error
		if err.Operation == "" && err.Classification == "" && err.Err == nil {
			err = terminalObservationError("tool_call", firstNonEmpty(event.Classification, status), nil, retryable)
		}
		if err.Operation == "" {
			err.Operation = "tool_call"
		}
		if err.Classification == "" {
			err.Classification = firstNonEmpty(event.Classification, status)
		}
		err.Retryable = retryable
		obsErr = &err
		addToolErrorAttributes(attrs, err, status == "canceled")
	}

	observation := toolEventObservation(corr, "tool.settled", observationTime(event.Time), attrs, nil)
	observation.Status = observationStatus
	observation.Error = cloneObservationErrorPtr(obsErr)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func addToolAttributes(attrs map[string]any, name string, callID string, kind string, status string) {
	addStringAttr(attrs, "tool.name", name)
	addStringAttr(attrs, "tool.call_id", callID)
	addStringAttr(attrs, "tool.kind", kind)
	addStringAttr(attrs, "tool.status", status)
}

func aguiToolCorrelation(ctx context.Context, explicit Correlation, threadID string, aguiRunID string, toolCallID string) Correlation {
	corr := correlationFromContext(ctx, explicit)
	if threadID != "" {
		corr.ThreadID = threadID
	}
	if aguiRunID != "" {
		corr.AGUIRunID = aguiRunID
	}
	if toolCallID != "" {
		corr.ToolCallID = toolCallID
	}
	return corr
}

func validToolSettledStatus(status string) bool {
	switch status {
	case "succeeded", "failed", "canceled":
		return true
	default:
		return false
	}
}

func addToolErrorAttributes(attrs map[string]any, err ObservationError, canceled bool) {
	addStringAttr(attrs, "error.operation", err.Operation)
	addStringAttr(attrs, "error.classification", err.Classification)
	attrs["error.retryable"] = err.Retryable
	if canceled {
		attrs["error.canceled"] = true
	}
	if err.Dropped {
		attrs["error.dropped"] = true
	}
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
