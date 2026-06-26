package einoobs

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var ErrInvalidConfig = errors.New("invalid eino-obs config")

type Option func(*Config) error

type Observer struct {
	mu              sync.Mutex
	config          Config
	configErr       error
	shutdown        bool
	lastShutdownErr *ObservationError
}

func New(config Config, opts ...Option) *Observer {
	cfg := config.Clone()
	var configErr error
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			configErr = err
			break
		}
	}
	if configErr == nil {
		configErr = cfg.Validate()
	}
	if configErr == nil {
		if _, ok := cfg.Exporter.(noNetworkMarker); ok {
			cfg.Exporter = nil
		}
	}
	if configErr == nil && cfg.Exporter == nil {
		cfg.Exporter = NewNoNetworkExporter(cfg.Redaction)
	}
	return &Observer{config: cfg, configErr: configErr}
}

func WithService(service string) Option {
	return func(config *Config) error {
		config.Service = service
		return nil
	}
}

func WithEnv(env string) Option {
	return func(config *Config) error {
		config.Env = env
		return nil
	}
}

func WithVersion(version string) Option {
	return func(config *Config) error {
		config.Version = version
		return nil
	}
}

func WithRedaction(redaction RedactionOptions) Option {
	return func(config *Config) error {
		config.Redaction = redaction
		return nil
	}
}

func WithExporter(exporter Exporter) Option {
	return func(config *Config) error {
		config.Exporter = exporter
		return nil
	}
}

func WithErrorHandler(handler ErrorHandler) Option {
	return func(config *Config) error {
		config.ErrorHandler = handler
		return nil
	}
}

func (o *Observer) Config() Config {
	if o == nil {
		return Config{}
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.config.Clone()
}

func (o *Observer) Exporter() Exporter {
	if o == nil {
		return nil
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.config.Exporter
}

func (o *Observer) Snapshot() NoNetworkSnapshot {
	if o == nil {
		return NoNetworkSnapshot{}
	}
	exporter := o.Exporter()
	snapshotter, ok := exporter.(interface {
		Snapshot() NoNetworkSnapshot
	})
	if !ok {
		return NoNetworkSnapshot{}
	}
	return snapshotter.Snapshot()
}

func (o *Observer) Reset() {
	if o == nil {
		return
	}
	exporter := o.Exporter()
	resetter, ok := exporter.(interface {
		Reset()
	})
	if ok {
		resetter.Reset()
	}
	o.mu.Lock()
	o.shutdown = false
	o.lastShutdownErr = nil
	o.mu.Unlock()
}

func (o *Observer) Flush(ctx context.Context) error {
	if o == nil {
		return nil
	}
	exporter, configErr, shutdown, lastShutdownErr := o.lifecycleState()
	if configErr != nil {
		err := normalizeObservationError("redact", "invalid_config", configErr, false, true)
		o.handleError(ctx, err)
		return err
	}
	if shutdown {
		if lastShutdownErr != nil {
			return *lastShutdownErr
		}
		return nil
	}
	if exporter == nil {
		return nil
	}
	if err := exporter.Flush(ctx); err != nil {
		obsErr := normalizeObservationError("flush", "exporter_failure", err, true, false)
		o.handleError(ctx, obsErr)
		return obsErr
	}
	return nil
}

func (o *Observer) Shutdown(ctx context.Context) error {
	if o == nil {
		return nil
	}
	exporter, configErr, alreadyShutdown, lastShutdownErr := o.markShutdown()
	if alreadyShutdown {
		if lastShutdownErr != nil {
			return *lastShutdownErr
		}
		return nil
	}
	if configErr != nil {
		err := normalizeObservationError("redact", "invalid_config", configErr, false, true)
		o.handleError(ctx, err)
		return err
	}
	if exporter == nil {
		return nil
	}
	if err := exporter.Shutdown(ctx); err != nil {
		obsErr := normalizeObservationError("shutdown", "exporter_failure", err, true, false)
		o.setLastShutdownError(obsErr)
		o.handleError(ctx, obsErr)
		return obsErr
	}
	o.clearLastShutdownError()
	return nil
}

func (c Config) Clone() Config {
	return c
}

func (c Config) Validate() error {
	if c.Redaction.MaxSummaryBytes < 0 {
		return ObservationError{
			Operation:      "redact",
			Classification: "invalid_config",
			Err:            fmt.Errorf("%w: redaction max summary bytes must be non-negative", ErrInvalidConfig),
			Retryable:      false,
			Dropped:        true,
		}
	}
	return nil
}

func (o *Observer) lifecycleState() (Exporter, error, bool, *ObservationError) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.config.Exporter, o.configErr, o.shutdown, cloneObservationErrorPtr(o.lastShutdownErr)
}

func (o *Observer) markShutdown() (Exporter, error, bool, *ObservationError) {
	o.mu.Lock()
	defer o.mu.Unlock()
	alreadyShutdown := o.shutdown
	o.shutdown = true
	return o.config.Exporter, o.configErr, alreadyShutdown, cloneObservationErrorPtr(o.lastShutdownErr)
}

func (o *Observer) setLastShutdownError(err ObservationError) {
	o.mu.Lock()
	defer o.mu.Unlock()
	errCopy := err
	o.lastShutdownErr = &errCopy
}

func (o *Observer) clearLastShutdownError() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.lastShutdownErr = nil
}

func (o *Observer) handleError(ctx context.Context, err ObservationError) {
	o.mu.Lock()
	handler := o.config.ErrorHandler
	o.mu.Unlock()
	if handler == nil {
		return
	}
	handler(ctx, err)
}

func cloneObservationErrorPtr(err *ObservationError) *ObservationError {
	if err == nil {
		return nil
	}
	out := *err
	return &out
}

func normalizeObservationError(operation string, classification string, err error, retryable bool, dropped bool) ObservationError {
	var obsErr ObservationError
	if errors.As(err, &obsErr) {
		if obsErr.Operation == "" {
			obsErr.Operation = operation
		}
		if obsErr.Classification == "" {
			obsErr.Classification = classification
		}
		return obsErr
	}
	return ObservationError{
		Operation:      operation,
		Classification: classification,
		Err:            err,
		Retryable:      retryable,
		Dropped:        dropped,
	}
}
