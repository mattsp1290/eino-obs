package datadog

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
	"github.com/mattsp1290/eino-obs/internal/model"
	internalrecorder "github.com/mattsp1290/eino-obs/internal/recorder"
	"github.com/mattsp1290/eino-obs/internal/redaction"
)

const (
	defaultSite          = "datadoghq.com"
	defaultMLApp         = "eino-obs"
	defaultService       = "eino-obs"
	defaultTimeout       = 10 * time.Second
	defaultBatchSize     = 100
	defaultPayloadBytes  = 1024 * 1024
	defaultMaxRetries    = 3
	defaultRetryBase     = 200 * time.Millisecond
	defaultRetryMax      = 5 * time.Second
	intakePath           = "/api/intake/llm-obs/v1/trace/spans"
	unknownClientVersion = "unknown"
)

type Sleeper interface {
	Sleep(context.Context, time.Duration) error
}

type Config struct {
	APIKey                       string
	Site                         string
	Endpoint                     string
	MLApp                        string
	Service                      string
	Env                          string
	Version                      string
	HTTPClient                   *http.Client
	Timeout                      time.Duration
	BatchSize                    int
	MaxPayloadBytes              int
	MaxRetries                   int
	RetryBaseDelay               time.Duration
	RetryMaxDelay                time.Duration
	RetryJitterSeed              int64
	Sleeper                      Sleeper
	ValidateCredentials          bool
	DisableCompression           bool
	AllowMissingAPIKeyForTesting bool

	TimeoutOverride             *time.Duration
	BatchSizeOverride           *int
	MaxPayloadBytesOverride     *int
	MaxRetriesOverride          *int
	RetryBaseDelayOverride      *time.Duration
	RetryMaxDelayOverride       *time.Duration
	ValidateCredentialsOverride *bool
	DisableCompressionOverride  *bool
}

type Exporter struct {
	config     Config
	client     *http.Client
	ownsClient bool
}

func New(config Config) (*Exporter, error) {
	resolved, err := ResolveConfig(config)
	if err != nil {
		return nil, err
	}
	client := resolved.HTTPClient
	ownsClient := false
	if client == nil {
		client = &http.Client{Timeout: resolved.Timeout}
		ownsClient = true
	}
	exporter := &Exporter{config: resolved, client: client, ownsClient: ownsClient}
	if resolved.ValidateCredentials {
		ctx := context.Background()
		var cancel context.CancelFunc
		if resolved.Timeout > 0 {
			ctx, cancel = context.WithTimeout(ctx, resolved.Timeout)
			defer cancel()
		}
		if err := exporter.validateCredentials(ctx); err != nil {
			if ownsClient {
				_ = exporter.Shutdown(context.Background())
			}
			return nil, err
		}
	}
	return exporter, nil
}

