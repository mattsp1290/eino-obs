package redaction

import (
	"fmt"
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
	if records[0].OriginalBytes != 0 || records[0].RetainedBytes != 0 {
		t.Fatalf("encrypted reasoning record counted bytes: %#v", records[0])
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

func TestOversizedMapRetentionIgnoresOmittedEntries(t *testing.T) {
	metadata := map[string]string{
		"api-key": "secret",
	}
	for i := 0; i < MaxMapEntries; i++ {
		metadata[fmt.Sprintf("safe-%02d", i)] = "safe"
	}

	attrs, records, err := MetadataAttributes(metadata, Options{})
	if err != nil {
		t.Fatalf("MetadataAttributes() error = %v", err)
	}
	if got := len(attrs); got != MaxMapEntries {
		t.Fatalf("retained attrs = %d, want %d", got, MaxMapEntries)
	}
	if _, ok := attrs["metadata.safe-31"]; !ok {
		t.Fatalf("last safe metadata entry was not retained")
	}
	assertRecord(t, records, "metadata.api-key", ReasonDefaultOmitted, len("secret"), 0)
}

func TestApplySpanOmitsRawFieldsAndRedactsEvents(t *testing.T) {
	span := model.NewSpan(model.ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, model.SpanKindModelCall, "model", time.Now())
	span.SetAttr("genai.provider", "openai")
	span.SetAttr("genai.model", "gpt-example")
	span.SetAttr("prompt.text", "raw prompt")
	span.SetAttr("genai.request.summary", "hello")
	span.SetAttr("metadata.encrypted reasoning", "ciphertext")
	span.SetAttr("tool.input.summary", "query")
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
	if got := redacted.Attributes["tool.input.summary"]; got != "quer" {
		t.Fatalf("tool input summary = %q, want quer", got)
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

func TestApplyAttributesCanonicalMetadataAndRawPayloadOmission(t *testing.T) {
	attrs := model.Attributes{
		" Metadata.Api-Key ":           "secret",
		"metadata_encrypted.reasoning": "ciphertext",
		"model.input.messages":         "raw input",
		"model-output-text":            "raw output",
		"tool_input.payload":           "raw tool input",
		"tool output payload":          "raw tool output",
		"attachments":                  "raw attachment",
		"reasoning.text":               "raw reasoning",
		"provider.response.body":       "raw response",
		"tool.output.summary":          "safe summary",
	}

	redacted, records, err := ApplyAttributes(attrs, Options{CaptureOutputSummary: true})
	if err != nil {
		t.Fatalf("ApplyAttributes() error = %v", err)
	}
	for key := range attrs {
		if key == "tool.output.summary" {
			continue
		}
		if _, ok := redacted[key]; ok {
			t.Fatalf("sensitive attr %q was retained", key)
		}
	}
	if got := redacted["tool.output.summary"]; got != "safe summary" {
		t.Fatalf("tool output summary = %q, want safe summary", got)
	}
	assertRecord(t, records, " Metadata.Api-Key ", ReasonDefaultOmitted, len("secret"), 0)
	assertRecord(t, records, "metadata_encrypted.reasoning", ReasonEncryptedReasoningForbidden, len("ciphertext"), 0)
}

func TestApplyAttributesOmitsEveryUnsupportedRawPayloadCategory(t *testing.T) {
	attrs := model.Attributes{
		"prompt":                 "raw prompt",
		"model.output.text":      "raw output",
		"tool.input.payload":     "raw tool input",
		"tool.output.payload":    "raw tool output",
		"attachment.bytes":       "raw attachment",
		"reasoning.text":         "raw reasoning",
		"provider.request.body":  "raw request",
		"provider.response.body": "raw response",
		"metadata.safe":          "safe",
	}

	redacted, records, err := ApplyAttributes(attrs, Options{})
	if err != nil {
		t.Fatalf("ApplyAttributes() error = %v", err)
	}
	for key := range attrs {
		if key == "metadata.safe" {
			continue
		}
		if _, ok := redacted[key]; ok {
			t.Fatalf("raw payload attr %q was retained", key)
		}
		assertRecord(t, records, key, ReasonDefaultOmitted, len(attrs[key].(string)), 0)
	}
	if redacted["metadata.safe"] != "safe" {
		t.Fatalf("safe metadata = %q, want safe", redacted["metadata.safe"])
	}
}

func TestSensitiveNameCanonicalizationVariants(t *testing.T) {
	for _, name := range []string{
		" Authorization ",
		"api-key",
		"api.key",
		"api key",
		"ACCESS_TOKEN",
		"client.secret",
		"set cookie",
		"encrypted-reasoning",
		"reasoning encrypted",
	} {
		t.Run(name, func(t *testing.T) {
			attrs, records, err := MetadataAttributes(map[string]string{name: "secret"}, Options{})
			if err != nil {
				t.Fatalf("MetadataAttributes() error = %v", err)
			}
			if len(attrs) != 0 {
				t.Fatalf("attrs = %#v, want omitted", attrs)
			}
			reason := ReasonDefaultOmitted
			if strings.Contains(strings.ToLower(name), "reasoning") {
				reason = ReasonEncryptedReasoningForbidden
			}
			assertRecord(t, records, "metadata."+strings.TrimSpace(name), reason, len("secret"), 0)
		})
	}
}

func TestOversizedSummaryAndMetadataNamesAreOmitted(t *testing.T) {
	oversizedName := strings.Repeat("a", MaxNameBytes+1)
	attrs, records, err := SummaryAttributes("genai.request.summary", "summary", InputSummary, Summary{
		Name: oversizedName,
		Text: "safe",
	}, Options{CaptureInputSummary: true})
	if err != nil {
		t.Fatalf("SummaryAttributes() error = %v", err)
	}
	if len(attrs) != 0 {
		t.Fatalf("oversized summary attrs = %#v, want omitted", attrs)
	}
	assertRecord(t, records, "summary.name", ReasonFieldLimitExceeded, len(oversizedName), 0)

	attrs, records, err = MetadataAttributes(map[string]string{oversizedName: "safe"}, Options{})
	if err != nil {
		t.Fatalf("MetadataAttributes() error = %v", err)
	}
	if len(attrs) != 0 {
		t.Fatalf("oversized metadata attrs = %#v, want omitted", attrs)
	}
	assertRecord(t, records, "metadata."+oversizedName, ReasonFieldLimitExceeded, len(oversizedName), 0)
}

func TestApplySpanRedactsEncryptedReasoningErrorMessageWithMetadata(t *testing.T) {
	span := model.NewSpan(model.ObservationIdentity{ID: "span-1", TraceID: "trace-1"}, model.SpanKindRun, "run", time.Now())
	span.Error = &model.ObservationError{Operation: "encrypted_reasoning", Type: "provider", Message: "ciphertext"}

	redacted, err := ApplySpan(span, Options{})
	if err != nil {
		t.Fatalf("ApplySpan() error = %v", err)
	}
	if redacted.Error.Message != "" {
		t.Fatalf("error message = %q, want omitted", redacted.Error.Message)
	}
	assertRecord(t, redacted.Redaction, "error.message", ReasonEncryptedReasoningForbidden, 0, 0)
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
