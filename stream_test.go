package einoobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mattsp1290/eino-obs/internal/model"
)

func TestStreamEndExportsChunksFirstTokenUsageLatencyRedactionAndCorrelation(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true, CaptureOutputSummary: true, MaxSummaryBytes: 4},
	})
	start := time.Date(2026, 6, 27, 5, 0, 0, 0, time.FixedZone("offset", -4*60*60))
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		AgentID:             "agent-1",
		Provider:            "ctx-provider",
		Model:               "ctx-model",
		TraceID:             "trace-1",
		ParentObservationID: "run-obs",
	})

	stream := observer.StartStream(ctx, StreamStart{
		Correlation:   Correlation{ObservationID: "stream-obs"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		Name:          "chat.stream",
		StartTime:     start,
		RetryAttempt:  3,
		InputSummary:  Summary{Name: "request", Text: "hello world"},
		Metadata:      Metadata{"phase": "start"},
	})
	stream.Chunk(StreamChunk{
		Index:         0,
		Time:          start.Add(200 * time.Millisecond),
		OutputSummary: Summary{Name: "delta", Text: "delta text"},
		Metadata:      Metadata{"chunk": "first"},
	})
	stream.FirstToken(StreamFirstToken{Time: start.Add(250 * time.Millisecond)})
	stream.Chunk(StreamChunk{Index: 1, Time: start.Add(300 * time.Millisecond)})
	stream.End(StreamEnd{
		EndTime: start.Add(1500 * time.Millisecond),
		Usage: TokenUsage{
			InputTokens:       100,
			OutputTokens:      80,
			TotalTokens:       180,
			ReasoningTokens:   12,
			CachedInputTokens: 20,
		},
		OutputSummary: Summary{Name: "response", Text: "assistant output"},
		Metadata:      Metadata{"phase": "end"},
	})
	stream.End(StreamEnd{EndTime: start.Add(time.Hour)})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	validateNormalizedStreamSpan(t, got)
	if len(got.Events) != 3 {
		t.Fatalf("events = %d, want 3; observation=%#v", len(got.Events), got)
	}
	chunk0 := got.Events[0]
	validateNormalizedStreamEvent(t, chunk0)
	if chunk0.Kind != "stream.chunk" || chunk0.ID != "stream-obs.chunk.0" || chunk0.ParentID != "stream-obs" || chunk0.TraceID != "trace-1" {
		t.Fatalf("chunk0 identity/shape = %#v", chunk0)
	}
	if chunk0.Attributes["stream.chunk.index"] != int64(0) ||
		chunk0.Attributes["stream.chunk.summary"] != "delt" ||
		chunk0.Attributes["stream.chunk.summary.name"] != "delta" ||
		chunk0.Attributes["correlation.session_id"] != "session-1" ||
		chunk0.Attributes["metadata.chunk"] != "firs" {
		t.Fatalf("chunk0 attrs = %#v", chunk0.Attributes)
	}
	assertPublicRedactionRecord(t, chunk0.Redaction, "stream.chunk.summary.text", "summary_truncated")

	firstToken := got.Events[1]
	validateNormalizedStreamEvent(t, firstToken)
	if firstToken.Kind != "stream.first_token" || firstToken.ID != "stream-obs.first_token" || firstToken.ParentID != "stream-obs" {
		t.Fatalf("first token identity/shape = %#v", firstToken)
	}
	if firstToken.Attributes["genai.latency.first_token_ms"] != int64(250) {
		t.Fatalf("first token attrs = %#v", firstToken.Attributes)
	}

	chunk1 := got.Events[2]
	validateNormalizedStreamEvent(t, chunk1)
	if chunk1.Attributes["stream.chunk.index"] != int64(1) {
		t.Fatalf("chunk1 attrs = %#v", chunk1.Attributes)
	}

	if got.Kind != "stream" || got.Name != "chat.stream" || got.Status != "ok" ||
		got.ID != "stream-obs" || got.ParentID != "run-obs" || got.TraceID != "trace-1" {
		t.Fatalf("stream span identity/shape = %#v", got)
	}
	if !got.Timestamp.Equal(start.UTC()) || got.Duration != 1500*time.Millisecond || !got.DurationKnown {
		t.Fatalf("stream timing = timestamp:%s duration:%s known:%v", got.Timestamp, got.Duration, got.DurationKnown)
	}
	for key, want := range map[string]any{
		"genai.provider":                  "openai",
		"genai.model":                     "gpt-example",
		"genai.retry.attempt":             int64(3),
		"genai.latency.first_token_ms":    int64(250),
		"genai.latency.total_ms":          int64(1500),
		"genai.usage.input_tokens":        int64(100),
		"genai.usage.output_tokens":       int64(80),
		"genai.usage.total_tokens":        int64(180),
		"genai.usage.reasoning_tokens":    int64(12),
		"genai.usage.cached_input_tokens": int64(20),
		"correlation.session_id":          "session-1",
		"correlation.run_id":              "run-1",
		"metadata.phase":                  "end",
		"genai.request.summary":           "hell",
		"genai.response.summary":          "assi",
	} {
		if got.Attributes[key] != want {
			t.Fatalf("%s = %#v, want %#v; attrs=%#v", key, got.Attributes[key], want, got.Attributes)
		}
	}
	assertPublicRedactionRecord(t, got.Redaction, "genai.request.summary.text", "summary_truncated")
	assertPublicRedactionRecord(t, got.Redaction, "genai.response.summary.text", "summary_truncated")

}

