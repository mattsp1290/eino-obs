package einoobs

import (
	"context"
	"errors"
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

func TestInterruptEventExportsCorrelationReasonStatusAndRedactsMetadata(t *testing.T) {
	observer := New(Config{})
	at := time.Date(2026, 6, 26, 12, 10, 0, 0, time.UTC)
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		TraceID:             "trace-1",
		ObservationID:       "run-1",
		ParentObservationID: "session-1",
		SessionID:           "session-1",
		RunID:               "run-1",
	})

	observer.Interrupt(ctx, InterruptEvent{
		Reason: "human_feedback",
		Status: "paused",
		Time:   at,
		Metadata: Metadata{
			"safe":    "visible",
			"api_key": "secret",
		},
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "interrupt" || got.Name != "interrupt" || got.Status != "ok" || !got.Timestamp.Equal(at) {
		t.Fatalf("interrupt observation = %#v", got)
	}
	if got.TraceID != "trace-1" || got.ID != "run-1" || got.ParentID != "session-1" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Attributes["correlation.session_id"] != "session-1" ||
		got.Attributes["correlation.run_id"] != "run-1" ||
		got.Attributes["interrupt.reason"] != "human_feedback" ||
		got.Attributes["interrupt.status"] != "paused" ||
		got.Attributes["metadata.safe"] != "visible" {
		t.Fatalf("interrupt attributes = %#v", got.Attributes)
	}
	if _, ok := got.Attributes["metadata.api_key"]; ok {
		t.Fatalf("sensitive metadata leaked: %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "metadata.api_key", "default_omitted")
}

func TestResumeEventExportsCorrelationReasonStatusAndRedactsMetadata(t *testing.T) {
	observer := New(Config{})
	at := time.Date(2026, 6, 26, 12, 15, 0, 0, time.UTC)

	observer.Resume(context.Background(), ResumeEvent{
		Correlation: Correlation{
			TraceID:             "trace-2",
			ObservationID:       "run-2",
			ParentObservationID: "session-2",
			SessionID:           "session-2",
			RunID:               "run-2",
		},
		Reason: "user_approved",
		Status: "running",
		Time:   at,
		Metadata: Metadata{
			"safe":          "visible",
			"Authorization": "secret",
		},
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "resume" || got.Name != "resume" || got.Status != "ok" || !got.Timestamp.Equal(at) {
		t.Fatalf("resume observation = %#v", got)
	}
	if got.TraceID != "trace-2" || got.ID != "run-2" || got.ParentID != "session-2" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Attributes["correlation.session_id"] != "session-2" ||
		got.Attributes["correlation.run_id"] != "run-2" ||
		got.Attributes["resume.reason"] != "user_approved" ||
		got.Attributes["resume.status"] != "running" ||
		got.Attributes["metadata.safe"] != "visible" {
		t.Fatalf("resume attributes = %#v", got.Attributes)
	}
	if _, ok := got.Attributes["metadata.Authorization"]; ok {
		t.Fatalf("sensitive metadata leaked: %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "metadata.Authorization", "default_omitted")
}

func TestCancellationEventExportsCanceledErrorAndCorrelation(t *testing.T) {
	observer := New(Config{})
	at := time.Date(2026, 6, 26, 12, 20, 0, 0, time.UTC)
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		TraceID:             "trace-3",
		ObservationID:       "run-3",
		ParentObservationID: "session-3",
		SessionID:           "session-3",
		RunID:               "run-3",
	})

	observer.Cancellation(ctx, CancellationEvent{
		Operation:      "run",
		Classification: "user_canceled",
		Reason:         "human_feedback",
		Err:            context.Canceled,
		Time:           at,
		Metadata: Metadata{
			"safe":    "visible",
			"api_key": "secret",
		},
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "cancellation" || got.Name != "cancellation" || got.Status != "canceled" || !got.Timestamp.Equal(at) {
		t.Fatalf("cancellation observation = %#v", got)
	}
	if got.TraceID != "trace-3" || got.ID != "run-3" || got.ParentID != "session-3" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Error == nil ||
		got.Error.Operation != "run" ||
		got.Error.Classification != "user_canceled" ||
		got.Error.Retryable {
		t.Fatalf("cancellation error = %#v", got.Error)
	}
	if got.Attributes["cancellation.reason"] != "human_feedback" ||
		got.Attributes["error.operation"] != "run" ||
		got.Attributes["error.classification"] != "user_canceled" ||
		got.Attributes["error.canceled"] != true ||
		got.Attributes["error.retryable"] != false ||
		got.Attributes["metadata.safe"] != "visible" {
		t.Fatalf("cancellation attributes = %#v", got.Attributes)
	}
	if _, ok := got.Attributes["metadata.api_key"]; ok {
		t.Fatalf("sensitive metadata leaked: %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "metadata.api_key", "default_omitted")
}

func TestErrorEventExportsGenericErrorFieldsAndExistingObservationError(t *testing.T) {
	observer := New(Config{})
	at := time.Date(2026, 6, 26, 12, 25, 0, 0, time.UTC)
	cause := ObservationError{
		Operation:      "tool_call",
		Classification: "tool_timeout",
		Err:            errors.New("safe timeout"),
		Retryable:      true,
		Dropped:        true,
	}

	observer.Error(context.Background(), ErrorEvent{
		Correlation: Correlation{
			TraceID:             "trace-4",
			ObservationID:       "run-4.error",
			ParentObservationID: "run-4",
			RunID:               "run-4",
		},
		Operation:      "ignored",
		Classification: "ignored",
		Err:            cause,
		Retryable:      false,
		Time:           at,
		Metadata:       Metadata{"phase": "tool"},
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Kind != "error" || got.Name != "error" || got.Status != "error" || !got.Timestamp.Equal(at) {
		t.Fatalf("error observation = %#v", got)
	}
	if got.TraceID != "trace-4" || got.ID != "run-4.error" || got.ParentID != "run-4" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Error == nil ||
		got.Error.Operation != "tool_call" ||
		got.Error.Classification != "tool_timeout" ||
		!got.Error.Retryable ||
		!got.Error.Dropped {
		t.Fatalf("error fields = %#v", got.Error)
	}
	if got.Attributes["error.operation"] != "tool_call" ||
		got.Attributes["error.classification"] != "tool_timeout" ||
		got.Attributes["error.retryable"] != true ||
		got.Attributes["error.dropped"] != true ||
		got.Attributes["metadata.phase"] != "tool" {
		t.Fatalf("error attributes = %#v", got.Attributes)
	}
}

func TestLifecycleCancellationAndErrorHelpersTolerateNilInputs(t *testing.T) {
	var observer *Observer
	observer.Cancellation(nil, CancellationEvent{})
	observer.Error(nil, ErrorEvent{})

	observer = New(Config{})
	observer.Cancellation(nil, CancellationEvent{})
	observer.Error(nil, ErrorEvent{Err: errors.New("safe")})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(snapshot.Observations))
	}
	if snapshot.Observations[0].Kind != "cancellation" || snapshot.Observations[0].Status != "canceled" {
		t.Fatalf("default cancellation = %#v", snapshot.Observations[0])
	}
	if snapshot.Observations[1].Kind != "error" || snapshot.Observations[1].Status != "error" {
		t.Fatalf("default error = %#v", snapshot.Observations[1])
	}
}

func TestLifecycleEventHelpersShareSchemaRedactionAndCorrelation(t *testing.T) {
	observer := New(Config{
		Service: "svc",
		Redaction: RedactionOptions{
			CaptureInputSummary: true,
			MaxSummaryBytes:     6,
		},
	})
	base := time.Date(2026, 6, 26, 13, 0, 0, 0, time.UTC)
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		TraceID:             "trace-life",
		ObservationID:       "run-life",
		ParentObservationID: "session-life",
		SessionID:           "session-life",
		RunID:               "run-life",
		AgentID:             "agent-life",
	})
	metadata := Metadata{
		"phase":         "life",
		"Authorization": "secret",
	}

	observer.Interrupt(ctx, InterruptEvent{
		Reason:   "human_feedback",
		Status:   "paused",
		Time:     base,
		Metadata: metadata,
	})
	observer.Resume(ctx, ResumeEvent{
		Reason:   "user_approved",
		Status:   "running",
		Time:     base.Add(time.Second),
		Metadata: metadata,
	})
	observer.Retry(ctx, RetryEvent{
		Attempt:        2,
		MaxAttempts:    3,
		Classification: "rate_limit",
		Time:           base.Add(2 * time.Second),
		Metadata:       metadata,
	})
	observer.Compaction(ctx, CompactionEvent{
		Reason:        "context_window",
		BeforeTokens:  100,
		AfterTokens:   40,
		DroppedTokens: 60,
		Time:          base.Add(3 * time.Second),
		Summary: Summary{
			Text:   "caller summary text",
			Fields: map[string]string{"safe": "abcdefghi", "api_key": "secret"},
		},
		Metadata: metadata,
	})
	observer.Cancellation(ctx, CancellationEvent{
		Operation:      "run",
		Classification: "user_canceled",
		Reason:         "human_feedback",
		Time:           base.Add(4 * time.Second),
		Metadata:       metadata,
	})
	observer.Error(ctx, ErrorEvent{
		Operation:      "run",
		Classification: "runtime",
		Err:            errors.New("safe runtime"),
		Retryable:      true,
		Time:           base.Add(5 * time.Second),
		Metadata:       metadata,
	})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 6 {
		t.Fatalf("observations = %d, want 6", len(snapshot.Observations))
	}
	wantKinds := []string{"interrupt", "resume", "retry", "compaction", "cancellation", "error"}
	for i, want := range wantKinds {
		got := snapshot.Observations[i]
		if got.Kind != want || got.Name != want {
			t.Fatalf("observation %d kind/name = %q/%q, want %q", i, got.Kind, got.Name, want)
		}
		if got.TraceID != "trace-life" || got.ID != "run-life" || got.ParentID != "session-life" {
			t.Fatalf("observation %d identity = %#v", i, got)
		}
		if got.Attributes["service.name"] != "svc" ||
			got.Attributes["correlation.session_id"] != "session-life" ||
			got.Attributes["correlation.run_id"] != "run-life" ||
			got.Attributes["correlation.agent_id"] != "agent-life" ||
			got.Attributes["metadata.phase"] != "life" {
			t.Fatalf("observation %d shared attributes = %#v", i, got.Attributes)
		}
		if _, ok := got.Attributes["metadata.Authorization"]; ok {
			t.Fatalf("observation %d leaked sensitive metadata: %#v", i, got.Attributes)
		}
		assertPublicRedactionRecord(t, got.Redaction, "metadata.Authorization", "default_omitted")
	}
	if snapshot.Observations[2].Attributes["retry.attempt"] != int64(2) ||
		snapshot.Observations[2].Attributes["retry.reason"] != "rate_limit" {
		t.Fatalf("retry attributes = %#v", snapshot.Observations[2].Attributes)
	}
	compaction := snapshot.Observations[3]
	if compaction.Attributes["compaction.reason"] != "context_window" ||
		compaction.Attributes["compaction.summary"] != "caller" ||
		compaction.Attributes["compaction.summary.fields.safe"] != "abcdef" ||
		compaction.Attributes["compaction.dropped_tokens"] != int64(60) {
		t.Fatalf("compaction attributes = %#v", compaction.Attributes)
	}
	assertPublicRedactionRecord(t, compaction.Redaction, "compaction.summary.text", "summary_truncated")
	assertPublicRedactionRecord(t, compaction.Redaction, "compaction.summary.fields.api_key", "default_omitted")

	cancellation := snapshot.Observations[4]
	if cancellation.Status != "canceled" ||
		cancellation.Error == nil ||
		cancellation.Attributes["cancellation.reason"] != "human_feedback" ||
		cancellation.Attributes["error.canceled"] != true ||
		cancellation.Attributes["error.retryable"] != false {
		t.Fatalf("cancellation observation = status:%q error:%#v attrs:%#v", cancellation.Status, cancellation.Error, cancellation.Attributes)
	}
	genericErr := snapshot.Observations[5]
	if genericErr.Status != "error" ||
		genericErr.Error == nil ||
		genericErr.Error.Operation != "run" ||
		genericErr.Error.Classification != "runtime" ||
		!genericErr.Error.Retryable ||
		genericErr.Attributes["error.retryable"] != true {
		t.Fatalf("error observation = status:%q error:%#v attrs:%#v", genericErr.Status, genericErr.Error, genericErr.Attributes)
	}
}