func ResolveConfig(config Config) (Config, error) {
	cfg := config
	cfg.APIKey = firstConfigValue(cfg.APIKey, os.Getenv("DD_API_KEY"))
	cfg.Site = firstConfigValue(cfg.Site, os.Getenv("DD_SITE"), defaultSite)
	cfg.Endpoint = firstConfigValue(cfg.Endpoint, os.Getenv("EINO_OBS_DATADOG_ENDPOINT"))
	cfg.MLApp = firstConfigValue(cfg.MLApp, os.Getenv("DD_LLMOBS_ML_APP"), os.Getenv("EINO_OBS_ML_APP"), cfg.Service, os.Getenv("DD_SERVICE"), defaultMLApp)
	cfg.Service = firstConfigValue(cfg.Service, os.Getenv("DD_SERVICE"), defaultService)
	cfg.Env = firstConfigValue(cfg.Env, os.Getenv("DD_ENV"))
	cfg.Version = firstConfigValue(cfg.Version, os.Getenv("DD_VERSION"))

	if cfg.TimeoutOverride != nil {
		cfg.Timeout = *cfg.TimeoutOverride
	} else if cfg.Timeout == 0 {
		duration, err := durationEnv("EINO_OBS_EXPORT_TIMEOUT", defaultTimeout)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.Timeout = duration
	}
	if cfg.BatchSizeOverride != nil {
		cfg.BatchSize = *cfg.BatchSizeOverride
	} else if cfg.BatchSize == 0 {
		value, err := positiveIntEnv("EINO_OBS_EXPORT_BATCH_SIZE", defaultBatchSize)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.BatchSize = value
	}
	if cfg.MaxPayloadBytesOverride != nil {
		cfg.MaxPayloadBytes = *cfg.MaxPayloadBytesOverride
	} else if cfg.MaxPayloadBytes == 0 {
		value, err := positiveIntEnv("EINO_OBS_EXPORT_MAX_PAYLOAD_BYTES", defaultPayloadBytes)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.MaxPayloadBytes = value
	}
	if cfg.MaxRetriesOverride != nil {
		cfg.MaxRetries = *cfg.MaxRetriesOverride
	} else if cfg.MaxRetries == 0 {
		value, err := nonNegativeIntEnv("EINO_OBS_EXPORT_MAX_RETRIES", defaultMaxRetries)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.MaxRetries = value
	}
	if cfg.RetryBaseDelayOverride != nil {
		cfg.RetryBaseDelay = *cfg.RetryBaseDelayOverride
	} else if cfg.RetryBaseDelay == 0 {
		duration, err := durationEnv("EINO_OBS_EXPORT_RETRY_BASE_DELAY", defaultRetryBase)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.RetryBaseDelay = duration
	}
	if cfg.RetryMaxDelayOverride != nil {
		cfg.RetryMaxDelay = *cfg.RetryMaxDelayOverride
	} else if cfg.RetryMaxDelay == 0 {
		duration, err := durationEnv("EINO_OBS_EXPORT_RETRY_MAX_DELAY", defaultRetryMax)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.RetryMaxDelay = duration
	}
	if cfg.ValidateCredentialsOverride != nil {
		cfg.ValidateCredentials = *cfg.ValidateCredentialsOverride
	} else {
		validateCredentials, err := boolEnv("EINO_OBS_VALIDATE_CREDENTIALS", cfg.ValidateCredentials)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.ValidateCredentials = validateCredentials
	}
	if cfg.DisableCompressionOverride != nil {
		cfg.DisableCompression = *cfg.DisableCompressionOverride
	} else {
		disableCompression, err := boolEnv("EINO_OBS_EXPORT_DISABLE_COMPRESSION", cfg.DisableCompression)
		if err != nil {
			return Config{}, invalidConfig(err)
		}
		cfg.DisableCompression = disableCompression
	}

	endpoint, err := resolveEndpoint(cfg.Site, cfg.Endpoint)
	if err != nil {
		return Config{}, invalidConfig(err)
	}
	cfg.Endpoint = endpoint
	if err := validateResolvedConfig(cfg); err != nil {
		return Config{}, invalidConfig(err)
	}
	return cfg, nil
}

func (e *Exporter) Config() Config {
	if e == nil {
		return Config{}
	}
	return e.config
}

func (e *Exporter) HTTPClient() *http.Client {
	if e == nil {
		return nil
	}
	return e.client
}

func (e *Exporter) Export(ctx context.Context, batch []einoobs.Observation) error {
	if e == nil {
		return nil
	}
	spans := make([]model.Span, 0, len(batch))
	for _, observation := range batch {
		span, err := redaction.ApplySpan(internalrecorder.PublicObservationToSpan(observation), redaction.Options{})
		if err != nil {
			return observationError("export", "redaction", err, false, true)
		}
		spans = append(spans, span)
	}
	payload := buildPayload(e.config, spans)
	if len(payload.Spans) == 0 {
		return nil
	}
	return e.postPayload(ctx, payload, "export")
}

func (e *Exporter) validateCredentials(ctx context.Context) error {
	start := time.Now().UTC()
	span := model.NewSpan(model.ObservationIdentity{ID: "credential-validation", TraceID: "credential-validation"}, model.SpanKindExportFlush, "credential_validation", start)
	span.End(start, model.StatusOK)
	return e.postPayload(ctx, buildPayload(e.config, []model.Span{span}), "credential_validation")
}

func (e *Exporter) postPayload(ctx context.Context, payload payload, operation string) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return observationError(operation, "exporter_failure", err, false, true)
	}
	if !e.config.DisableCompression {
		var compressed bytes.Buffer
		gzipWriter := gzip.NewWriter(&compressed)
		if _, err := gzipWriter.Write(body); err != nil {
			return observationError(operation, "exporter_failure", err, false, true)
		}
		if err := gzipWriter.Close(); err != nil {
			return observationError(operation, "exporter_failure", err, false, true)
		}
		body = compressed.Bytes()
	}
	var lastErr error
	attempts := e.config.MaxRetries + 1
	for attempt := 1; attempt <= attempts; attempt++ {
		lastErr = e.sendPayload(ctx, body, operation)
		if lastErr == nil {
			return nil
		}
		var obsErr einoobs.ObservationError
		if !errors.As(lastErr, &obsErr) || !obsErr.Retryable || attempt == attempts {
			return lastErr
		}
		if err := e.sleepBeforeRetry(ctx, operation, attempt); err != nil {
			return err
		}
	}
	return lastErr
}

