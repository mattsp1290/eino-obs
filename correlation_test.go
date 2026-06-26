package einoobs

import (
	"context"
	"testing"
)

func TestCorrelationFromContextMissing(t *testing.T) {
	corr, ok := CorrelationFromContext(context.Background())
	if ok {
		t.Fatalf("expected missing correlation")
	}
	if !corr.IsZero() {
		t.Fatalf("expected zero correlation, got %#v", corr)
	}
}

func TestContextWithCorrelationRoundTripAllFields(t *testing.T) {
	want := Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		AgentID:             "agent-1",
		AssistantMessageID:  "assistant-message-1",
		ThreadID:            "thread-1",
		AGUIRunID:           "agui-run-1",
		ToolCallID:          "tool-call-1",
		Provider:            "openai",
		Model:               "gpt-example",
		TraceID:             "trace-1",
		ObservationID:       "obs-1",
		ParentObservationID: "parent-1",
	}

	got, ok := CorrelationFromContext(ContextWithCorrelation(context.Background(), want))
	if !ok {
		t.Fatalf("expected correlation")
	}
	if got != want {
		t.Fatalf("correlation mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestContextWithCorrelationMergesWithExisting(t *testing.T) {
	base := Correlation{
		SessionID:           "session-1",
		RunID:               "run-1",
		AgentID:             "agent-1",
		AssistantMessageID:  "assistant-message-1",
		ThreadID:            "thread-1",
		AGUIRunID:           "agui-run-1",
		ToolCallID:          "tool-call-1",
		Provider:            "openai",
		Model:               "gpt-example",
		TraceID:             "trace-1",
		ObservationID:       "obs-1",
		ParentObservationID: "parent-1",
	}
	ctx := ContextWithCorrelation(context.Background(), base)

	got, ok := CorrelationFromContext(ContextWithCorrelation(ctx, Correlation{
		RunID:               "run-2",
		ToolCallID:          "tool-call-2",
		Model:               "gpt-other",
		ObservationID:       "obs-2",
		ParentObservationID: "parent-2",
	}))
	if !ok {
		t.Fatalf("expected correlation")
	}

	want := base
	want.RunID = "run-2"
	want.ToolCallID = "tool-call-2"
	want.Model = "gpt-other"
	want.ObservationID = "obs-2"
	want.ParentObservationID = "parent-2"

	if got != want {
		t.Fatalf("correlation mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestMergeCorrelationDoesNotClearWithEmptyExplicitFields(t *testing.T) {
	base := Correlation{
		SessionID: "session-1",
		RunID:     "run-1",
		Provider:  "openai",
		Model:     "gpt-example",
		TraceID:   "trace-1",
	}
	explicit := Correlation{RunID: "run-2"}

	got := MergeCorrelation(base, explicit)
	want := Correlation{
		SessionID: "session-1",
		RunID:     "run-2",
		Provider:  "openai",
		Model:     "gpt-example",
		TraceID:   "trace-1",
	}

	if got != want {
		t.Fatalf("correlation mismatch\ngot:  %#v\nwant: %#v", got, want)
	}
}

func TestContextWithCorrelationAllowsNilParent(t *testing.T) {
	got, ok := CorrelationFromContext(ContextWithCorrelation(nil, Correlation{SessionID: "session-1"}))
	if !ok {
		t.Fatalf("expected correlation")
	}
	if got.SessionID != "session-1" {
		t.Fatalf("SessionID = %q, want session-1", got.SessionID)
	}
}
