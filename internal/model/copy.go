package model

func (s Span) Clone() Span {
	out := s
	out.Attributes = CloneAttributes(s.Attributes)
	out.Events = cloneEvents(s.Events)
	out.Redaction = cloneRedaction(s.Redaction)
	if s.Error != nil {
		errCopy := *s.Error
		out.Error = &errCopy
	}
	return out
}

func (e Event) Clone() Event {
	out := e
	out.Attributes = CloneAttributes(e.Attributes)
	out.Redaction = cloneRedaction(e.Redaction)
	if e.Error != nil {
		errCopy := *e.Error
		out.Error = &errCopy
	}
	return out
}

func CloneAttributes(attrs Attributes) Attributes {
	if attrs == nil {
		return nil
	}
	out := make(Attributes, len(attrs))
	for key, value := range attrs {
		out[key] = cloneAttributeValue(value)
	}
	return out
}

func cloneAttributeValue(value any) any {
	switch v := value.(type) {
	case []byte:
		out := make([]byte, len(v))
		copy(out, v)
		return out
	case []string:
		out := make([]string, len(v))
		copy(out, v)
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = cloneAttributeValue(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for key, item := range v {
			out[key] = cloneAttributeValue(item)
		}
		return out
	case Attributes:
		return CloneAttributes(v)
	default:
		return value
	}
}

func cloneEvents(events []Event) []Event {
	if events == nil {
		return nil
	}
	out := make([]Event, len(events))
	for i, event := range events {
		out[i] = event.Clone()
	}
	return out
}

func cloneRedaction(records []RedactionRecord) []RedactionRecord {
	if records == nil {
		return nil
	}
	out := make([]RedactionRecord, len(records))
	copy(out, records)
	return out
}
