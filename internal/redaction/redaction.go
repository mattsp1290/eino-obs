package redaction

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mattsp1290/eino-obs/internal/model"
)

const (
	DefaultMaxSummaryBytes = 1024
	MaxNameBytes           = 128
	MaxMapEntries          = 32

	ReasonDefaultOmitted              = "default_omitted"
	ReasonSummaryDisabled             = "summary_disabled"
	ReasonSummaryTruncated            = "summary_truncated"
	ReasonFieldLimitExceeded          = "field_limit_exceeded"
	ReasonEncryptedReasoningForbidden = "encrypted_reasoning_forbidden"
)

type Options struct {
	CaptureInputSummary  bool
	CaptureOutputSummary bool
	MaxSummaryBytes      int
}

type Summary struct {
	Name   string
	Text   string
	Fields map[string]string
}

type SummarySide string

const (
	InputSummary  SummarySide = "input"
	OutputSummary SummarySide = "output"
)

func ValidateOptions(opts Options) error {
	if opts.MaxSummaryBytes < 0 {
		return fmt.Errorf("max summary bytes must be non-negative, got %d", opts.MaxSummaryBytes)
	}
	return nil
}

func ApplySpan(span model.Span, opts Options) (model.Span, error) {
	if err := ValidateOptions(opts); err != nil {
		return model.Span{}, err
	}
	out := span.Clone()
	attrs, records, err := ApplyAttributes(out.Attributes, opts)
	if err != nil {
		return model.Span{}, err
	}
	out.Attributes = attrs
	out.Redaction = append(out.Redaction, records...)
	for i, event := range out.Events {
		redacted, err := ApplyEvent(event, opts)
		if err != nil {
			return model.Span{}, err
		}
		out.Events[i] = redacted
	}
	if out.Error != nil {
		redactedError, record := redactError(out.Error)
		out.Error = redactedError
		if record != nil {
			out.Redaction = append(out.Redaction, *record)
		}
	}
	return out, nil
}

func ApplyEvent(event model.Event, opts Options) (model.Event, error) {
	if err := ValidateOptions(opts); err != nil {
		return model.Event{}, err
	}
	out := event.Clone()
	attrs, records, err := ApplyAttributes(out.Attributes, opts)
	if err != nil {
		return model.Event{}, err
	}
	out.Attributes = attrs
	out.Redaction = append(out.Redaction, records...)
	if out.Error != nil {
		redactedError, record := redactError(out.Error)
		out.Error = redactedError
		if record != nil {
			out.Redaction = append(out.Redaction, *record)
		}
	}
	return out, nil
}

func ApplyAttributes(attrs model.Attributes, opts Options) (model.Attributes, []model.RedactionRecord, error) {
	if err := ValidateOptions(opts); err != nil {
		return nil, nil, err
	}
	if attrs == nil {
		return nil, nil, nil
	}
	out := make(model.Attributes, len(attrs))
	var records []model.RedactionRecord
	for _, key := range sortedAttrKeys(attrs) {
		value := attrs[key]
		if side, ok := summarySideForAttribute(key); ok {
			if !summaryEnabled(side, opts) {
				records = append(records, model.RedactionRecord{FieldPath: key, Reason: ReasonSummaryDisabled})
				continue
			}
			text, ok := value.(string)
			if !ok {
				out[key] = value
				continue
			}
			redacted, record := boundedString(key, text, maxSummaryBytes(opts), ReasonSummaryTruncated)
			out[key] = redacted
			if record != nil {
				records = append(records, *record)
			}
			continue
		}
		if shouldOmitRawAttribute(key) {
			records = append(records, omitRecord(key, keyOmissionReason(key), byteLen(value)))
			continue
		}
		if name, ok := metadataNameFromAttribute(key); ok {
			if isSensitiveName(name) {
				records = append(records, omitRecord(key, keyOmissionReason(name), byteLen(value)))
				continue
			}
			if len([]byte(name)) > MaxNameBytes {
				records = append(records, omitRecord(key, ReasonFieldLimitExceeded, byteLen(value)))
				continue
			}
			if text, ok := value.(string); ok {
				redacted, record := boundedString(key, text, maxSummaryBytes(opts), ReasonSummaryTruncated)
				out[key] = redacted
				if record != nil {
					records = append(records, *record)
				}
				continue
			}
		}
		out[key] = value
	}
	return out, records, nil
}