func TestStreamErrorExportsPartialFailureAndCancellation(t *testing.T) {
	observer := New(Config{})
	start := time.Now()

	failed := observer.StartStream(context.Background(), StreamStart{
		Correlation:   Correlation{ObservationID: "failed-stream", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		StartTime:     start,
	})
	failed.Error(StreamError{
		Err:            errSentinel{},
		Classification: "rate_limit",
		Retryable:      true,
		EndTime:        start.Add(time.Second),
		PartialUsage:   TokenUsage{InputTokens: 10, OutputTokens: 2},
	})

	canceled := observer.StartStream(context.Background(), StreamStart{
		Correlation:   Correlation{ObservationID: "canceled-stream", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		StartTime:     start,
	})
	canceled.Error(StreamError{
		Err:            context.Canceled,
		Classification: "canceled",
		Canceled:       true,
		Retryable:      true,
		EndTime:        start.Add(2 * time.Second),
		PartialUsage:   TokenUsage{InputTokens: 10, OutputTokens: 0, OutputTokensKnown: true},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Observations) != 2 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 2/2", snapshot.ExportCount, len(snapshot.Observations))
	}
	gotFailed := snapshot.Observations[0]
	validateNormalizedStreamSpan(t, gotFailed)
	if gotFailed.Status != "error" || gotFailed.Error == nil ||
		gotFailed.Error.Operation != "stream" ||
		gotFailed.Error.Classification != "rate_limit" ||
		!gotFailed.Error.Retryable ||
		gotFailed.Attributes["error.retryable"] != true ||
		gotFailed.Attributes["genai.usage.input_tokens"] != int64(10) ||
		gotFailed.Attributes["genai.usage.output_tokens"] != int64(2) {
		t.Fatalf("failed stream = status:%q error:%#v attrs:%#v", gotFailed.Status, gotFailed.Error, gotFailed.Attributes)
	}

	gotCanceled := snapshot.Observations[1]
	validateNormalizedStreamSpan(t, gotCanceled)
	if len(gotCanceled.Events) != 1 {
		t.Fatalf("canceled stream events = %d, want 1; observation=%#v", len(gotCanceled.Events), gotCanceled)
	}
	cancellation := gotCanceled.Events[0]
	validateNormalizedStreamEvent(t, cancellation)
	if cancellation.Kind != "cancellation" || cancellation.Status != "canceled" ||
		cancellation.ID != "canceled-stream.cancellation" ||
		cancellation.ParentID != "canceled-stream" ||
		cancellation.Attributes["cancellation.reason"] != "canceled" ||
		cancellation.Attributes["error.canceled"] != true ||
		cancellation.Attributes["error.retryable"] != false {
		t.Fatalf("cancellation event = %#v", cancellation)
	}

	if gotCanceled.Status != "canceled" || gotCanceled.Error == nil ||
		gotCanceled.Error.Operation != "stream" ||
		gotCanceled.Error.Classification != "canceled" ||
		gotCanceled.Error.Retryable ||
		gotCanceled.Attributes["error.retryable"] != false ||
		gotCanceled.Attributes["error.canceled"] != true ||
		gotCanceled.Attributes["genai.usage.input_tokens"] != int64(10) ||
		gotCanceled.Attributes["genai.usage.output_tokens"] != int64(0) {
		t.Fatalf("canceled stream = status:%q error:%#v attrs:%#v", gotCanceled.Status, gotCanceled.Error, gotCanceled.Attributes)
	}

}

func TestStreamRequiresProviderAndModel(t *testing.T) {
	for _, tt := range []struct {
		name          string
		providerModel ProviderModel
	}{
		{name: "missing both"},
		{name: "missing provider", providerModel: ProviderModel{Model: "gpt-example"}},
		{name: "missing model", providerModel: ProviderModel{Provider: "openai"}},
	} {
		t.Run(tt.name, func(t *testing.T) {
			var handled []ObservationError
			observer := New(Config{
				ErrorHandler: func(_ context.Context, err ObservationError) {
					handled = append(handled, err)
				},
			})

			stream := observer.StartStream(context.Background(), StreamStart{
				Correlation:   Correlation{ObservationID: "stream-obs", TraceID: "trace-1"},
				ProviderModel: tt.providerModel,
			})
			stream.Chunk(StreamChunk{Index: 0})
			stream.FirstToken(StreamFirstToken{})
			stream.End(StreamEnd{})

			snapshot := observer.Snapshot()
			if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
				t.Fatalf("invalid provider/model exported observation: %#v", snapshot)
			}
			if len(handled) != 1 ||
				handled[0].Operation != "record" ||
				handled[0].Classification != "invalid_schema" ||
				!handled[0].Dropped {
				t.Fatalf("handled errors = %#v", handled)
			}
		})
	}
}

func TestStreamHelpersIgnoreCanceledCallerContextAfterShutdownAndConcurrentTerminal(t *testing.T) {
	observer := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	observer.StartStream(ctx, StreamStart{
		Correlation:   Correlation{ObservationID: "stream-obs", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
	}).End(StreamEnd{})
	if snapshot := observer.Snapshot(); snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("canceled caller context snapshot = %#v", snapshot)
	}

	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	observer.StartStream(context.Background(), StreamStart{
		Correlation:   Correlation{ObservationID: "post-shutdown", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
	}).End(StreamEnd{})
	if snapshot := observer.Snapshot(); snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("post-shutdown exported stream: %#v", snapshot)
	}

	observer = New(Config{})
	stream := observer.StartStream(context.Background(), StreamStart{
		Correlation:   Correlation{ObservationID: "concurrent-stream", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
	})
	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				stream.End(StreamEnd{})
				return
			}
			stream.Error(StreamError{Err: errSentinel{}, Classification: "rate_limit", Retryable: true})
		}(i)
	}
	wg.Wait()
	if snapshot := observer.Snapshot(); snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("concurrent terminal exports = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
}

