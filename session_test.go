package einoobs

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestSessionEndExportsCorrelationAndMetadata(t *testing.T) {
	observer := New(Config{Service: "svc", Env: "test", Version: "v1"})
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "session-ctx",
		AgentID:             "agent-1",
		ThreadID:            "thread-1",
		TraceID:             "trace-ctx",
		ParentObservationID: "parent-ctx",
	})
	start := time.Date(2026, 6, 26, 10, 0, 0, 0, time.FixedZone("offset", -4*60*60))
	end := start.Add(25 * time.Millisecond)
	metadata := Metadata{"tenant": "acme"}

	session := observer.StartSession(ctx, SessionStart{
		Correlation: Correlation{ObservationID: "session-obs", SessionID: "session-explicit"},
		Name:        "chat-session",
		StartTime:   start,
		Metadata:    metadata,
	})
	metadata["tenant"] = "mutated"
	session.End(SessionEnd{EndTime: end, Metadata: Metadata{"outcome": "accepted"}})
	session.End(SessionEnd{})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.ID != "session-obs" || got.ParentID != "parent-ctx" || got.TraceID != "trace-ctx" {
		t.Fatalf("identity = %#v", got)
	}
	if got.Kind != "session" || got.Name != "chat-session" || got.Status != "ok" {
		t.Fatalf("shape = kind:%q name:%q status:%q", got.Kind, got.Name, got.Status)
	}
	if !got.Timestamp.Equal(start.UTC()) || got.Duration != 25*time.Millisecond || !got.DurationKnown {
		t.Fatalf("timing = timestamp:%s duration:%s known:%t", got.Timestamp, got.Duration, got.DurationKnown)
	}
	if got.Attributes["service.name"] != "svc" || got.Attributes["service.env"] != "test" || got.Attributes["service.version"] != "v1" {
		t.Fatalf("service attributes = %#v", got.Attributes)
	}
	if got.Attributes["correlation.session_id"] != "session-explicit" ||
		got.Attributes["correlation.agent_id"] != "agent-1" ||
		got.Attributes["correlation.thread_id"] != "thread-1" {
		t.Fatalf("correlation attributes = %#v", got.Attributes)
	}
	if got.Attributes["metadata.tenant"] != "acme" || got.Attributes["metadata.outcome"] != "accepted" {
		t.Fatalf("metadata attributes = %#v", got.Attributes)
	}
}

func TestSessionStartRunParentsRunToSession(t *testing.T) {
	observer := New(Config{})
	ctx := ContextWithCorrelation(context.Background(), Correlation{
		SessionID:           "stale-session",
		RunID:               "run-ctx",
		AgentID:             "agent-1",
		TraceID:             "stale-trace",
		ParentObservationID: "stale-parent",
	})
	session := observer.StartSession(ctx, SessionStart{Correlation: Correlation{
		SessionID:     "session-1",
		ObservationID: "session-obs",
		TraceID:       "trace-session",
	}})

	run := session.StartRun(RunStart{
		Correlation: Correlation{ObservationID: "run-obs", RunID: "run-explicit"},
		Name:        "answer-user-message",
		Metadata:    Metadata{"workflow": "chat"},
	})
	run.End(RunEnd{})
	session.End(SessionEnd{})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(snapshot.Observations))
	}
	gotRun := snapshot.Observations[0]
	if gotRun.Kind != "run" || gotRun.ID != "run-obs" || gotRun.ParentID != "session-obs" || gotRun.TraceID != "trace-session" {
		t.Fatalf("run identity/shape = %#v", gotRun)
	}
	if gotRun.Attributes["correlation.session_id"] != "session-1" ||
		gotRun.Attributes["correlation.run_id"] != "run-explicit" ||
		gotRun.Attributes["correlation.agent_id"] != "agent-1" ||
		gotRun.Attributes["metadata.workflow"] != "chat" {
		t.Fatalf("run attributes = %#v", gotRun.Attributes)
	}
	gotSession := snapshot.Observations[1]
	if gotSession.Kind != "session" || gotSession.ID != "session-obs" || gotSession.TraceID != "trace-session" {
		t.Fatalf("session identity/shape = %#v", gotSession)
	}
}

func TestRunErrorExportsTerminalFailureOnce(t *testing.T) {
	observer := New(Config{})
	cause := errors.New("runtime failed")
	run := observer.StartRun(context.Background(), RunStart{
		Correlation: Correlation{ObservationID: "run-obs", TraceID: "trace-1"},
		Name:        "runtime",
	})

	run.Error(RunError{Err: cause, Classification: "runtime", Retryable: true})
	run.End(RunEnd{})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.Status != "error" || got.Error == nil {
		t.Fatalf("terminal error = status:%q error:%#v", got.Status, got.Error)
	}
	if got.Error.Operation != "run" || got.Error.Classification != "runtime" || !got.Error.Retryable {
		t.Fatalf("error fields = %#v", got.Error)
	}
}

func TestRunCanceledOmitsUnsetIdentity(t *testing.T) {
	observer := New(Config{})
	run := observer.StartRun(context.Background(), RunStart{})

	run.Error(RunError{Canceled: true, Classification: "canceled"})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 1 {
		t.Fatalf("observations = %d, want 1", len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	if got.ID != "" || got.TraceID != "" || got.ParentID != "" {
		t.Fatalf("identity = id:%q trace:%q parent:%q, want omitted", got.ID, got.TraceID, got.ParentID)
	}
	if got.Status != "canceled" || got.Error == nil || got.Error.Retryable {
		t.Fatalf("canceled run = status:%q error:%#v", got.Status, got.Error)
	}
}

func TestTerminalExportIgnoresStartContextCancellation(t *testing.T) {
	observer := New(Config{})
	ctx, cancel := context.WithCancel(context.Background())
	run := observer.StartRun(ctx, RunStart{
		Correlation: Correlation{ObservationID: "run-obs", TraceID: "trace-1"},
	})
	cancel()

	run.End(RunEnd{})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	if got := snapshot.Observations[0]; got.ID != "run-obs" || got.Status != "ok" {
		t.Fatalf("observation = %#v", got)
	}
}

func TestHelperAfterShutdownIsInert(t *testing.T) {
	var handled []ObservationError
	observer := New(Config{
		ErrorHandler: func(_ context.Context, err ObservationError) {
			handled = append(handled, err)
		},
	})
	if err := observer.Shutdown(context.Background()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}

	run := observer.StartRun(context.Background(), RunStart{Correlation: Correlation{ObservationID: "run-obs", TraceID: "trace-1"}})
	run.End(RunEnd{})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 0 || len(snapshot.Observations) != 0 {
		t.Fatalf("post-shutdown snapshot = exports:%d observations:%d", snapshot.ExportCount, len(snapshot.Observations))
	}
	if len(handled) != 0 {
		t.Fatalf("post-shutdown handler calls = %#v", handled)
	}
}