func SummaryAttributes(attrKey string, fieldPath string, side SummarySide, summary Summary, opts Options) (model.Attributes, []model.RedactionRecord, error) {
	if err := ValidateOptions(opts); err != nil {
		return nil, nil, err
	}
	if fieldPath == "" {
		fieldPath = attrKey
	}
	if !summaryEnabled(side, opts) {
		return nil, []model.RedactionRecord{{FieldPath: fieldPath, Reason: ReasonSummaryDisabled}}, nil
	}
	reason := summaryNameOmissionReason(summary.Name)
	if reason != "" {
		return nil, []model.RedactionRecord{{FieldPath: fieldPath, Reason: reason}}, nil
	}
	if len([]byte(summary.Name)) > MaxNameBytes {
		return nil, []model.RedactionRecord{{FieldPath: fieldPath + ".name", Reason: ReasonFieldLimitExceeded, OriginalBytes: len([]byte(summary.Name))}}, nil
	}

	attrs := model.Attributes{}
	var records []model.RedactionRecord
	if summary.Text != "" {
		text, record := boundedString(fieldPath+".text", summary.Text, maxSummaryBytes(opts), ReasonSummaryTruncated)
		attrs[attrKey] = text
		if record != nil {
			records = append(records, *record)
		}
	}
	if summary.Name != "" {
		attrs[attrKey+".name"] = summary.Name
	}
	fieldAttrs, fieldRecords := stringMapAttributes(attrKey+".fields.", fieldPath+".fields.", summary.Fields, opts)
	for key, value := range fieldAttrs {
		attrs[key] = value
	}
	records = append(records, fieldRecords...)
	if len(attrs) == 0 && len(records) == 0 {
		return nil, nil, nil
	}
	return attrs, records, nil
}

func MetadataAttributes(metadata map[string]string, opts Options) (model.Attributes, []model.RedactionRecord, error) {
	if err := ValidateOptions(opts); err != nil {
		return nil, nil, err
	}
	attrs, records := stringMapAttributes("metadata.", "metadata.", metadata, opts)
	return attrs, records, nil
}

func stringMapAttributes(attrPrefix string, fieldPrefix string, values map[string]string, opts Options) (model.Attributes, []model.RedactionRecord) {
	if values == nil {
		return nil, nil
	}
	attrs := model.Attributes{}
	var records []model.RedactionRecord
	retained := 0
	for _, entry := range sortedStringMapEntries(values) {
		fieldPath := fieldPrefix + entry.key
		if len([]byte(entry.key)) > MaxNameBytes {
			records = append(records, model.RedactionRecord{FieldPath: fieldPath, Reason: ReasonFieldLimitExceeded, OriginalBytes: len([]byte(entry.key))})
			continue
		}
		if isSensitiveName(entry.key) {
			records = append(records, model.RedactionRecord{FieldPath: fieldPath, Reason: keyOmissionReason(entry.key), OriginalBytes: len([]byte(entry.value))})
			continue
		}
		if retained >= MaxMapEntries {
			records = append(records, model.RedactionRecord{FieldPath: fieldPath, Reason: ReasonFieldLimitExceeded, OriginalBytes: len([]byte(entry.value))})
			continue
		}
		value, record := boundedString(fieldPath, entry.value, maxSummaryBytes(opts), ReasonSummaryTruncated)
		attrs[attrPrefix+entry.key] = value
		retained++
		if record != nil {
			records = append(records, *record)
		}
	}
	return attrs, records
}

func sortedAttrKeys(attrs model.Attributes) []string {
	keys := make([]string, 0, len(attrs))
	for key := range attrs {
		keys = append(keys, key)
	}
	sort.Slice(keys, func(i, j int) bool {
		left := canonicalName(keys[i])
		right := canonicalName(keys[j])
		if left == right {
			return keys[i] < keys[j]
		}
		return left < right
	})
	return keys
}

type stringEntry struct {
	key       string
	value     string
	canonical string
}

func sortedStringMapEntries(values map[string]string) []stringEntry {
	entries := make([]stringEntry, 0, len(values))
	for key, value := range values {
		trimmed := strings.TrimSpace(key)
		entries = append(entries, stringEntry{key: trimmed, value: value, canonical: canonicalName(trimmed)})
	}
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].canonical == entries[j].canonical {
			return entries[i].key < entries[j].key
		}
		return entries[i].canonical < entries[j].canonical
	})
	return entries
}

func boundedString(path string, value string, limit int, reason string) (string, *model.RedactionRecord) {
	retained := truncateUTF8(value, limit)
	if len([]byte(retained)) == len([]byte(value)) {
		return retained, nil
	}
	return retained, &model.RedactionRecord{
		FieldPath:     path,
		Reason:        reason,
		OriginalBytes: len([]byte(value)),
		RetainedBytes: len([]byte(retained)),
	}
}

func truncateUTF8(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if len([]byte(value)) <= limit {
		return value
	}
	last := 0
	for i := range value {
		if i > limit {
			break
		}
		last = i
	}
	if last == 0 {
		_, size := utf8.DecodeRuneInString(value)
		if size <= limit {
			return value[:size]
		}
		return ""
	}
	return value[:last]
}

