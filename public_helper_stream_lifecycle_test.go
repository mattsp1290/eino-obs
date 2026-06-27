package einoobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestPublicStreamingAndLifecycleHelpersWorkflow(t *testing.T) {
	observer := New(Config{
		Service: "svc",
		Redaction: RedactionOptions{
			CaptureInputSummary:  true,
			CaptureOutputSummary: true,
			MaxSummaryBytes:      5,
		},
	})
	start := time.Date(2026, 6, 27, 6, 0, 0, 0, time.UTC)
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-helpers",
		RunID:               "run-helpers",
		AgentID:             "agent-helpers",
		Provider:            "openai",
		Model:               "gpt-example",
		TraceID:             "trace-helpers",
		ParentObservationID: "run-obs",
	})

	success := observer.StartStream(ctx, StreamStart{
		Correlation:  Correlation{ObservationID: "stream-success"},
		Name:         "chat.stream",
		StartTime:    start,
		InputSummary: Summary{Name: "request", Text: "hello sensitive request"},
		Metadata:     Metadata{"phase": "start", "Authorization": "secret"},
	})
	success.Chunk(StreamChunk{
		Index:         0,
		Time:          start.Add(100 * time.Millisecond),
		OutputSummary: Summary{Name: "delta", Text: "first delta text"},
	})
	success.FirstToken(StreamFirstToken{Time: start.Add(175 * time.Millisecond)})
	success.End(StreamEnd{
		EndTime:       start.Add(900 * time.Millisecond),
		Usage:         TokenUsage{InputTokens: 11, OutputTokens: 7, TotalTokens: 18},
		OutputSummary: Summary{Name: "response", Text: "assistant response text"},
		Metadata:      Metadata{"phase": "done"},
	})

	failed := observer.StartStream(ctx, StreamStart{
		Correlation: Correlation{ObservationID: "stream-failed"},
		StartTime:   start.Add(time.Second),
	})
	failed.Error(StreamError{
		Err:            errors.New("safe rate limit"),
		Classification: "rate_limit",
		Retryable:      true,
		EndTime:        start.Add(1300 * time.Millisecond),
		PartialUsage:   TokenUsage{InputTokens: 5, OutputTokens: 1},
	})

	canceled := observer.StartStream(ctx, StreamStart{
		Correlation: Correlation{ObservationID: "stream-canceled"},
		StartTime:   start.Add(2 * time.Second),
	})
	canceled.Error(StreamError{
		Err:            context.Canceled,
		Classification: "canceled",
		Canceled:       true,
		Retryable:      true,
		EndTime:        start.Add(2200 * time.Millisecond),
		PartialUsage:   TokenUsage{InputTokens: 3, OutputTokens: 0, OutputTokensKnown: true},
	})

	observer.Retry(ctx, RetryEvent{
		Attempt:        2,
		MaxAttempts:    4,
		Classification: "rate_limit",
		Time:           start.Add(3 * time.Second),
		Metadata:       Metadata{"phase": "retry", "Authorization": "secret"},
	})
	observer.Compaction(ctx, CompactionEvent{
		Reason:        "context_window",
		BeforeTokens:  100,
		AfterTokens:   40,
		DroppedTokens: 60,
		Time:          start.Add(4 * time.Second),
		Summary:       Summary{Text: "compaction summary text", Fields: map[string]string{"safe": "abcdef", "api_key": "secret"}},
	})
	observer.Interrupt(ctx, InterruptEvent{
		Reason:   "human_feedback",
		Status:   "paused",
		Time:     start.Add(5 * time.Second),
		Metadata: Metadata{"phase": "pause", "Authorization": "secret"},
	})
	observer.Resume(ctx, ResumeEvent{
		Reason:   "user_approved",
		Status:   "running",
		Time:     start.Add(6 * time.Second),
		Metadata: Metadata{"phase": "run", "Authorization": "secret"},
	})
	observer.Cancellation(ctx, CancellationEvent{
		Operation:      "run",
		Classification: "user_canceled",
		Reason:         "human_feedback",
		Time:           start.Add(7 * time.Second),
	})
	observer.Error(ctx, ErrorEvent{
		Operation:      "run",
		Classification: "runtime",
		Err:            errors.New("safe runtime"),
		Retryable:      true,
		Time:           start.Add(8 * time.Second),
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 9 || len(snapshot.Observations) != 9 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 9/9", snapshot.ExportCount, len(snapshot.Observations))
	}

	gotSuccess := snapshot.Observations[0]
	if gotSuccess.Kind != "stream" || gotSuccess.Status != "ok" || gotSuccess.Name != "chat.stream" {
		t.Fatalf("success stream shape = %#v", gotSuccess)
	}
	if gotSuccess.ID != "stream-success" ||
		gotSuccess.ParentID != "run-obs" ||
		gotSuccess.TraceID != "trace-helpers" ||
		!gotSuccess.Timestamp.Equal(start) ||
		gotSuccess.Duration != 900*time.Millisecond ||
		!gotSuccess.DurationKnown {
		t.Fatalf("success stream envelope = %#v", gotSuccess)
	}
	if gotSuccess.Attributes["genai.provider"] != "openai" ||
		gotSuccess.Attributes["genai.model"] != "gpt-example" ||
		gotSuccess.Attributes["genai.latency.first_token_ms"] != int64(175) ||
		gotSuccess.Attributes["genai.latency.total_ms"] != int64(900) ||
		gotSuccess.Attributes["genai.usage.input_tokens"] != int64(11) ||
		gotSuccess.Attributes["genai.usage.output_tokens"] != int64(7) ||
		gotSuccess.Attributes["genai.response.summary"] != "assis" ||
		gotSuccess.Attributes["genai.response.summary.name"] != "response" ||
		gotSuccess.Attributes["metadata.phase"] != "done" ||
		gotSuccess.Attributes["correlation.session_id"] != "session-helpers" {
		t.Fatalf("success stream attrs = %#v", gotSuccess.Attributes)
	}
	if _, ok := gotSuccess.Attributes["metadata.Authorization"]; ok {
		t.Fatalf("sensitive stream metadata leaked: %#v", gotSuccess.Attributes)
	}
	assertPublicRedactionRecord(t, gotSuccess.Redaction, "genai.request.summary.text", "summary_truncated")
	assertPublicRedactionRecord(t, gotSuccess.Redaction, "genai.response.summary.text", "summary_truncated")
	assertPublicRedactionRecord(t, gotSuccess.Redaction, "metadata.Authorization", "default_omitted")
	if len(gotSuccess.Events) != 2 ||
		gotSuccess.Events[0].Kind != "stream.chunk" ||
		gotSuccess.Events[0].ID != "stream-success.chunk.0" ||
		gotSuccess.Events[0].ParentID != "stream-success" ||
		gotSuccess.Events[0].TraceID != "trace-helpers" ||
		gotSuccess.Events[0].Attributes["stream.chunk.index"] != int64(0) ||
		gotSuccess.Events[0].Attributes["stream.chunk.summary"] != "first" ||
		gotSuccess.Events[1].Kind != "stream.first_token" ||
		gotSuccess.Events[1].ID != "stream-success.first_token" ||
		gotSuccess.Events[1].ParentID != "stream-success" ||
		gotSuccess.Events[1].Attributes["genai.latency.first_token_ms"] != int64(175) {
		t.Fatalf("success stream events = %#v", gotSuccess.Events)
	}

	gotFailed := snapshot.Observations[1]
	if gotFailed.Status != "error" ||
		gotFailed.Error == nil ||
		gotFailed.Error.Classification != "rate_limit" ||
		!gotFailed.Error.Retryable ||
		gotFailed.Attributes["genai.usage.input_tokens"] != int64(5) ||
		gotFailed.Attributes["genai.usage.output_tokens"] != int64(1) {
		t.Fatalf("failed stream = status:%q error:%#v attrs:%#v", gotFailed.Status, gotFailed.Error, gotFailed.Attributes)
	}

	gotCanceled := snapshot.Observations[2]
	if gotCanceled.Status != "canceled" ||
		gotCanceled.Error == nil ||
		gotCanceled.Error.Retryable ||
		gotCanceled.Attributes["error.canceled"] != true ||
		gotCanceled.Attributes["genai.usage.input_tokens"] != int64(3) ||
		gotCanceled.Attributes["genai.usage.output_tokens"] != int64(0) ||
		len(gotCanceled.Events) != 1 ||
		gotCanceled.Events[0].Kind != "cancellation" ||
		gotCanceled.Events[0].Attributes["error.retryable"] != false {
		t.Fatalf("canceled stream = status:%q error:%#v attrs:%#v events:%#v", gotCanceled.Status, gotCanceled.Error, gotCanceled.Attributes, gotCanceled.Events)
	}

	retry := snapshot.Observations[3]
	if retry.Kind != "retry" ||
		retry.TraceID != "trace-helpers" ||
		retry.ParentID != "run-obs" ||
		!retry.Timestamp.Equal(start.Add(3*time.Second)) ||
		retry.Attributes["retry.attempt"] != int64(2) ||
		retry.Attributes["retry.max_attempts"] != int64(4) ||
		retry.Attributes["retry.reason"] != "rate_limit" ||
		retry.Attributes["metadata.phase"] != "retry" {
		t.Fatalf("retry lifecycle = %#v", retry)
	}
	if _, ok := retry.Attributes["metadata.Authorization"]; ok {
		t.Fatalf("retry sensitive metadata leaked: %#v", retry.Attributes)
	}
	assertPublicRedactionRecord(t, retry.Redaction, "metadata.Authorization", "default_omitted")
	compaction := snapshot.Observations[4]
	if compaction.Kind != "compaction" ||
		compaction.Attributes["compaction.reason"] != "context_window" ||
		compaction.Attributes["compaction.summary"] != "compa" ||
		compaction.Attributes["compaction.summary.fields.safe"] != "abcde" {
		t.Fatalf("compaction lifecycle = %#v", compaction)
	}
	assertPublicRedactionRecord(t, compaction.Redaction, "compaction.summary.fields.api_key", "default_omitted")
	interrupt := snapshot.Observations[5]
	resume := snapshot.Observations[6]
	if interrupt.Kind != "interrupt" ||
		interrupt.Attributes["interrupt.reason"] != "human_feedback" ||
		interrupt.Attributes["interrupt.status"] != "paused" ||
		interrupt.Attributes["metadata.phase"] != "pause" ||
		resume.Kind != "resume" ||
		resume.Attributes["resume.reason"] != "user_approved" ||
		resume.Attributes["resume.status"] != "running" ||
		resume.Attributes["metadata.phase"] != "run" {
		t.Fatalf("interrupt/resume lifecycle = interrupt:%#v resume:%#v", interrupt, resume)
	}
	if _, ok := interrupt.Attributes["metadata.Authorization"]; ok {
		t.Fatalf("interrupt sensitive metadata leaked: %#v", interrupt.Attributes)
	}
	if _, ok := resume.Attributes["metadata.Authorization"]; ok {
		t.Fatalf("resume sensitive metadata leaked: %#v", resume.Attributes)
	}
	assertPublicRedactionRecord(t, interrupt.Redaction, "metadata.Authorization", "default_omitted")
	assertPublicRedactionRecord(t, resume.Redaction, "metadata.Authorization", "default_omitted")
	cancellation := snapshot.Observations[7]
	if cancellation.Kind != "cancellation" ||
		cancellation.Status != "canceled" ||
		cancellation.TraceID != "trace-helpers" ||
		cancellation.ParentID != "run-obs" ||
		cancellation.Error == nil ||
		cancellation.Error.Operation != "run" ||
		cancellation.Error.Classification != "user_canceled" ||
		cancellation.Attributes["cancellation.reason"] != "human_feedback" ||
		cancellation.Attributes["error.operation"] != "run" ||
		cancellation.Attributes["error.classification"] != "user_canceled" ||
		cancellation.Attributes["error.canceled"] != true {
		t.Fatalf("cancellation lifecycle = %#v", cancellation)
	}
	genericErr := snapshot.Observations[8]
	if genericErr.Kind != "error" ||
		genericErr.Status != "error" ||
		genericErr.Error == nil ||
		genericErr.Error.Operation != "run" ||
		genericErr.Error.Classification != "runtime" ||
		!genericErr.Error.Retryable ||
		genericErr.Attributes["error.operation"] != "run" ||
		genericErr.Attributes["error.classification"] != "runtime" {
		t.Fatalf("generic error lifecycle = %#v", genericErr)
	}
}
