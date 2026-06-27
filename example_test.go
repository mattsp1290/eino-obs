package einoobs_test

import (
	"context"
	"fmt"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
)

func ExampleObserver_minimalSession() {
	observer := einoobs.New(einoobs.Config{Service: "example-agent"})
	ctx := einoobs.ContextWithCorrelation(context.Background(), einoobs.Correlation{
		TraceID:       "trace-1",
		ObservationID: "session-1",
		SessionID:     "session-1",
	})

	session := observer.StartSession(ctx, einoobs.SessionStart{Name: "chat"})
	run := session.StartRun(einoobs.RunStart{Name: "answer"})
	run.End(einoobs.RunEnd{})
	session.End(einoobs.SessionEnd{})

	fmt.Println(len(observer.Snapshot().Observations))
	// Output: 2
}

func ExampleObserver_modelStreamToolLifecycle() {
	observer := einoobs.New(einoobs.Config{
		Service: "example-agent",
		Redaction: einoobs.RedactionOptions{
			CaptureInputSummary:  true,
			CaptureOutputSummary: true,
			MaxSummaryBytes:      32,
		},
	})
	start := time.Date(2026, 6, 27, 12, 0, 0, 0, time.UTC)
	ctx := einoobs.ContextWithCorrelation(context.Background(), einoobs.Correlation{
		TraceID:             "trace-1",
		ObservationID:       "session-1",
		ParentObservationID: "session-1",
		SessionID:           "session-1",
		RunID:               "run-1",
		Provider:            "openai",
		Model:               "gpt-example",
	})

	session := observer.StartSession(ctx, einoobs.SessionStart{
		Correlation: einoobs.Correlation{ObservationID: "session-1"},
		Name:        "chat",
		StartTime:   start,
	})
	run := session.StartRun(einoobs.RunStart{
		Correlation: einoobs.Correlation{ObservationID: "run-1"},
		Name:        "answer",
		StartTime:   start.Add(10 * time.Millisecond),
	})

	call := observer.StartModelCall(ctx, einoobs.ModelCallStart{
		Correlation:  einoobs.Correlation{ObservationID: "model-call-1", ParentObservationID: "run-1"},
		Name:         "chat.completion",
		StartTime:    start.Add(20 * time.Millisecond),
		InputSummary: einoobs.Summary{Name: "request", Text: "short user request summary"},
	})
	call.End(einoobs.ModelCallEnd{
		EndTime:       start.Add(120 * time.Millisecond),
		Usage:         einoobs.TokenUsage{InputTokens: 12, OutputTokens: 8, TotalTokens: 20},
		OutputSummary: einoobs.Summary{Name: "response", Text: "short assistant response summary"},
	})

	stream := observer.StartStream(ctx, einoobs.StreamStart{
		Correlation: einoobs.Correlation{ObservationID: "stream-1", ParentObservationID: "run-1"},
		Name:        "chat.stream",
		StartTime:   start.Add(130 * time.Millisecond),
	})
	stream.FirstToken(einoobs.StreamFirstToken{Time: start.Add(180 * time.Millisecond)})
	stream.Chunk(einoobs.StreamChunk{
		Index:         0,
		Time:          start.Add(190 * time.Millisecond),
		OutputSummary: einoobs.Summary{Name: "delta", Text: "first delta"},
	})
	stream.End(einoobs.StreamEnd{
		EndTime: start.Add(260 * time.Millisecond),
		Usage:   einoobs.TokenUsage{InputTokens: 12, OutputTokens: 8, TotalTokens: 20},
	})

	observer.ToolRegistered(ctx, einoobs.ToolRegistered{
		Correlation: einoobs.Correlation{ObservationID: "tool-registered-1"},
		ToolName:    "search",
		ToolKind:    "server",
		Time:        start.Add(270 * time.Millisecond),
	})
	tool := observer.StartToolCall(ctx, einoobs.ToolCallStart{
		Correlation:  einoobs.Correlation{ObservationID: "tool-call-1", ParentObservationID: "run-1"},
		ToolCallID:   "tool-call-1",
		ToolName:     "search",
		StartTime:    start.Add(280 * time.Millisecond),
		InputSummary: einoobs.Summary{Name: "query", Fields: map[string]string{"q": "weather"}},
	})
	tool.End(einoobs.ToolCallEnd{
		EndTime:       start.Add(340 * time.Millisecond),
		OutputSummary: einoobs.Summary{Name: "result", Text: "sunny"},
	})

	observer.Retry(ctx, einoobs.RetryEvent{Attempt: 1, Reason: "rate_limit", Time: start.Add(350 * time.Millisecond)})
	observer.Compaction(ctx, einoobs.CompactionEvent{Reason: "context_window", BeforeTokens: 1200, AfterTokens: 400, Time: start.Add(360 * time.Millisecond)})
	run.End(einoobs.RunEnd{EndTime: start.Add(400 * time.Millisecond)})
	session.End(einoobs.SessionEnd{EndTime: start.Add(450 * time.Millisecond)})

	for _, observation := range observer.Snapshot().Observations {
		fmt.Println(observation.Kind, observation.Status)
	}
	// Output:
	// model_call ok
	// stream ok
	// tool.registered ok
	// tool_call ok
	// retry ok
	// compaction ok
	// run ok
	// session ok
}
