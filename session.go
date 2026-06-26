package einoobs

import (
	"context"
	"errors"
	"sync"
	"time"
)

type SessionStart struct {
	Correlation Correlation
	Name        string
	StartTime   time.Time
	Metadata    Metadata
}

type SessionEnd struct {
	EndTime  time.Time
	Metadata Metadata
}

type SessionError struct {
	Err            error
	Classification string
	Canceled       bool
	Metadata       Metadata
}

type RunStart struct {
	Correlation Correlation
	Name        string
	StartTime   time.Time
	Metadata    Metadata
}

type RunEnd struct {
	EndTime  time.Time
	Metadata Metadata
}

type RunError struct {
	Err            error
	Classification string
	Canceled       bool
	Retryable      bool
	Metadata       Metadata
}

type Session struct {
	observer *Observer
	base     activeObservation
}

type Run struct {
	observer *Observer
	base     activeObservation
}

type activeObservation struct {
	mu         sync.Mutex
	terminal   bool
	ctx        context.Context
	id         string
	parentID   string
	traceID    string
	kind       string
	name       string
	startTime  time.Time
	corr       Correlation
	attributes map[string]any
}

func (o *Observer) StartSession(ctx context.Context, start SessionStart) *Session {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, start.Correlation)
	id := corr.ObservationID
	traceID := corr.TraceID
	parentID := corr.ParentObservationID
	corr.ObservationID = id
	corr.TraceID = traceID
	corr.ParentObservationID = parentID
	metadata := cloneMetadata(start.Metadata)

	return &Session{
		observer: o,
		base: activeObservation{
			ctx:        context.WithoutCancel(ctx),
			id:         id,
			parentID:   parentID,
			traceID:    traceID,
			kind:       "session",
			name:       firstNonEmpty(start.Name, "session"),
			startTime:  observationTime(start.StartTime),
			corr:       corr,
			attributes: baseObservationAttributes(o, corr, metadata),
		},
	}
}

func (o *Observer) StartRun(ctx context.Context, start RunStart) *Run {
	if ctx == nil {
		ctx = context.Background()
	}
	corr := correlationFromContext(ctx, start.Correlation)
	return o.startRun(ctx, start, corr)
}

func (s *Session) StartRun(start RunStart) *Run {
	if s == nil || s.observer == nil {
		return (*Observer)(nil).StartRun(context.Background(), start)
	}
	s.base.mu.Lock()
	baseCtx := s.base.ctx
	baseCorr := s.base.corr
	sessionObsID := s.base.id
	s.base.mu.Unlock()

	if baseCtx == nil {
		baseCtx = context.Background()
	}
	baseCorr.ParentObservationID = sessionObsID
	corr := MergeCorrelation(baseCorr, start.Correlation)
	if start.Correlation.ParentObservationID == "" {
		corr.ParentObservationID = sessionObsID
	}
	return s.observer.startRun(baseCtx, start, corr)
}

func (s *Session) End(end SessionEnd) {
	if s == nil {
		return
	}
	s.base.end(s.observer, "ok", observationTime(end.EndTime), nil, end.Metadata)
}

func (s *Session) Error(err SessionError) {
	if s == nil {
		return
	}
	status := "error"
	if err.Canceled {
		status = "canceled"
	}
	obsErr := terminalObservationError("session", firstNonEmpty(err.Classification, status), err.Err, false)
	s.base.end(s.observer, status, time.Now().UTC(), &obsErr, err.Metadata)
}

func (r *Run) End(end RunEnd) {
	if r == nil {
		return
	}
	r.base.end(r.observer, "ok", observationTime(end.EndTime), nil, end.Metadata)
}

func (r *Run) Error(err RunError) {
	if r == nil {
		return
	}
	status := "error"
	retryable := err.Retryable
	if err.Canceled {
		status = "canceled"
		retryable = false
	}
	obsErr := terminalObservationError("run", firstNonEmpty(err.Classification, status), err.Err, retryable)
	r.base.end(r.observer, status, time.Now().UTC(), &obsErr, err.Metadata)
}

