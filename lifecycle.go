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
	observation := lifecycleEventObservation(corr, "retry", observationTime(event.Time), attrs, nil)
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
	observation := lifecycleEventObservation(corr, "compaction", observationTime(event.Time), attrs, records)
	exportObservation(context.WithoutCancel(ctx), o, observation)
}

func lifecycleEventObservation(corr Correlation, name string, timestamp time.Time, attrs map[string]any, records []RedactionRecord) Observation {
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