func (e *Exporter) sendPayload(ctx context.Context, body []byte, operation string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.config.Endpoint, bytes.NewReader(body))
	if err != nil {
		return observationError(operation, "invalid_config", err, false, true)
	}
	req.Header.Set("DD-API-KEY", e.config.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "eino-obs/"+firstConfigValue(e.config.Version, unknownClientVersion))
	if e.config.DisableCompression {
		req.Header.Set("Accept-Encoding", "identity")
	} else {
		req.Header.Set("Content-Encoding", "gzip")
	}
	resp, err := e.client.Do(req)
	if err != nil {
		classification, retryable, dropped := classifyTransportError(ctx, err)
		return observationError(operation, classification, err, retryable, dropped)
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	classification, retryable, dropped := classifyStatus(resp.StatusCode)
	return observationError(operation, classification, fmt.Errorf("Datadog intake returned HTTP %d", resp.StatusCode), retryable, dropped)
}

func (e *Exporter) sleepBeforeRetry(ctx context.Context, operation string, retryIndex int) error {
	delay := e.retryDelay(retryIndex)
	if delay <= 0 {
		return nil
	}
	sleeper := e.config.Sleeper
	if sleeper == nil {
		sleeper = realSleeper{}
	}
	if err := sleeper.Sleep(ctx, delay); err != nil {
		return observationError(operation, classifyContextError(err), err, false, false)
	}
	return nil
}

func (e *Exporter) retryDelay(retryIndex int) time.Duration {
	if e.config.RetryBaseDelay <= 0 {
		return 0
	}
	delay := e.config.RetryBaseDelay
	for i := 1; i < retryIndex; i++ {
		if e.config.RetryMaxDelay > 0 && delay >= e.config.RetryMaxDelay/2 {
			delay = e.config.RetryMaxDelay
			break
		}
		delay *= 2
	}
	if e.config.RetryMaxDelay > 0 && delay > e.config.RetryMaxDelay {
		delay = e.config.RetryMaxDelay
	}
	jitterLimit := delay / 2
	if jitterLimit <= 0 {
		return delay
	}
	seed := e.config.RetryJitterSeed
	if seed == 0 {
		seed = time.Now().UnixNano()
	}
	jitter := time.Duration(rand.New(rand.NewSource(seed + int64(retryIndex))).Int63n(int64(jitterLimit) + 1))
	if e.config.RetryMaxDelay > 0 && delay+jitter > e.config.RetryMaxDelay {
		return e.config.RetryMaxDelay
	}
	return delay + jitter
}

func classifyTransportError(ctx context.Context, err error) (string, bool, bool) {
	if errors.Is(ctx.Err(), context.Canceled) {
		return "canceled", false, false
	}
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return "timeout", false, false
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return "timeout", true, false
	}
	var temporary interface{ Temporary() bool }
	if errors.As(err, &temporary) && temporary.Temporary() {
		return "exporter_failure", true, false
	}
	return "exporter_failure", false, true
}

func classifyStatus(status int) (string, bool, bool) {
	switch {
	case status == http.StatusRequestTimeout:
		return "timeout", true, false
	case status == http.StatusTooManyRequests:
		return "rate_limit", true, false
	case status >= 500 && status <= 599:
		return "exporter_failure", true, false
	case status == http.StatusUnauthorized || status == http.StatusForbidden:
		return "auth", false, true
	case status == http.StatusRequestEntityTooLarge:
		return "payload_too_large", false, true
	case status == http.StatusNotFound || status == http.StatusBadRequest:
		return "invalid_config", false, true
	default:
		return "exporter_failure", false, true
	}
}

func classifyContextError(err error) string {
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	return "exporter_failure"
}

func observationError(operation string, classification string, err error, retryable bool, dropped bool) einoobs.ObservationError {
	return einoobs.ObservationError{
		Operation:      operation,
		Classification: classification,
		Err:            err,
		Retryable:      retryable,
		Dropped:        dropped,
	}
}

