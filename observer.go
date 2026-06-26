package einoobs

import (
	"context"
	"errors"
	"fmt"
)

var ErrInvalidConfig = errors.New("invalid eino-obs config")

type Option func(*Config) error

type Observer struct {
	config Config
}

func New(config Config, opts ...Option) (*Observer, error) {
	cfg := config.Clone()
	for _, opt := range opts {
		if opt == nil {
			continue
		}
		if err := opt(&cfg); err != nil {
			return nil, err
		}
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &Observer{config: cfg}, nil
}

func MustNew(config Config, opts ...Option) *Observer {
	observer, err := New(config, opts...)
	if err != nil {
		panic(err)
	}
	return observer
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
	return o.config.Clone()
}

func (o *Observer) Exporter() Exporter {
	if o == nil {
		return nil
	}
	return o.config.Exporter
}

func (o *Observer) Flush(ctx context.Context) error {
	if o == nil || o.config.Exporter == nil {
		return nil
	}
	return o.config.Exporter.Flush(ctx)
}

func (o *Observer) Shutdown(ctx context.Context) error {
	if o == nil || o.config.Exporter == nil {
		return nil
	}
	return o.config.Exporter.Shutdown(ctx)
}

func (c Config) Clone() Config {
	return c
}

func (c Config) Validate() error {
	if c.Redaction.MaxSummaryBytes < 0 {
		return fmt.Errorf("%w: redaction max summary bytes must be non-negative", ErrInvalidConfig)
	}
	return nil
}
