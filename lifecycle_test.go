package einoobs

import (
	"context"
	"testing"
	"time"
)

func TestRetryEventExportsAttemptClassificationAndCorrelation(t *testing.T) {
	observer := New(Config{Service: "svc"})
	at := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		TraceID:       "trace-1",
		ObservationID: "run-1",
		RunID:         "run-1",
	})

	observer.Retry(ctx, RetryEvent{
		Attempt:        2,
		MaxAttempts:    5,
		Classification: "rate_limit",
		Time:           at,
		Metadata:       Metadata{"stage": "model"},
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "retry" || got.Name != "retry" || got.Status != "ok" || !got.Timestamp.Equal(at) {
		t.Fatalf("retry observation = %#v", got)
	}
	if got.TraceID != "trace-1" || got.ID != "run-1" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Attributes["retry.attempt"] != int64(2) ||
		got.Attributes["retry.max_attempts"] != int64(5) ||
		got.Attributes["retry.reason"] != "rate_limit" ||
		got.Attributes["error.classification"] != "rate_limit" ||
		got.Attributes["metadata.stage"] != "model" {
		t.Fatalf("retry attributes = %#v", got.Attributes)
	}
}

func TestCompactionEventExportsSummaryWithoutRawPromptPayload(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{
			CaptureInputSummary: true,
			MaxSummaryBytes:     4,
		},
	})
	at := time.Date(2026, 6, 26, 12, 5, 0, 0, time.UTC)

	observer.Compaction(context.Background(), CompactionEvent{
		Correlation:  Correlation{TraceID: "trace-1", ObservationID: "run-1"},
		Reason:       "context_window",
		BeforeTokens: 1000,
		AfterTokens:  250,
		Summary: Summary{
			Text: "raw prompt text that must be summarized",
			Fields: map[string]string{
				"safe":          "abcdef",
				"Authorization": "secret",
			},
		},
		Time: at,
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "compaction" || got.Name != "compaction" || !got.Timestamp.Equal(at) {
		t.Fatalf("compaction observation = %#v", got)
	}
	if got.Attributes["compaction.reason"] != "context_window" ||
		got.Attributes["compaction.before_tokens"] != int64(1000) ||
		got.Attributes["compaction.after_tokens"] != int64(250) {
		t.Fatalf("compaction attributes = %#v", got.Attributes)
	}
	if got.Attributes["compaction.summary"] != "raw " ||
		got.Attributes["compaction.summary.fields.safe"] != "abcd" {
		t.Fatalf("summary attributes = %#v", got.Attributes)
	}
	if _, ok := got.Attributes["prompt.text"]; ok {
		t.Fatalf("raw prompt payload leaked: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["compaction.summary.fields.Authorization"]; ok {
		t.Fatalf("sensitive summary field leaked: %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "compaction.summary.text", "summary_truncated")
	assertPublicRedactionRecord(t, got.Redaction, "compaction.summary.fields.safe", "summary_truncated")
	assertPublicRedactionRecord(t, got.Redaction, "compaction.summary.fields.Authorization", "default_omitted")
}

func TestCompactionSummaryRespectsDisabledInputSummaryRedaction(t *testing.T) {
	observer := New(Config{})

	observer.Compaction(context.Background(), CompactionEvent{
		Reason: "context_window",
		Summary: Summary{
			Text:   "raw prompt text",
			Fields: map[string]string{"safe": "value"},
		},
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if _, ok := got.Attributes["compaction.summary"]; ok {
		t.Fatalf("disabled compaction summary was retained: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["compaction.summary.fields.safe"]; ok {
		t.Fatalf("disabled compaction summary field was retained: %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "compaction.summary", "summary_disabled")
}
