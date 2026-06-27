package einoobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/mattsp1290/eino-obs/internal/redaction"
)

type StreamStart struct {
	Correlation   Correlation
	ProviderModel ProviderModel
	Name          string
	StartTime     time.Time
	RetryAttempt  int64
	InputSummary  Summary
	Metadata      Metadata
}

type StreamChunk struct {
	Index         int64
	Time          time.Time
	OutputSummary Summary
	Metadata      Metadata
}

type StreamFirstToken struct {
	Time     time.Time
	Latency  time.Duration
	Metadata Metadata
}

type StreamEnd struct {
	EndTime       time.Time
	Usage         TokenUsage
	OutputSummary Summary
	Metadata      Metadata
}

type StreamError struct {
	Err            error
	Classification string
	Canceled       bool
	Retryable      bool
	EndTime        time.Time
	PartialUsage   TokenUsage
	OutputSummary  Summary
	Metadata       Metadata
}

type Stream struct {
	mu                sync.Mutex
	ended             bool
	firstTokenEmitted bool
	firstTokenLatency int64
	observer          *Observer
	ctx               context.Context
	corr              Correlation
	name              string
	start             time.Time
	attrs             map[string]any
	events            []Observation
	redact            []RedactionRecord
}

func (o *Observer) StartStream(ctx context.Context, start StreamStart) *Stream {
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
		return &Stream{ended: true}
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

	return &Stream{
		observer: o,
		ctx:      context.WithoutCancel(ctx),
		corr:     corr,
		name:     firstNonEmpty(start.Name, "stream"),
		start:    observationTime(start.StartTime),
		attrs:    attrs,
		redact:   clonePublicRedaction(records),
	}
}

func (s *Stream) Chunk(chunk StreamChunk) {
	if s == nil {
		return
	}
	eventTime := observationTime(chunk.Time)
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	corr := s.eventCorrelationLocked("chunk", chunk.Index)
	attrs := baseObservationAttributes(s.observer, corr, cloneMetadata(chunk.Metadata))
	attrs["stream.chunk.index"] = chunk.Index
	summaryAttrs, summaryRecords := streamChunkSummaryAttributes(s.observer, chunk.OutputSummary)
	for key, value := range summaryAttrs {
		attrs[key] = value
	}
	s.events = append(s.events, streamEventObservation(corr, "stream.chunk", eventTime, "ok", attrs, summaryRecords, nil))
	s.mu.Unlock()
}

func (s *Stream) FirstToken(first StreamFirstToken) {
	if s == nil {
		return
	}
	eventTime := observationTime(first.Time)
	s.mu.Lock()
	if s.ended || s.firstTokenEmitted {
		s.mu.Unlock()
		return
	}
	latency := first.Latency
	if latency == 0 {
		latency = eventTime.Sub(s.start)
	}
	if latency < 0 {
		latency = 0
	}
	latencyMS := int64(latency / time.Millisecond)
	s.firstTokenEmitted = true
	s.firstTokenLatency = latencyMS
	corr := s.eventCorrelationLocked("first_token", 0)
	attrs := baseObservationAttributes(s.observer, corr, cloneMetadata(first.Metadata))
	attrs["genai.latency.first_token_ms"] = latencyMS
	s.events = append(s.events, streamEventObservation(corr, "stream.first_token", eventTime, "ok", attrs, nil, nil))
	s.mu.Unlock()
}

func (s *Stream) End(end StreamEnd) {
	if s == nil {
		return
	}
	s.finish("ok", observationTime(end.EndTime), nil, end.Usage, end.OutputSummary, end.Metadata)
}

func (s *Stream) Error(event StreamError) {
	if s == nil {
		return
	}
	status := "error"
	retryable := event.Retryable
	if event.Canceled {
		status = "canceled"
		retryable = false
	}
	err := terminalObservationError("stream", firstNonEmpty(event.Classification, status), event.Err, retryable)
	s.finish(status, observationTime(event.EndTime), &err, event.PartialUsage, event.OutputSummary, event.Metadata)
}

