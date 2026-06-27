package einoobs

import (
	"context"
	"time"

	"github.com/mattsp1290/eino-obs/internal/redaction"
)

type RetryEvent struct {
	Correlation    Correlation
	Attempt        int64
	MaxAttempts    int64
	Classification string
	Reason         string
	Time           time.Time
	Metadata       Metadata
}

type CompactionEvent struct {
	Correlation   Correlation
	Reason        string
	BeforeTokens  int64
	AfterTokens   int64
	DroppedTokens int64
	Time          time.Time
	Summary       Summary
	Metadata      Metadata
}

type InterruptEvent struct {
	Correlation Correlation
	Reason      string
	Status      string
	Time        time.Time
	Metadata    Metadata
}

type ResumeEvent struct {
	Correlation Correlation
	Reason      string
	Status      string
	Time        time.Time
	Metadata    Metadata
}

type CancellationEvent struct {
	Correlation    Correlation
	Operation      string
	Classification string
	Reason         string
	Err            error
	Time           time.Time
	Metadata       Metadata
}

type ErrorEvent struct {
	Correlation    Correlation
	Operation      string
	Classification string
	Err            error
	Retryable      bool
	Dropped        bool
	Time           time.Time
	Metadata       Metadata
}

func (o *Observer) Retry(ctx context.Context, event RetryEvent) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	attrs["retry.attempt"] = event.Attempt
	if event.MaxAttempts > 0 {
		attrs["retry.max_attempts"] = event.MaxAttempts
	}
	reason := firstNonEmpty(event.Reason, event.Classification, "retry")
	addStringAttr(attrs, "retry.reason", reason)
	addStringAttr(attrs, "error.classification", event.Classification)
	observation := lifecycleEventObservation(corr, "retry", "ok", observationTime(event.Time), attrs, nil, nil)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func (o *Observer) Compaction(ctx context.Context, event CompactionEvent) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	addStringAttr(attrs, "compaction.reason", firstNonEmpty(event.Reason, "compaction"))
	if event.BeforeTokens > 0 {
		attrs["compaction.before_tokens"] = event.BeforeTokens
	}
	if event.AfterTokens > 0 {
		attrs["compaction.after_tokens"] = event.AfterTokens
	}
	if event.DroppedTokens > 0 {
		attrs["compaction.dropped_tokens"] = event.DroppedTokens
	}
	var records []RedactionRecord
	summaryAttrs, summaryRecords := compactionSummaryAttributes(o, event.Summary)
	for key, value := range summaryAttrs {
		attrs[key] = value
	}
	records = append(records, summaryRecords...)
	observation := lifecycleEventObservation(corr, "compaction", "ok", observationTime(event.Time), attrs, records, nil)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func (o *Observer) Interrupt(ctx context.Context, event InterruptEvent) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	addStringAttr(attrs, "interrupt.reason", firstNonEmpty(event.Reason, "interrupt"))
	addStringAttr(attrs, "interrupt.status", firstNonEmpty(event.Status, "interrupted"))
	observation := lifecycleEventObservation(corr, "interrupt", "ok", observationTime(event.Time), attrs, nil, nil)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func (o *Observer) Resume(ctx context.Context, event ResumeEvent) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	addStringAttr(attrs, "resume.reason", firstNonEmpty(event.Reason, "resume"))
	addStringAttr(attrs, "resume.status", firstNonEmpty(event.Status, "resumed"))
	observation := lifecycleEventObservation(corr, "resume", "ok", observationTime(event.Time), attrs, nil, nil)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func (o *Observer) Cancellation(ctx context.Context, event CancellationEvent) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	classification := firstNonEmpty(event.Classification, "canceled")
	obsErr := terminalObservationError(firstNonEmpty(event.Operation, "lifecycle"), classification, event.Err, false)
	addStringAttr(attrs, "cancellation.reason", firstNonEmpty(event.Reason, classification))
	addModelErrorAttributes(attrs, obsErr, true)
	observation := lifecycleEventObservation(corr, "cancellation", "canceled", observationTime(event.Time), attrs, nil, &obsErr)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func (o *Observer) Error(ctx context.Context, event ErrorEvent) {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, event.Correlation)
	attrs := baseObservationAttributes(o, corr, cloneMetadata(event.Metadata))
	obsErr := normalizeObservationError(
		firstNonEmpty(event.Operation, "lifecycle"),
		firstNonEmpty(event.Classification, "error"),
		event.Err,
		event.Retryable,
		event.Dropped,
	)
	addModelErrorAttributes(attrs, obsErr, false)
	observation := lifecycleEventObservation(corr, "error", "error", observationTime(event.Time), attrs, nil, &obsErr)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func lifecycleEventObservation(corr Correlation, name string, status string, timestamp time.Time, attrs map[string]any, records []RedactionRecord, obsErr *ObservationError) Observation {
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

func compactionSummaryAttributes(observer *Observer, summary Summary) (map[string]any, []RedactionRecord) {
	if summary.Name == "" && summary.Text == "" && len(summary.Fields) == 0 {
		return nil, nil
	}
	opts := RedactionOptions{}
	if observer != nil {
		opts = observer.Config().Redaction
	}
	attrs, records, err := redaction.SummaryAttributes(
		"compaction.summary",
		"compaction.summary",
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
