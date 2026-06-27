package einoobs

import (
	"context"
	"sync"
	"time"

	"github.com/mattsp1290/eino-obs/internal/redaction"
)

type ModelCallStart struct {
	Correlation   Correlation
	ProviderModel ProviderModel
	Name          string
	StartTime     time.Time
	RetryAttempt  int64
	InputSummary  Summary
	Metadata      Metadata
}

type ModelCallEnd struct {
	EndTime       time.Time
	Usage         TokenUsage
	OutputSummary Summary
	Metadata      Metadata
}

type ModelCallError struct {
	Err            error
	Classification string
	Canceled       bool
	Retryable      bool
	EndTime        time.Time
	PartialUsage   TokenUsage
	OutputSummary  Summary
	Metadata       Metadata
}

type ModelCall struct {
	mu       sync.Mutex
	ended    bool
	observer *Observer
	ctx      context.Context
	corr     Correlation
	name     string
	start    time.Time
	attrs    map[string]any
	redact   []RedactionRecord
}

func (o *Observer) StartModelCall(ctx context.Context, start ModelCallStart) *ModelCall {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, start.Correlation)
	provider := firstNonEmpty(start.ProviderModel.Provider, corr.Provider)
	modelName := firstNonEmpty(start.ProviderModel.Model, corr.Model)
	if provider != "" {
		corr.Provider = provider
	}
	if modelName != "" {
		corr.Model = modelName
	}
	if provider == "" || modelName == "" {
		if o != nil {
			o.handleError(ctx, ObservationError{Operation: "record", Classification: "invalid_schema", Dropped: true})
		}
		return &ModelCall{ended: true}
	}
	attrs := baseObservationAttributes(o, corr, cloneMetadata(start.Metadata))
	addStringAttr(attrs, "genai.provider", provider)
	addStringAttr(attrs, "genai.model", modelName)
	if start.RetryAttempt > 0 {
		attrs["genai.retry.attempt"] = start.RetryAttempt
	}
	var records []RedactionRecord
	summaryAttrs, summaryRecords := modelInputSummaryAttributes(o, start.InputSummary)
	for key, value := range summaryAttrs {
		attrs[key] = value
	}
	records = append(records, summaryRecords...)

	return &ModelCall{
		observer: o,
		ctx:      context.WithoutCancel(ctx),
		corr:     corr,
		name:     firstNonEmpty(start.Name, "model_call"),
		start:    observationTime(start.StartTime),
		attrs:    attrs,
		redact:   clonePublicRedaction(records),
	}
}

func (m *ModelCall) End(end ModelCallEnd) {
	if m == nil {
		return
	}
	m.finish("ok", observationTime(end.EndTime), nil, end.Usage, end.OutputSummary, end.Metadata)
}

func (m *ModelCall) Error(event ModelCallError) {
	if m == nil {
		return
	}
	status := "error"
	retryable := event.Retryable
	if event.Canceled {
		status = "canceled"
		retryable = false
	}
	err := terminalObservationError("model_call", firstNonEmpty(event.Classification, status), event.Err, retryable)
	m.finish(status, observationTime(event.EndTime), &err, event.PartialUsage, event.OutputSummary, event.Metadata)
}

func (m *ModelCall) finish(status string, endTime time.Time, obsErr *ObservationError, usage TokenUsage, output Summary, metadata Metadata) {
	m.mu.Lock()
	if m.ended {
		m.mu.Unlock()
		return
	}
	m.ended = true
	attrs := cloneObservationAttributes(m.attrs)
	addMetadataAttributes(attrs, metadata)
	if endTime.Before(m.start) {
		endTime = m.start
	}
	duration := endTime.Sub(m.start)
	attrs["genai.latency.total_ms"] = int64(duration / time.Millisecond)
	addTokenUsageAttributes(attrs, usage)
	records := clonePublicRedaction(m.redact)
	summaryAttrs, summaryRecords := modelOutputSummaryAttributes(m.observer, output)
	for key, value := range summaryAttrs {
		attrs[key] = value
	}
	records = append(records, summaryRecords...)
	if obsErr != nil {
		addModelErrorAttributes(attrs, *obsErr, status == "canceled")
	}
	observation := Observation{
		ID:            m.corr.ObservationID,
		ParentID:      m.corr.ParentObservationID,
		TraceID:       m.corr.TraceID,
		Kind:          "model_call",
		Name:          m.name,
		Status:        status,
		Timestamp:     m.start,
		Duration:      duration,
		DurationKnown: true,
		Attributes:    attrs,
		Redaction:     clonePublicRedaction(records),
		Error:         cloneObservationErrorPtr(obsErr),
	}
	ctx := m.ctx
	observer := m.observer
	m.mu.Unlock()

	exportObservation(ctx, observer, observation)
}

func addTokenUsageAttributes(attrs map[string]any, usage TokenUsage) {
	if usage.InputTokens != 0 || usage.InputTokensKnown {
		attrs["genai.usage.input_tokens"] = usage.InputTokens
	}
	if usage.OutputTokens != 0 || usage.OutputTokensKnown {
		attrs["genai.usage.output_tokens"] = usage.OutputTokens
	}
	if usage.TotalTokens != 0 || usage.TotalTokensKnown {
		attrs["genai.usage.total_tokens"] = usage.TotalTokens
	}
	if usage.ReasoningTokens != 0 || usage.ReasoningTokensKnown {
		attrs["genai.usage.reasoning_tokens"] = usage.ReasoningTokens
	}
	if usage.CachedInputTokens != 0 || usage.CachedInputTokensKnown {
		attrs["genai.usage.cached_input_tokens"] = usage.CachedInputTokens
	}
}

func addModelErrorAttributes(attrs map[string]any, err ObservationError, canceled bool) {
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

func modelInputSummaryAttributes(observer *Observer, summary Summary) (map[string]any, []RedactionRecord) {
	return modelSummaryAttributes(observer, "genai.request.summary", redaction.InputSummary, summary)
}

func modelOutputSummaryAttributes(observer *Observer, summary Summary) (map[string]any, []RedactionRecord) {
	return modelSummaryAttributes(observer, "genai.response.summary", redaction.OutputSummary, summary)
}

func modelSummaryAttributes(observer *Observer, attrPrefix string, kind redaction.SummarySide, summary Summary) (map[string]any, []RedactionRecord) {
	if summary.Name == "" && summary.Text == "" && len(summary.Fields) == 0 {
		return nil, nil
	}
	opts := RedactionOptions{}
	if observer != nil {
		opts = observer.Config().Redaction
	}
	attrs, records, err := redaction.SummaryAttributes(
		attrPrefix,
		attrPrefix,
		kind,
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
