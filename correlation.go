package einoobs

import "context"

type correlationContextKey struct{}

// Correlation carries low-cardinality identifiers propagated across
// observations. Empty fields inherit from context when merged and are omitted
// from normalized output.
type Correlation struct {
	SessionID           string
	RunID               string
	AgentID             string
	AssistantMessageID  string
	ThreadID            string
	AGUIRunID           string
	ToolCallID          string
	Provider            string
	Model               string
	TraceID             string
	ObservationID       string
	ParentObservationID string
}

// ContextWithCorrelation returns a child context carrying corr merged over any
// correlation already present on ctx. Non-empty fields in corr override existing
// fields; empty fields inherit existing values.
func ContextWithCorrelation(ctx context.Context, corr Correlation) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if existing, ok := CorrelationFromContext(ctx); ok {
		corr = MergeCorrelation(existing, corr)
	}
	return context.WithValue(ctx, correlationContextKey{}, corr)
}

// CorrelationFromContext returns the correlation stored on ctx.
func CorrelationFromContext(ctx context.Context) (Correlation, bool) {
	if ctx == nil {
		return Correlation{}, false
	}
	corr, ok := ctx.Value(correlationContextKey{}).(Correlation)
	return corr, ok
}

// MergeCorrelation overlays explicit fields on base. Empty explicit fields
// inherit base values.
func MergeCorrelation(base, explicit Correlation) Correlation {
	merged := base
	if explicit.SessionID != "" {
		merged.SessionID = explicit.SessionID
	}
	if explicit.RunID != "" {
		merged.RunID = explicit.RunID
	}
	if explicit.AgentID != "" {
		merged.AgentID = explicit.AgentID
	}
	if explicit.AssistantMessageID != "" {
		merged.AssistantMessageID = explicit.AssistantMessageID
	}
	if explicit.ThreadID != "" {
		merged.ThreadID = explicit.ThreadID
	}
	if explicit.AGUIRunID != "" {
		merged.AGUIRunID = explicit.AGUIRunID
	}
	if explicit.ToolCallID != "" {
		merged.ToolCallID = explicit.ToolCallID
	}
	if explicit.Provider != "" {
		merged.Provider = explicit.Provider
	}
	if explicit.Model != "" {
		merged.Model = explicit.Model
	}
	if explicit.TraceID != "" {
		merged.TraceID = explicit.TraceID
	}
	if explicit.ObservationID != "" {
		merged.ObservationID = explicit.ObservationID
	}
	if explicit.ParentObservationID != "" {
		merged.ParentObservationID = explicit.ParentObservationID
	}
	return merged
}

// IsZero reports whether corr carries no propagated fields.
func (corr Correlation) IsZero() bool {
	return corr == Correlation{}
}
