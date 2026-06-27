package einoobs

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/mattsp1290/eino-obs/internal/model"
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
	validateNormalizedPublicSpan(t, got)
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
	runStart := time.Date(2026, 6, 26, 10, 1, 0, 0, time.UTC)
	runEnd := runStart.Add(250 * time.Millisecond)
	sessionEnd := runEnd.Add(50 * time.Millisecond)
	session := observer.StartSession(ctx, SessionStart{
		Correlation: Correlation{
			SessionID:     "session-1",
			ObservationID: "session-obs",
			TraceID:       "trace-session",
		},
		StartTime: runStart.Add(-100 * time.Millisecond),
	})

	run := session.StartRun(RunStart{
		Correlation: Correlation{ObservationID: "run-obs", RunID: "run-explicit"},
		Name:        "answer-user-message",
		StartTime:   runStart,
		Metadata:    Metadata{"workflow": "chat"},
	})
	run.End(RunEnd{EndTime: runEnd})
	session.End(SessionEnd{EndTime: sessionEnd})

	snapshot := observer.Snapshot()
	if len(snapshot.Observations) != 2 {
		t.Fatalf("observations = %d, want 2", len(snapshot.Observations))
	}
	gotRun := snapshot.Observations[0]
	validateNormalizedPublicSpan(t, gotRun)
	if gotRun.Kind != "run" || gotRun.ID != "run-obs" || gotRun.ParentID != "session-obs" || gotRun.TraceID != "trace-session" {
		t.Fatalf("run identity/shape = %#v", gotRun)
	}
	if gotRun.Duration != 250*time.Millisecond || !gotRun.DurationKnown {
		t.Fatalf("run timing = duration:%s known:%t", gotRun.Duration, gotRun.DurationKnown)
	}
	if gotRun.Attributes["correlation.session_id"] != "session-1" ||
		gotRun.Attributes["correlation.run_id"] != "run-explicit" ||
		gotRun.Attributes["correlation.agent_id"] != "agent-1" ||
		gotRun.Attributes["metadata.workflow"] != "chat" {
		t.Fatalf("run attributes = %#v", gotRun.Attributes)
	}
	gotSession := snapshot.Observations[1]
	validateNormalizedPublicSpan(t, gotSession)
	if gotSession.Kind != "session" || gotSession.ID != "session-obs" || gotSession.TraceID != "trace-session" {
		t.Fatalf("session identity/shape = %#v", gotSession)
	}
}

func TestSessionErrorExportsFailureAndCancellation(t *testing.T) {
	observer := New(Config{})
	start := time.Date(2026, 6, 27, 6, 20, 0, 0, time.UTC)

	failed := observer.StartSession(context.Background(), SessionStart{
		Correlation: Correlation{
			ObservationID:       "session-failed",
			ParentObservationID: "root",
			TraceID:             "trace-session",
			SessionID:           "session-1",
		},
		StartTime: start,
	})
	failed.Error(SessionError{
		Err:            errors.New("safe session failure"),
		Classification: "admission_failed",
		Metadata:       Metadata{"outcome": "failed"},
	})
	failed.End(SessionEnd{})

	canceled := observer.StartSession(context.Background(), SessionStart{
		Correlation: Correlation{
			ObservationID: "session-canceled",
			TraceID:       "trace-session",
			SessionID:     "session-2",
		},
		StartTime: start.Add(time.Second),
	})
	canceled.Error(SessionError{
		Err:            context.Canceled,
		Classification: "canceled",
		Canceled:       true,
		Metadata:       Metadata{"outcome": "canceled"},
	})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 2 || len(snapshot.Observations) != 2 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 2/2", snapshot.ExportCount, len(snapshot.Observations))
	}
	gotFailed := snapshot.Observations[0]
	validateNormalizedPublicSpan(t, gotFailed)
	if gotFailed.ID != "session-failed" ||
		gotFailed.ParentID != "root" ||
		gotFailed.TraceID != "trace-session" ||
		gotFailed.Status != "error" ||
		gotFailed.Error == nil ||
		gotFailed.Error.Operation != "session" ||
		gotFailed.Error.Classification != "admission_failed" ||
		gotFailed.Error.Retryable ||
		gotFailed.Attributes["metadata.outcome"] != "failed" {
		t.Fatalf("failed session = status:%q error:%#v attrs:%#v", gotFailed.Status, gotFailed.Error, gotFailed.Attributes)
	}

	gotCanceled := snapshot.Observations[1]
	validateNormalizedPublicSpan(t, gotCanceled)
	if gotCanceled.ID != "session-canceled" ||
		gotCanceled.TraceID != "trace-session" ||
		gotCanceled.Status != "canceled" ||
		gotCanceled.Error == nil ||
		gotCanceled.Error.Operation != "session" ||
		gotCanceled.Error.Classification != "canceled" ||
		gotCanceled.Error.Retryable ||
		gotCanceled.Attributes["metadata.outcome"] != "canceled" {
		t.Fatalf("canceled session = status:%q error:%#v attrs:%#v", gotCanceled.Status, gotCanceled.Error, gotCanceled.Attributes)
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
	validateNormalizedPublicSpan(t, got)
	if got.Status != "error" || got.Error == nil {
		t.Fatalf("terminal error = status:%q error:%#v", got.Status, got.Error)
	}
	if got.Error.Operation != "run" || got.Error.Classification != "runtime" || !got.Error.Retryable {
		t.Fatalf("error fields = %#v", got.Error)
	}
}

func TestRunCanceledWithIdentityValidatesNormalizedTerminalFields(t *testing.T) {
	observer := New(Config{})
	run := observer.StartRun(context.Background(), RunStart{
		Correlation: Correlation{ObservationID: "run-obs", TraceID: "trace-1"},
		Name:        "runtime",
	})

	run.Error(RunError{Err: context.Canceled, Canceled: true, Classification: "canceled", Retryable: true})

	snapshot := observer.Snapshot()
	if snapshot.ExportCount != 1 || len(snapshot.Observations) != 1 {
		t.Fatalf("snapshot count = exports:%d observations:%d, want 1/1", snapshot.ExportCount, len(snapshot.Observations))
	}
	got := snapshot.Observations[0]
	validateNormalizedPublicSpan(t, got)
	if got.Status != "canceled" || got.Error == nil ||
		got.Error.Operation != "run" ||
		got.Error.Classification != "canceled" ||
		got.Error.Retryable {
		t.Fatalf("canceled run = status:%q error:%#v", got.Status, got.Error)
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

func validateNormalizedPublicSpan(t *testing.T, observation Observation) {
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
		canceled := observation.Status == "canceled"
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
		t.Fatalf("normalized public span validation failed: %v", err)
	}
}
