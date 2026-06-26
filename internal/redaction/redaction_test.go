package redaction

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/mattsp1290/eino-obs/internal/model"
)

func TestSummaryAttributesDisabledAndNegativeLimit(t *testing.T) {
	attrs, records, err := SummaryAttributes("genai.request.summary", "summary", InputSummary, Summary{Text: "raw"}, Options{})
	if err != nil {
		t.Fatalf("SummaryAttributes() error = %v", err)
	}
	if len(attrs) != 0 {
		t.Fatalf("attrs = %v, want none", attrs)
	}
	if len(records) != 1 || records[0].Reason != ReasonSummaryDisabled {
		t.Fatalf("records = %#v, want summary_disabled", records)
	}

	_, _, err = SummaryAttributes("genai.request.summary", "summary", InputSummary, Summary{Text: "raw"}, Options{MaxSummaryBytes: -1})
	if err == nil {
		t.Fatalf("SummaryAttributes() negative limit error = nil")
	}
}

func TestSummaryAttributesTruncatesUTF8AndCopiesFields(t *testing.T) {
	fields := map[string]string{"safe": "abcdef", "Authorization": "secret"}
	attrs, records, err := SummaryAttributes("genai.request.summary", "summary", InputSummary, Summary{
		Name:   "request",
		Text:   "éclair",
		Fields: fields,
	}, Options{CaptureInputSummary: true, MaxSummaryBytes: 3})
	if err != nil {
		t.Fatalf("SummaryAttributes() error = %v", err)
	}
	fields["safe"] = "changed"

	if got := attrs["genai.request.summary"]; got != "éc" {
		t.Fatalf("summary text = %q, want UTF-8-safe truncation", got)
	}
	if !utf8.ValidString(attrs["genai.request.summary"].(string)) {
		t.Fatalf("summary text is not valid UTF-8")
	}
	if got := attrs["genai.request.summary.fields.safe"]; got != "abc" {
		t.Fatalf("summary field = %q, want abc", got)
	}
	assertRecord(t, records, "summary.text", ReasonSummaryTruncated, len("éclair"), len("éc"))
	assertRecord(t, records, "summary.fields.safe", ReasonSummaryTruncated, len("abcdef"), 3)
	assertRecord(t, records, "summary.fields.Authorization", ReasonDefaultOmitted, len("secret"), 0)
}

func TestSensitiveSummaryNameOmitsWholeSummary(t *testing.T) {
	attrs, records, err := SummaryAttributes("genai.response.summary", "summary", OutputSummary, Summary{
		Name:   " encrypted-reasoning ",
		Text:   "ciphertext",
		Fields: map[string]string{"safe": "value"},
	}, Options{CaptureOutputSummary: true})
	if err != nil {
		t.Fatalf("SummaryAttributes() error = %v", err)
	}
	if len(attrs) != 0 {
		t.Fatalf("attrs = %v, want none", attrs)
	}
	if len(records) != 1 || records[0].Reason != ReasonEncryptedReasoningForbidden {
		t.Fatalf("records = %#v, want encrypted_reasoning_forbidden", records)
	}
}

func TestMetadataAttributesBoundsSensitiveKeysAndDeterministicOverflow(t *testing.T) {
	metadata := map[string]string{
		"z":       "last",
		"api.key": "secret",
		"long":    "abcdef",
		"tool-id": "safe",
		"tool id": "safe2",
	}
	for i := 0; i < MaxMapEntries; i++ {
		metadata[string(rune('a'+i))] = "v"
	}
	attrs, records, err := MetadataAttributes(metadata, Options{MaxSummaryBytes: 3})
	if err != nil {
		t.Fatalf("MetadataAttributes() error = %v", err)
	}
	metadata["long"] = "changed"

	if got := attrs["metadata.long"]; got != "abc" {
		t.Fatalf("metadata.long = %q, want abc", got)
	}
	if _, ok := attrs["metadata.api.key"]; ok {
		t.Fatalf("sensitive metadata was retained")
	}
	assertRecord(t, records, "metadata.api.key", ReasonDefaultOmitted, len("secret"), 0)
	assertRecord(t, records, "metadata.long", ReasonSummaryTruncated, len("abcdef"), 3)
	if !recordsSorted(records) {
		t.Fatalf("records are not deterministic: %#v", records)
	}
}