type realSleeper struct{}

func (realSleeper) Sleep(ctx context.Context, delay time.Duration) error {
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (e *Exporter) Flush(context.Context) error {
	return nil
}

func (e *Exporter) Shutdown(context.Context) error {
	if e == nil || e.client == nil || !e.ownsClient {
		return nil
	}
	e.client.CloseIdleConnections()
	return nil
}

func Duration(value time.Duration) *time.Duration {
	return &value
}

func Int(value int) *int {
	return &value
}

func Bool(value bool) *bool {
	return &value
}

func validateResolvedConfig(cfg Config) error {
	if cfg.APIKey == "" && !cfg.AllowMissingAPIKeyForTesting {
		return errors.New("missing Datadog API key")
	}
	if cfg.APIKey == "" && !localhostEndpoint(cfg.Endpoint) {
		return errors.New("missing Datadog API key is allowed only for localhost endpoint overrides")
	}
	if cfg.MLApp == "" {
		return errors.New("ml app is required")
	}
	if cfg.Timeout < 0 {
		return errors.New("timeout must be non-negative")
	}
	if cfg.BatchSize <= 0 {
		return errors.New("batch size must be positive")
	}
	if cfg.MaxPayloadBytes <= 0 {
		return errors.New("max payload bytes must be positive")
	}
	if cfg.MaxRetries < 0 {
		return errors.New("max retries must be non-negative")
	}
	if cfg.RetryBaseDelay < 0 || cfg.RetryMaxDelay < 0 {
		return errors.New("retry delays must be non-negative")
	}
	return nil
}

func resolveEndpoint(site string, endpoint string) (string, error) {
	if endpoint != "" {
		return normalizeEndpoint(endpoint)
	}
	host, ok := siteHosts()[strings.TrimSpace(site)]
	if !ok {
		return "", fmt.Errorf("unsupported Datadog site %q", site)
	}
	return normalizeEndpoint(host)
}

func normalizeEndpoint(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return "", fmt.Errorf("endpoint must use http or https")
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("endpoint host is required")
	}
	if parsed.Scheme == "http" && !localhostHost(parsed.Hostname()) {
		return "", fmt.Errorf("http endpoint overrides are allowed only for localhost")
	}
	if parsed.Path == "" || parsed.Path == "/" {
		parsed.Path = intakePath
	}
	return parsed.String(), nil
}

func siteHosts() map[string]string {
	return map[string]string{
		"datadoghq.com":     "https://api.datadoghq.com",
		"us3.datadoghq.com": "https://api.us3.datadoghq.com",
		"us5.datadoghq.com": "https://api.us5.datadoghq.com",
		"datadoghq.eu":      "https://api.datadoghq.eu",
		"ap1.datadoghq.com": "https://api.ap1.datadoghq.com",
		"ap2.datadoghq.com": "https://api.ap2.datadoghq.com",
		"ddog-gov.com":      "https://api.ddog-gov.com",
	}
}

func firstConfigValue(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func durationEnv(name string, fallback time.Duration) (time.Duration, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	value, err := time.ParseDuration(raw)
	if err != nil {
		return 0, err
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", name)
	}
	return value, nil
}

func positiveIntEnv(name string, fallback int) (int, error) {
	value, ok, err := intEnv(name)
	if err != nil || !ok {
		return fallback, err
	}
	if value <= 0 {
		return 0, fmt.Errorf("%s must be positive", name)
	}
	return value, nil
}

func nonNegativeIntEnv(name string, fallback int) (int, error) {
	value, ok, err := intEnv(name)
	if err != nil || !ok {
		return fallback, err
	}
	if value < 0 {
		return 0, fmt.Errorf("%s must be non-negative", name)
	}
	return value, nil
}

func intEnv(name string) (int, bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return 0, false, nil
	}
	value, err := strconv.Atoi(raw)
	return value, true, err
}

func boolEnv(name string, fallback bool) (bool, error) {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback, nil
	}
	return strconv.ParseBool(raw)
}

func invalidConfig(err error) einoobs.ObservationError {
	return einoobs.ObservationError{
		Operation:      "credential_validation",
		Classification: "invalid_config",
		Err:            err,
		Retryable:      false,
		Dropped:        false,
	}
}

func localhostEndpoint(endpoint string) bool {
	parsed, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	return localhostHost(parsed.Hostname())
}

func localhostHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

var _ einoobs.Exporter = (*Exporter)(nil)