func maxSummaryBytes(opts Options) int {
	if opts.MaxSummaryBytes == 0 {
		return DefaultMaxSummaryBytes
	}
	return opts.MaxSummaryBytes
}

func summaryEnabled(side SummarySide, opts Options) bool {
	switch side {
	case InputSummary:
		return opts.CaptureInputSummary
	case OutputSummary:
		return opts.CaptureOutputSummary
	default:
		return false
	}
}

func summarySideForAttribute(key string) (SummarySide, bool) {
	switch canonicalName(key) {
	case "genai_request_summary", "tool_input_summary":
		return InputSummary, true
	case "genai_response_summary", "stream_chunk_summary", "tool_output_summary":
		return OutputSummary, true
	default:
		return "", false
	}
}

func metadataNameFromAttribute(key string) (string, bool) {
	trimmed := strings.TrimSpace(key)
	if strings.HasPrefix(trimmed, "metadata.") {
		return strings.TrimPrefix(trimmed, "metadata."), true
	}
	canonical := canonicalName(trimmed)
	const prefix = "metadata_"
	if strings.HasPrefix(canonical, prefix) {
		return strings.TrimPrefix(canonical, prefix), true
	}
	return "", false
}

func summaryNameOmissionReason(name string) string {
	if name == "" {
		return ""
	}
	if isEncryptedReasoningName(name) {
		return ReasonEncryptedReasoningForbidden
	}
	if isSensitiveName(name) {
		return ReasonDefaultOmitted
	}
	return ""
}

func shouldOmitRawAttribute(key string) bool {
	canonical := canonicalName(key)
	if isEncryptedReasoningCanonical(canonical) {
		return true
	}
	for _, prefix := range []string{
		"prompt_", "model_input_", "model_output_", "tool_input_",
		"tool_output_", "attachment_", "attachments_", "reasoning_",
		"provider_request_", "provider_response_",
	} {
		if strings.HasPrefix(canonical, prefix) {
			return true
		}
	}
	switch canonical {
	case "prompt", "prompt_text", "model_input", "model_input_messages",
		"model_output", "model_output_text", "tool_input", "tool_output",
		"attachment", "attachments", "reasoning", "reasoning_text",
		"provider_request", "provider_response", "provider_request_body",
		"provider_response_body":
		return true
	default:
		return false
	}
}

func keyOmissionReason(name string) string {
	if isEncryptedReasoningName(name) {
		return ReasonEncryptedReasoningForbidden
	}
	return ReasonDefaultOmitted
}

func isSensitiveName(name string) bool {
	canonical := canonicalName(name)
	if isEncryptedReasoningCanonical(canonical) {
		return true
	}
	switch canonical {
	case "authorization", "api_key", "apikey", "x_api_key", "access_token",
		"refresh_token", "bearer_token", "token", "password", "secret",
		"client_secret", "cookie", "set_cookie":
		return true
	default:
		return false
	}
}

func isEncryptedReasoningName(name string) bool {
	return isEncryptedReasoningCanonical(canonicalName(name))
}

func isEncryptedReasoningCanonical(canonical string) bool {
	return canonical == "encrypted_reasoning" || canonical == "reasoning_encrypted"
}

func canonicalName(name string) string {
	name = strings.TrimFunc(name, unicode.IsSpace)
	var b strings.Builder
	lastSep := false
	for _, r := range name {
		sep := r == '-' || r == '_' || r == '.' || unicode.IsSpace(r)
		if sep {
			if !lastSep && b.Len() > 0 {
				b.WriteByte('_')
			}
			lastSep = true
			continue
		}
		lastSep = false
		if r >= 'A' && r <= 'Z' {
			r += 'a' - 'A'
		}
		b.WriteRune(r)
	}
	return strings.Trim(b.String(), "_")
}

func byteLen(value any) int {
	switch v := value.(type) {
	case string:
		return len([]byte(v))
	case []byte:
		return len(v)
	default:
		return 0
	}
}

func omitRecord(path string, reason string, originalBytes int) model.RedactionRecord {
	return model.RedactionRecord{FieldPath: path, Reason: reason, OriginalBytes: originalBytes}
}

func redactError(err *model.ObservationError) (*model.ObservationError, *model.RedactionRecord) {
	if err == nil {
		return nil, nil
	}
	out := *err
	if out.Message != "" && (isEncryptedReasoningName(out.Type) || isEncryptedReasoningName(out.Operation)) {
		out.Message = ""
		return &out, &model.RedactionRecord{FieldPath: "error.message", Reason: ReasonEncryptedReasoningForbidden}
	}
	return &out, nil
}
