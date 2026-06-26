package recorder

import (
	"context"
	"sync"
	"testing"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/internal/model"
)

func TestRecorderRecordsPostRedactionSnapshotAndReset(t *testing.T) {
	rec := New(Config{Redaction: einoobs.RedactionOptions{CaptureInputSummary: true, MaxSummaryBytes: 4}})
	span := model.NewSpan(model.ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, model.SpanKindModelCall, "model", time.Now())
	span.SetAttr("genai.provider", "openai")
	span.SetAttr("genai.model", "gpt-example")
	span.SetAttr("prompt.text", "raw prompt")
	span.SetAttr("genai.request.summary", "hello")
	event := model.NewEvent(model.ObservationIdentity{ID: "event-1", ParentID: "span-1", TraceID: "trace-1"}, model.EventStreamChunk, time.Now())
	event.SetAttr("stream.chunk.index", int64(0))
	event.SetAttr("stream.chunk.summary", "raw delta")
	span.Events = []model.Event{event}

	if err := rec.Record(context.Background(), observationFromSpan(span)); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	snapshot := rec.Snapshot()
	if snapshot.RecordCount != 1 || snapshot.OperationCounts["record"] != 1 {
		t.Fatalf("counts = record:%d ops:%v", snapshot.RecordCount, snapshot.OperationCounts)
	}
	got := snapshot.Recorded[0]
	if _, ok := got.Attributes["prompt.text"]; ok {
		t.Fatalf("raw prompt was retained")
	}
	if got.Attributes["genai.request.summary"] != "hell" {
		t.Fatalf("summary = %q, want hell", got.Attributes["genai.request.summary"])
	}
	if got.Attributes["sequence"] != int64(1) {
		t.Fatalf("sequence = %v, want 1", got.Attributes["sequence"])
	}
	if _, ok := got.Events[0].Attributes["stream.chunk.summary"]; ok {
		t.Fatalf("disabled chunk summary was retained")
	}
	if got.Events[0].Attributes["span_event_sequence"] != int64(1) {
		t.Fatalf("event sequence = %v, want 1", got.Events[0].Attributes["span_event_sequence"])
	}

	snapshot.Recorded[0].Attributes["genai.request.summary"] = "mutated"
	if rec.Snapshot().Recorded[0].Attributes["genai.request.summary"] != "hell" {
		t.Fatalf("snapshot mutation changed recorder state")
	}

	rec.Reset()
	afterReset := rec.Snapshot()
	if len(afterReset.Recorded) != 0 || afterReset.RecordCount != 0 || len(afterReset.OperationCounts) != 0 {
		t.Fatalf("snapshot after reset = %#v", afterReset)
	}
}

func TestRecorderCapacityDropsAndFlushClearsDirty(t *testing.T) {
	rec := New(Config{Capacity: 1})
	first := model.NewSpan(model.ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, model.SpanKindRun, "run", time.Now())
	second := model.NewSpan(model.ObservationIdentity{ID: "span-2", TraceID: "trace-1"}, model.SpanKindRun, "run", time.Now())

	if err := rec.Record(context.Background(), observationFromSpan(first)); err != nil {
		t.Fatalf("first Record() error = %v", err)
	}
	if err := rec.Record(context.Background(), observationFromSpan(second)); err == nil {
		t.Fatalf("second Record() error = nil")
	}
	snapshot := rec.Snapshot()
	if len(snapshot.Recorded) != 1 || len(snapshot.Dropped) != 1 || !snapshot.Dirty {
		t.Fatalf("snapshot after drop = %#v", snapshot)
	}
	if snapshot.LastError == nil || snapshot.LastError.Classification != "capacity" || !snapshot.LastError.Dropped {
		t.Fatalf("last error = %#v", snapshot.LastError)
	}

	if err := rec.Flush(context.Background()); err != nil {
		t.Fatalf("Flush() error = %v", err)
	}
	if rec.Snapshot().Dirty {
		t.Fatalf("dirty state remained after successful flush")
	}
}

func TestRecorderConcurrentRecordSnapshotAndReset(t *testing.T) {
	rec := New(Config{})
	var wg sync.WaitGroup
	for i := 0; i < 25; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			span := model.NewSpan(model.ObservationIdentity{ID: "span", TraceID: "trace"}, model.SpanKindRun, "run", time.Now())
			span.SetAttr("worker", int64(i))
			_ = rec.Record(context.Background(), observationFromSpan(span))
			_ = rec.Snapshot()
		}(i)
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		rec.Reset()
	}()
	wg.Wait()
	_ = rec.Snapshot()
}

func observationFromSpan(span model.Span) einoobs.Observation {
	obs := einoobs.Observation{
		ID:         span.Identity.ID,
		ParentID:   span.Identity.ParentID,
		TraceID:    span.Identity.TraceID,
		Kind:       string(span.Kind),
		Name:       span.Name,
		Status:     string(span.Status),
		Timestamp:  span.StartTime,
		Attributes: map[string]any{},
	}
	for key, value := range span.Attributes {
		obs.Attributes[key] = value
	}
	for _, event := range span.Events {
		obs.Events = append(obs.Events, einoobs.Observation{
			ID:         event.Identity.ID,
			ParentID:   event.Identity.ParentID,
			TraceID:    event.Identity.TraceID,
			Kind:       string(event.Name),
			Name:       string(event.Name),
			Status:     string(event.Status),
			Timestamp:  event.Timestamp,
			Attributes: map[string]any{},
		})
		for key, value := range event.Attributes {
			obs.Events[len(obs.Events)-1].Attributes[key] = value
		}
	}
	return obs
}
