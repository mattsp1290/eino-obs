package einoobs

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/mattsp1290/eino-obs/internal/model"
)

func TestModelCallEndExportsProviderUsageLatencyRedactionAndCorrelation(t *testing.T) {
	observer := New(Config{
		Redaction: RedactionOptions{CaptureInputSummary: true, CaptureOutputSummary: true, MaxSummaryBytes: 4},
	})
	start := time.Date(2026, 6, 27, 3, 0, 0, 0, time.FixedZone("offset", -4*60*60))
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		AgentID:             "agent-1",
		Provider:            "ctx-provider",
		Model:               "ctx-model",
		TraceID:             "trace-1",
		ParentObservationID: "run-obs",
	})

	call := observer.StartModelCall(ctx, ModelCallStart{
		Correlation:   Correlation{ObservationID: "model-span"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		Name:          "chat.completions",
		StartTime:     start,
		RetryAttempt:  2,
		InputSummary:  Summary{Name: "request", Text: "hello world", Fields: map[string]string{"kind": "chat"}},
		Metadata:      Metadata{"phase": "start"},
	})
	call.End(ModelCallEnd{
		EndTime:       start.Add(1500 * time.Millisecond),
		Usage:         TokenUsage{InputTokens: 100, OutputTokens: 40, TotalTokens: 140, ReasoningTokens: 8, CachedInputTokens: 12},
		OutputSummary: Summary{Name: "response", Text: "assistant output"},
		Metadata:      Metadata{"phase": "end"},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	validateNormalizedModelCall(t, got)
	if got.ID != "model-span" || got.ParentID != "run-obs" || got.TraceID != "trace-1" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Kind != "model_call" || got.Name != "chat.completions" || got.Status != "ok" {
		t.Fatalf("shape = kind:%q name:%q status:%q", got.Kind, got.Name, got.Status)
	}
	if !got.Timestamp.Equal(start.UTC()) || got.Duration != 1500*time.Millisecond || !got.DurationKnown {
		t.Fatalf("timing = timestamp:%s duration:%s known:%v", got.Timestamp, got.Duration, got.DurationKnown)
	}
	if got.Attributes["genai.provider"] != "openai" ||
		got.Attributes["genai.model"] != "gpt-example" ||
		got.Attributes["genai.retry.attempt"] != int64(2) ||
		got.Attributes["genai.latency.total_ms"] != int64(1500) {
		t.Fatalf("model attrs = %#v", got.Attributes)
	}
	for key, want := range map[string]int64{
		"genai.usage.input_tokens":        100,
		"genai.usage.output_tokens":       40,
		"genai.usage.total_tokens":        140,
		"genai.usage.reasoning_tokens":    8,
		"genai.usage.cached_input_tokens": 12,
	} {
		if got.Attributes[key] != want {
			t.Fatalf("%s = %#v, want %d; attrs=%#v", key, got.Attributes[key], want, got.Attributes)
		}
	}
	if got.Attributes["correlation.session_id"] != "session-1" ||
		got.Attributes["correlation.run_id"] != "run-1" ||
		got.Attributes["correlation.agent_id"] != "agent-1" ||
		got.Attributes["metadata.phase"] != "end" {
		t.Fatalf("correlation/metadata attrs = %#v", got.Attributes)
	}
	if got.Attributes["genai.request.summary"] != "hell" ||
		got.Attributes["genai.request.summary.name"] != "request" ||
		got.Attributes["genai.request.summary.fields.kind"] != "chat" ||
		got.Attributes["genai.response.summary"] != "assi" {
		t.Fatalf("summary attrs = %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "genai.request.summary.text", "summary_truncated")
	assertPublicRedactionRecord(t, got.Redaction, "genai.response.summary.text", "summary_truncated")

	call.End(ModelCallEnd{EndTime: start.Add(time.Hour)})
	if snapshot := observer.Snapshot(); snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("second End exported again: %#v", snapshot)
	}
}

func TestModelCallErrorExportsRetryableAndCanceledFailures(t *testing.T) {
	observer := New(Config{})
	start := time.Now()

	failed := observer.StartModelCall(context.Background(), ModelCallStart{
		Correlation:   Correlation{ObservationID: "failed-model", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		StartTime:     start,
	})
	failed.Error(ModelCallError{
		Err:            errSentinel{},
		Classification: "rate_limit",
		Retryable:      true,
		EndTime:        start.Add(time.Second),
		PartialUsage:   TokenUsage{InputTokens: 10, OutputTokens: 2},
	})

	canceled := observer.StartModelCall(context.Background(), ModelCallStart{
		Correlation:   Correlation{ObservationID: "canceled-model", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		StartTime:     start,
	})
	canceled.Error(ModelCallError{
		Err:            context.Canceled,
		Classification: "canceled",
		Canceled:       true,
		Retryable:      true,
		EndTime:        start.Add(2 * time.Second),
		PartialUsage:   TokenUsage{InputTokens: 10},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Observations) != 2 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 2/2", snapshot.ExportCount, len(snapshot.Observations))
	}
	gotFailed := snapshot.Observations[0]
	validateNormalizedModelCall(t, gotFailed)
	if gotFailed.Status != "error" || gotFailed.Error == nil ||
		gotFailed.Error.Operation != "model_call" ||
		gotFailed.Error.Classification != "rate_limit" ||
		!gotFailed.Error.Retryable {
		t.Fatalf("failed model error = status:%q error:%#v", gotFailed.Status, gotFailed.Error)
	}
	if gotFailed.Attributes["error.operation"] != "model_call" ||
		gotFailed.Attributes["error.classification"] != "rate_limit" ||
		gotFailed.Attributes["error.retryable"] != true ||
		gotFailed.Attributes["genai.usage.input_tokens"] != int64(10) ||
		gotFailed.Attributes["genai.usage.output_tokens"] != int64(2) {
		t.Fatalf("failed attrs = %#v", gotFailed.Attributes)
	}

	gotCanceled := snapshot.Observations[1]
	validateNormalizedModelCall(t, gotCanceled)
	if gotCanceled.Status != "canceled" || gotCanceled.Error == nil ||
		gotCanceled.Error.Operation != "model_call" ||
		gotCanceled.Error.Classification != "canceled" ||
		gotCanceled.Error.Retryable {
		t.Fatalf("canceled model error = status:%q error:%#v", gotCanceled.Status, gotCanceled.Error)
	}
	if gotCanceled.Attributes["error.retryable"] != false ||
		gotCanceled.Attributes["error.canceled"] != true ||
		gotCanceled.Attributes["genai.usage.input_tokens"] != int64(10) {
		t.Fatalf("canceled attrs = %#v", gotCanceled.Attributes)
	}
}

func TestModelCallUsesCorrelationProviderModelAndOmitsUnknownUsage(t *testing.T) {
	observer := New(Config{})
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		Provider: "anthropic",
		Model:    "claude-example",
		TraceID:  "trace-1",
	})

	observer.StartModelCall(ctx, ModelCallStart{
		Correlation: Correlation{ObservationID: "model-span"},
	}).End(ModelCallEnd{})

	got := observer.Snapshot().Observations[0]
	validateNormalizedModelCall(t, got)
	if got.Attributes["genai.provider"] != "anthropic" || got.Attributes["genai.model"] != "claude-example" {
		t.Fatalf("provider/model attrs = %#v", got.Attributes)
	}
	if _, ok := got.Attributes["genai.usage.input_tokens"]; ok {
		t.Fatalf("unknown usage was emitted: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["genai.retry.attempt"]; ok {
		t.Fatalf("unknown retry attempt was emitted: %#v", got.Attributes)
	}
}

func TestModelCallRequiresProviderAndModel(t *testing.T) {
	for _, tt := range []struct {
		name          string
		correlation   Correlation
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
			ctx := ContextWithCorrelation(context.Background(), tt.correlation)

			call := observer.StartModelCall(ctx, ModelCallStart{
				Correlation:   Correlation{ObservationID: "model-span", TraceID: "trace-1"},
				ProviderModel: tt.providerModel,
			})
			call.End(ModelCallEnd{})

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

func TestModelCallUsesMixedExplicitAndContextProviderModel(t *testing.T) {
	for _, tt := range []struct {
		name          string
		correlation   Correlation
		providerModel ProviderModel
		wantProvider  string
		wantModel     string
	}{
		{
			name:          "explicit provider context model",
			correlation:   Correlation{Provider: "ctx-provider", Model: "ctx-model"},
			providerModel: ProviderModel{Provider: "explicit-provider"},
			wantProvider:  "explicit-provider",
			wantModel:     "ctx-model",
		},
		{
			name:          "context provider explicit model",
			correlation:   Correlation{Provider: "ctx-provider", Model: "ctx-model"},
			providerModel: ProviderModel{Model: "explicit-model"},
			wantProvider:  "ctx-provider",
			wantModel:     "explicit-model",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			observer := New(Config{})
			ctx := ContextWithCorrelation(context.Background(), tt.correlation)
			observer.StartModelCall(ctx, ModelCallStart{
				Correlation:   Correlation{ObservationID: "model-span", TraceID: "trace-1"},
				ProviderModel: tt.providerModel,
			}).End(ModelCallEnd{})

			got := observer.Snapshot().Observations[0]
			validateNormalizedModelCall(t, got)
			if got.Attributes["genai.provider"] != tt.wantProvider || got.Attributes["genai.model"] != tt.wantModel {
				t.Fatalf("provider/model attrs = %#v", got.Attributes)
			}
		})
	}
}

func TestModelCallKnownZeroUsageIsEmitted(t *testing.T) {
	observer := New(Config{})
	observer.StartModelCall(context.Background(), ModelCallStart{
		Correlation:   Correlation{ObservationID: "model-span", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
	}).End(ModelCallEnd{
		Usage: TokenUsage{
			InputTokensKnown:       true,
			OutputTokensKnown:      true,
			TotalTokensKnown:       true,
			ReasoningTokensKnown:   true,
			CachedInputTokensKnown: true,
		},
	})

	got := observer.Snapshot().Observations[0]
	validateNormalizedModelCall(t, got)
	for _, key := range []string{
		"genai.usage.input_tokens",
		"genai.usage.output_tokens",
		"genai.usage.total_tokens",
		"genai.usage.reasoning_tokens",
		"genai.usage.cached_input_tokens",
	} {
		if got.Attributes[key] != int64(0) {
			t.Fatalf("%s = %#v, want explicit zero; attrs=%#v", key, got.Attributes[key], got.Attributes)
		}
	}
}

func TestModelCallRedactsDisabledAndSensitiveSummaries(t *testing.T) {
	observer := New(Config{Redaction: RedactionOptions{MaxSummaryBytes: 64}})
	observer.StartModelCall(context.Background(), ModelCallStart{
		Correlation:   Correlation{ObservationID: "model-span", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		InputSummary:  Summary{Name: "request", Text: "hello"},
	}).End(ModelCallEnd{
		OutputSummary: Summary{Name: "response", Text: "world"},
	})

	got := observer.Snapshot().Observations[0]
	validateNormalizedModelCall(t, got)
	if _, ok := got.Attributes["genai.request.summary"]; ok {
		t.Fatalf("disabled input summary emitted attrs: %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "genai.request.summary", "summary_disabled")
	assertPublicRedactionRecord(t, got.Redaction, "genai.response.summary", "summary_disabled")

	observer = New(Config{Redaction: RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 64}})
	observer.StartModelCall(context.Background(), ModelCallStart{
		Correlation:   Correlation{ObservationID: "sensitive-model-span", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
		InputSummary:  Summary{Name: "request", Text: "hello", Fields: map[string]string{"api_key": "secret", "safe": "value"}},
	}).End(ModelCallEnd{})

	got = observer.Snapshot().Observations[0]
	validateNormalizedModelCall(t, got)
	if got.Attributes["genai.request.summary.fields.safe"] != "value" {
		t.Fatalf("safe input summary field missing: %#v", got.Attributes)
	}
	if _, ok := got.Attributes["genai.request.summary.fields.api_key"]; ok {
		t.Fatalf("sensitive input summary field emitted attrs: %#v", got.Attributes)
	}
	assertPublicRedactionRecord(t, got.Redaction, "genai.request.summary.fields.api_key", "default_omitted")
}

func TestModelCallHelpersIgnoreCanceledCallerContextAndAfterShutdown(t *testing.T) {
	observer := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	observer.StartModelCall(ctx, ModelCallStart{
		Correlation:   Correlation{ObservationID: "model-span", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
	}).End(ModelCallEnd{})
	if snapshot := observer.Snapshot(); snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("canceled caller context snapshot = %#v", snapshot)
	}

	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	observer.StartModelCall(context.Background(), ModelCallStart{
		Correlation:   Correlation{ObservationID: "post-shutdown", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
	}).End(ModelCallEnd{})
	if snapshot := observer.Snapshot(); snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("post-shutdown exported model call: %#v", snapshot)
	}
}

func TestModelCallConcurrentTerminalMethodsExportOnce(t *testing.T) {
	observer := New(Config{})
	call := observer.StartModelCall(context.Background(), ModelCallStart{
		Correlation:   Correlation{ObservationID: "model-span", TraceID: "trace-1"},
		ProviderModel: ProviderModel{Provider: "openai", Model: "gpt-example"},
	})

	var wg sync.WaitGroup
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			if i%2 == 0 {
				call.End(ModelCallEnd{})
				return
			}
			call.Error(ModelCallError{Err: errSentinel{}, Classification: "rate_limit", Retryable: true})
		}(i)
	}
	wg.Wait()

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("concurrent terminal exports = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
}

func validateNormalizedModelCall(t *testing.T, observation Observation) {
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
		t.Fatalf("normalized model call validation failed: %v", err)
	}
}