func validateNormalizedStreamSpan(t *testing.T, observation Observation) {
	t.Helper()
	span := model.NewSpan(
		model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
		model.SpanKind(observation.Kind),
		observation.Name,
		observation.Timestamp,
	)
	span.Status = model.Status(observation.Status)
	span.Attributes = model.Attributes(observation.Attributes)
	if observation.DurationKnown {
		span.EndTime = observation.Timestamp.Add(observation.Duration).UTC()
	}
	if observation.Error != nil {
		retryable := observation.Error.Retryable
		dropped := observation.Error.Dropped
		canceled, _ := observation.Attributes["error.canceled"].(bool)
		span.Error = &model.ObservationError{
			Operation:      observation.Error.Operation,
			Classification: observation.Error.Classification,
			Message:        observation.Error.Error(),
			Retryable:      &retryable,
			Canceled:       &canceled,
			Dropped:        &dropped,
		}
	}
	if err := span.Validate(); err != nil {
		t.Fatalf("normalized stream span validation failed: %v", err)
	}
}

func validateNormalizedStreamEvent(t *testing.T, observation Observation) {
	t.Helper()
	event := model.NewEvent(
		model.ObservationIdentity{ID: observation.ID, ParentID: observation.ParentID, TraceID: observation.TraceID},
		model.EventName(observation.Name),
		observation.Timestamp,
	)
	event.Status = model.Status(observation.Status)
	event.Attributes = model.Attributes(observation.Attributes)
	if observation.Error != nil {
		retryable := observation.Error.Retryable
		dropped := observation.Error.Dropped
		canceled, _ := observation.Attributes["error.canceled"].(bool)
		event.Error = &model.ObservationError{
			Operation:      observation.Error.Operation,
			Classification: observation.Error.Classification,
			Message:        observation.Error.Error(),
			Retryable:      &retryable,
			Canceled:       &canceled,
			Dropped:        &dropped,
		}
	}
	if err := event.Validate(); err != nil {
		t.Fatalf("normalized stream event validation failed: %v", err)
	}
}