func (s *Stream) finish(status string, endTime time.Time, obsErr *ObservationError, usage TokenUsage, output Summary, metadata Metadata) {
	s.mu.Lock()
	if s.ended {
		s.mu.Unlock()
		return
	}
	s.ended = true
	attrs := cloneObservationAttributes(s.attrs)
	addMetadataAttributes(attrs, metadata)
	if endTime.Before(s.start) {
		endTime = s.start
	}
	duration := endTime.Sub(s.start)
	attrs["genai.latency.total_ms"] = int64(duration / time.Millisecond)
	if s.firstTokenEmitted {
		attrs["genai.latency.first_token_ms"] = s.firstTokenLatency
	}
	addTokenUsageAttributes(attrs, usage)
	records := clonePublicRedaction(s.redact)
	summaryAttrs, summaryRecords := modelOutputSummaryAttributes(s.observer, output)
	for key, value := range summaryAttrs {
		attrs[key] = value
	}
	records = append(records, summaryRecords...)
	if obsErr != nil {
		addModelErrorAttributes(attrs, *obsErr, status == "canceled")
	}
	if status == "canceled" {
		cancellationObs := s.cancellationEventLocked(endTime, obsErr)
		s.events = append(s.events, cancellationObs)
	}
	observation := Observation{
		ID:            s.corr.ObservationID,
		ParentID:      s.corr.ParentObservationID,
		TraceID:       s.corr.TraceID,
		Kind:          "stream",
		Name:          s.name,
		Status:        status,
		Timestamp:     s.start,
		Duration:      duration,
		DurationKnown: true,
		Attributes:    attrs,
		Events:        cloneObservations(s.events),
		Redaction:     clonePublicRedaction(records),
		Error:         cloneObservationErrorPtr(obsErr),
	}
	ctx := s.ctx
	observer := s.observer
	s.mu.Unlock()

	exportObservation(ctx, observer, observation)
}

func (s *Stream) cancellationEventLocked(at time.Time, obsErr *ObservationError) Observation {
	corr := s.eventCorrelationLocked("cancellation", 0)
	attrs := baseObservationAttributes(s.observer, corr, nil)
	addStringAttr(attrs, "cancellation.reason", firstNonEmpty(errorClassification(obsErr), "canceled"))
	if obsErr != nil {
		addModelErrorAttributes(attrs, *obsErr, true)
	}
	return streamEventObservation(corr, "cancellation", at, "canceled", attrs, nil, cloneObservationErrorPtr(obsErr))
}

func (s *Stream) eventCorrelationLocked(suffix string, index int64) Correlation {
	corr := s.corr
	corr.ParentObservationID = s.corr.ObservationID
	if corr.ObservationID != "" {
		if suffix == "chunk" {
			corr.ObservationID = fmt.Sprintf("%s.%s.%d", s.corr.ObservationID, suffix, index)
		} else {
			corr.ObservationID = fmt.Sprintf("%s.%s", s.corr.ObservationID, suffix)
		}
	}
	return corr
}

func streamEventObservation(corr Correlation, name string, timestamp time.Time, status string, attrs map[string]any, records []RedactionRecord, obsErr *ObservationError) Observation {
	return Observation{
		ID:         corr.ObservationID,
		ParentID:   corr.ParentObservationID,
		TraceID:    corr.TraceID,
		Kind:       name,
		Name:       name,
		Status:     status,
		Timestamp:  timestamp,
		Attributes: attrs,
		Redaction:  clonePublicRedaction(records),
		Error:      cloneObservationErrorPtr(obsErr),
	}
}

func streamChunkSummaryAttributes(observer *Observer, summary Summary) (map[string]any, []RedactionRecord) {
	return modelSummaryAttributes(observer, "stream.chunk.summary", redaction.OutputSummary, summary)
}

func errorClassification(err *ObservationError) string {
	if err == nil {
		return ""
	}
	return err.Classification
}
