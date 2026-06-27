package main

import (
	"context"
	"fmt"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
)

func main() {
	observer := einoobs.New(einoobs.Config{
		Service: "example-agent",
		Env:     "local",
		Redaction: einoobs.RedactionOptions{
			CaptureInputSummary: true,
			MaxSummaryBytes:     64,
		},
	})

	ctx := einoobs.ContextWithCorrelation(context.Background(), einoobs.Correlation{
		TraceID:       "example-trace",
		ObservationID: "example-session",
		SessionID:     "example-session",
	})
	session := observer.StartSession(ctx, einoobs.SessionStart{Name: "example session"})
	run := session.StartRun(einoobs.RunStart{Name: "answer question"})
	observer.Retry(ctx, einoobs.RetryEvent{
		Attempt:        1,
		MaxAttempts:    3,
		Classification: "rate_limit",
		Reason:         "model request throttled",
		Time:           time.Now().UTC(),
	})
	observer.Compaction(ctx, einoobs.CompactionEvent{
		Reason:       "context_window",
		BeforeTokens: 1200,
		AfterTokens:  400,
		Summary: einoobs.Summary{
			Text: "user asked for a concise answer",
			Fields: map[string]string{
				"task": "summarize",
			},
		},
	})
	run.End(einoobs.RunEnd{})
	session.End(einoobs.SessionEnd{})

	if err := observer.Flush(context.Background()); err != nil {
		panic(err)
	}
	snapshot := observer.Snapshot()
	fmt.Printf("captured %d observations\n", len(snapshot.Observations))
	for _, observation := range snapshot.Observations {
		fmt.Printf("%s %s\n", observation.Kind, observation.Name)
	}
}
