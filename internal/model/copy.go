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
		out[key] = value
	}
	return out
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