func (o *Observer) startRun(ctx context.Context, start RunStart, corr Correlation) *Run {
	id := corr.ObservationID
	traceID := corr.TraceID
	parentID := corr.ParentObservationID
	corr.ObservationID = id
	corr.TraceID = traceID
	corr.ParentObservationID = parentID
	metadata := cloneMetadata(start.Metadata)

	return &Run{
		observer: o,
		base: activeObservation{
			ctx:        context.WithoutCancel(ctx),
			id:         id,
			parentID:   parentID,
			traceID:    traceID,
			kind:       "run",
			name:       firstNonEmpty(start.Name, "run"),
			startTime:  observationTime(start.StartTime),
			corr:       corr,
			attributes: baseObservationAttributes(o, corr, metadata),
		},
	}
}

func (a *activeObservation) end(observer *Observer, status string, endTime time.Time, obsErr *ObservationError, metadata Metadata) {
	a.mu.Lock()
	if a.terminal {
		a.mu.Unlock()
		return
	}
	a.terminal = true
	attrs := cloneObservationAttributes(a.attributes)
	addMetadataAttributes(attrs, metadata)
	if obsErr != nil && obsErr.Classification == "" {
		obsErr.Classification = status
	}
	if endTime.Before(a.startTime) {
		endTime = a.startTime
	}
	observation := Observation{
		ID:            a.id,
		ParentID:      a.parentID,
		TraceID:       a.traceID,
		Kind:          a.kind,
		Name:          a.name,
		Status:        status,
		Timestamp:     a.startTime,
		Duration:      endTime.Sub(a.startTime),
		DurationKnown: true,
		Attributes:    attrs,
		Error:         cloneObservationErrorPtr(obsErr),
	}
	ctx := a.ctx
	a.mu.Unlock()

	exportObservation(ctx, observer, observation)
}

func exportObservation(ctx context.Context, observer *Observer, observation Observation) {
	if observer == nil {
		return
	}
	exporter, configErr, shutdown, _ := observer.lifecycleState()
	if configErr != nil {
		err := normalizeObservationError("export", "invalid_config", configErr, false, true)
		observer.handleError(ctx, err)
		return
	}
	if shutdown {
		return
	}
	if exporter == nil {
		return
	}
	if err := exporter.Export(ctx, []Observation{observation}); err != nil {
		observer.handleError(ctx, normalizeObservationError("export", "exporter_failure", err, true, false))
	}
}

func baseObservationAttributes(observer *Observer, corr Correlation, metadata Metadata) map[string]any {
	attrs := make(map[string]any)
	if observer != nil {
		cfg := observer.Config()
		addStringAttr(attrs, "service.name", cfg.Service)
		addStringAttr(attrs, "service.env", cfg.Env)
		addStringAttr(attrs, "service.version", cfg.Version)
	}
	addCorrelationAttributes(attrs, corr)
	addMetadataAttributes(attrs, metadata)
	return attrs
}

func addCorrelationAttributes(attrs map[string]any, corr Correlation) {
	addStringAttr(attrs, "correlation.session_id", corr.SessionID)
	addStringAttr(attrs, "correlation.run_id", corr.RunID)
	addStringAttr(attrs, "correlation.agent_id", corr.AgentID)
	addStringAttr(attrs, "correlation.assistant_message_id", corr.AssistantMessageID)
	addStringAttr(attrs, "correlation.thread_id", corr.ThreadID)
	addStringAttr(attrs, "correlation.agui_run_id", corr.AGUIRunID)
	addStringAttr(attrs, "correlation.tool_call_id", corr.ToolCallID)
}

func addMetadataAttributes(attrs map[string]any, metadata Metadata) {
	for key, value := range metadata {
		addStringAttr(attrs, "metadata."+key, value)
	}
}

func addStringAttr(attrs map[string]any, key, value string) {
	if value == "" {
		return
	}
	attrs[key] = value
}

func correlationFromContext(ctx context.Context, explicit Correlation) Correlation {
	if inherited, ok := CorrelationFromContext(ctx); ok {
		return MergeCorrelation(inherited, explicit)
	}
	return explicit
}

func cloneMetadata(metadata Metadata) Metadata {
	if metadata == nil {
		return nil
	}
	out := make(Metadata, len(metadata))
	for key, value := range metadata {
		out[key] = value
	}
	return out
}

func observationTime(at time.Time) time.Time {
	if at.IsZero() {
		return time.Now().UTC()
	}
	return at.UTC()
}

func terminalObservationError(operation, classification string, err error, retryable bool) ObservationError {
	if classification == "" {
		classification = "error"
	}
	if err == nil {
		err = errors.New(classification)
	}
	return ObservationError{
		Operation:      operation,
		Classification: classification,
		Err:            err,
		Retryable:      retryable,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