func TestApplySpanOmitsRawFieldsAndRedactsEvents(t *testing.T) {
	span := model.NewSpan(model.ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, model.SpanKindModelCall, "model", time.Now())
	span.SetAttr("genai.provider", "openai")
	span.SetAttr("genai.model", "gpt-example")
	span.SetAttr("prompt.text", "raw prompt")
	span.SetAttr("genai.request.summary", "hello")
	span.SetAttr("metadata.encrypted reasoning", "ciphertext")
	event := model.NewEvent(model.ObservationIdentity{ID: "event-1", ParentID: "span-1", TraceID: "trace-1"}, model.EventStreamChunk, time.Now())
	event.SetAttr("stream.chunk.index", int64(0))
	event.SetAttr("stream.chunk.summary", "delta")
	span.Events = []model.Event{event}

	redacted, err := ApplySpan(span, Options{CaptureInputSummary: true, MaxSummaryBytes: 4})
	if err != nil {
		t.Fatalf("ApplySpan() error = %v", err)
	}
	if _, ok := redacted.Attributes["prompt.text"]; ok {
		t.Fatalf("raw prompt attribute was retained")
	}
	if got := redacted.Attributes["genai.request.summary"]; got != "hell" {
		t.Fatalf("summary = %q, want hell", got)
	}
	if _, ok := redacted.Attributes["metadata.encrypted reasoning"]; ok {
		t.Fatalf("encrypted reasoning metadata was retained")
	}
	if _, ok := redacted.Events[0].Attributes["stream.chunk.summary"]; ok {
		t.Fatalf("disabled output chunk summary was retained")
	}
	if _, ok := span.Attributes["prompt.text"]; !ok {
		t.Fatalf("original span was mutated")
	}
	assertRecord(t, redacted.Redaction, "prompt.text", ReasonDefaultOmitted, len("raw prompt"), 0)
	assertRecord(t, redacted.Redaction, "metadata.encrypted reasoning", ReasonEncryptedReasoningForbidden, len("ciphertext"), 0)
	assertRecord(t, redacted.Events[0].Redaction, "stream.chunk.summary", ReasonSummaryDisabled, 0, 0)
}

func TestExactlyAtLimitRetainedAndTinyLimitValidUTF8(t *testing.T) {
	attrs, records, err := SummaryAttributes("genai.response.summary", "summary", OutputSummary, Summary{Text: "abcd"}, Options{CaptureOutputSummary: true, MaxSummaryBytes: 4})
	if err != nil {
		t.Fatalf("SummaryAttributes() error = %v", err)
	}
	if got := attrs["genai.response.summary"]; got != "abcd" {
		t.Fatalf("summary = %q, want abcd", got)
	}
	if len(records) != 0 {
		t.Fatalf("records = %#v, want none", records)
	}

	attrs, records, err = SummaryAttributes("genai.response.summary", "summary", OutputSummary, Summary{Text: "é"}, Options{CaptureOutputSummary: true, MaxSummaryBytes: 1})
	if err != nil {
		t.Fatalf("SummaryAttributes() tiny limit error = %v", err)
	}
	if got := attrs["genai.response.summary"]; got != "" {
		t.Fatalf("summary = %q, want empty retained prefix", got)
	}
	assertRecord(t, records, "summary.text", ReasonSummaryTruncated, len("é"), 0)
}

func assertRecord(t *testing.T, records []model.RedactionRecord, path string, reason string, original int, retained int) {
	t.Helper()
	for _, record := range records {
		if record.FieldPath == path && record.Reason == reason {
			if record.OriginalBytes != original || record.RetainedBytes != retained {
				t.Fatalf("record %s = %#v, want original=%d retained=%d", path, record, original, retained)
			}
			return
		}
	}
	t.Fatalf("missing redaction record path=%s reason=%s in %#v", path, reason, records)
}

func recordsSorted(records []model.RedactionRecord) bool {
	last := ""
	for _, record := range records {
		current := strings.ToLower(record.FieldPath)
		if current < last {
			return false
		}
		last = current
	}
	return true
}
