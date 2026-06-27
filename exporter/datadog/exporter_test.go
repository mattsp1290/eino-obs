package datadog

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	einoobs "github.com/mattsp1290/eino-obs"
)

func TestNewResolvesDefaultsAndSiteEndpoint(t *testing.T) {
	clearEnv(t)
	t.Setenv("DD_API_KEY", " env-key ")
	exp, err := New(Config{Service: "svc"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	cfg := exp.Config()
	if cfg.APIKey != "env-key" {
		t.Fatalf("APIKey = %q, want env-key", cfg.APIKey)
	}
	if cfg.Endpoint != "https://api.datadoghq.com/api/intake/llm-obs/v1/trace/spans" {
		t.Fatalf("Endpoint = %q", cfg.Endpoint)
	}
	if cfg.Site != defaultSite || cfg.MLApp != "svc" || cfg.Service != "svc" {
		t.Fatalf("identity config = %#v", cfg)
	}
	if cfg.Timeout != defaultTimeout || cfg.BatchSize != defaultBatchSize || cfg.MaxPayloadBytes != defaultPayloadBytes {
		t.Fatalf("defaults = %#v", cfg)
	}
}

func TestNewAppliesEnvironmentPrecedenceAndParsing(t *testing.T) {
	clearEnv(t)
	t.Setenv("DD_API_KEY", "env-key")
	t.Setenv("DD_SITE", "datadoghq.eu")
	t.Setenv("DD_LLMOBS_ML_APP", "ml-app")
	t.Setenv("DD_SERVICE", "svc-env")
	t.Setenv("DD_ENV", "prod")
	t.Setenv("DD_VERSION", "v1")
	t.Setenv("EINO_OBS_EXPORT_TIMEOUT", "3s")
	t.Setenv("EINO_OBS_EXPORT_BATCH_SIZE", "25")
	t.Setenv("EINO_OBS_EXPORT_MAX_PAYLOAD_BYTES", "2048")
	t.Setenv("EINO_OBS_EXPORT_MAX_RETRIES", "2")
	t.Setenv("EINO_OBS_EXPORT_RETRY_BASE_DELAY", "50ms")
	t.Setenv("EINO_OBS_EXPORT_RETRY_MAX_DELAY", "1s")
	t.Setenv("EINO_OBS_VALIDATE_CREDENTIALS", "true")
	t.Setenv("EINO_OBS_EXPORT_DISABLE_COMPRESSION", "true")

	cfg, err := ResolveConfig(Config{APIKey: "explicit", Site: "us5.datadoghq.com"})
	if err != nil {
		t.Fatalf("ResolveConfig() error = %v", err)
	}
	if cfg.APIKey != "explicit" || cfg.Site != "us5.datadoghq.com" {
		t.Fatalf("explicit precedence failed: %#v", cfg)
	}
	if cfg.Endpoint != "https://api.us5.datadoghq.com/api/intake/llm-obs/v1/trace/spans" ||
		cfg.MLApp != "ml-app" ||
		cfg.Service != "svc-env" ||
		cfg.Env != "prod" ||
		cfg.Version != "v1" {
		t.Fatalf("env config = %#v", cfg)
	}
	if cfg.Timeout != 3*time.Second ||
		cfg.BatchSize != 25 ||
		cfg.MaxPayloadBytes != 2048 ||
		cfg.MaxRetries != 2 ||
		cfg.RetryBaseDelay != 50*time.Millisecond ||
		cfg.RetryMaxDelay != time.Second ||
		!cfg.ValidateCredentials ||
		!cfg.DisableCompression {
		t.Fatalf("parsed config = %#v", cfg)
	}
}

func TestEndpointOverrideRules(t *testing.T) {
	clearEnv(t)
	cfg, err := ResolveConfig(Config{
		Endpoint:                     "http://127.0.0.1:8126",
		MLApp:                        "app",
		AllowMissingAPIKeyForTesting: true,
	})
	if err != nil {
		t.Fatalf("ResolveConfig(localhost) error = %v", err)
	}
	if cfg.Endpoint != "http://127.0.0.1:8126/api/intake/llm-obs/v1/trace/spans" {
		t.Fatalf("Endpoint = %q", cfg.Endpoint)
	}

	cfg, err = ResolveConfig(Config{
		APIKey:   "key",
		Endpoint: "https://example.test/custom/path",
		MLApp:    "app",
	})
	if err != nil {
		t.Fatalf("ResolveConfig(custom path) error = %v", err)
	}
	if cfg.Endpoint != "https://example.test/custom/path" {
		t.Fatalf("custom path Endpoint = %q", cfg.Endpoint)
	}
}

func TestInvalidConfigIsObservationErrorAndDoesNotLeakAPIKey(t *testing.T) {
	clearEnv(t)
	_, err := New(Config{APIKey: "super-secret", Site: "unknown.invalid", MLApp: "app"})
	if err == nil {
		t.Fatalf("New() error = nil")
	}
	var obsErr einoobs.ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("error = %T, want ObservationError", err)
	}
	if obsErr.Operation != "credential_validation" || obsErr.Classification != "invalid_config" || obsErr.Retryable || obsErr.Dropped {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("error leaked API key: %v", err)
	}
}

func TestMissingAPIKeyAllowedOnlyForLocalhostTesting(t *testing.T) {
	clearEnv(t)
	if _, err := New(Config{MLApp: "app"}); err == nil {
		t.Fatalf("New() without key for Datadog endpoint error = nil")
	}
	if _, err := New(Config{
		Endpoint:                     "https://example.test",
		MLApp:                        "app",
		AllowMissingAPIKeyForTesting: true,
	}); err == nil {
		t.Fatalf("New() without key for remote endpoint error = nil")
	}
	if _, err := New(Config{
		Endpoint:                     "http://localhost:8080",
		MLApp:                        "app",
		AllowMissingAPIKeyForTesting: true,
	}); err != nil {
		t.Fatalf("New() localhost missing key error = %v", err)
	}
}

func TestCustomHTTPClientIsRetained(t *testing.T) {
	clearEnv(t)
	transport := &countingTransport{}
	client := &http.Client{Timeout: 42 * time.Second, Transport: transport}
	exp, err := New(Config{
		APIKey:     "key",
		MLApp:      "app",
		HTTPClient: client,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if exp.HTTPClient() != client {
		t.Fatalf("HTTPClient() did not return supplied client")
	}
	if err := exp.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
	if transport.closeIdleCalls != 0 {
		t.Fatalf("custom transport CloseIdleConnections calls = %d, want 0", transport.closeIdleCalls)
	}
}

func TestExplicitZeroAndFalseOverridesWinOverEnvironment(t *testing.T) {
	clearEnv(t)
	t.Setenv("DD_API_KEY", "key")
	t.Setenv("EINO_OBS_EXPORT_MAX_RETRIES", "5")
	t.Setenv("EINO_OBS_EXPORT_RETRY_BASE_DELAY", "500ms")
	t.Setenv("EINO_OBS_VALIDATE_CREDENTIALS", "true")
	t.Setenv("EINO_OBS_EXPORT_DISABLE_COMPRESSION", "true")
	zero := 0
	zeroDelay := time.Duration(0)
	falseValue := false

	cfg, err := ResolveConfig(Config{
		MLApp:                        "app",
		MaxRetriesOverride:           &zero,
		RetryBaseDelayOverride:       &zeroDelay,
		ValidateCredentialsOverride:  &falseValue,
		DisableCompressionOverride:   &falseValue,
		AllowMissingAPIKeyForTesting: false,
		MaxPayloadBytesOverride:      Int(128),
		BatchSizeOverride:            Int(1),
		RetryMaxDelayOverride:        Duration(0),
		TimeoutOverride:              Duration(0),
	})
	if err != nil {
		t.Fatalf("ResolveConfig() error = %v", err)
	}
	if cfg.MaxRetries != 0 || cfg.RetryBaseDelay != 0 || cfg.ValidateCredentials || cfg.DisableCompression {
		t.Fatalf("overrides not honored: %#v", cfg)
	}
}

func TestValidateCredentialsPostsMinimalPayload(t *testing.T) {
	clearEnv(t)
	var requests int
	var got payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Header.Get("DD-API-KEY") != "key" {
			t.Fatalf("DD-API-KEY = %q, want key", r.Header.Get("DD-API-KEY"))
		}
		gzipReader, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("gzip.NewReader() error = %v", err)
		}
		defer gzipReader.Close()
		if err := json.NewDecoder(gzipReader).Decode(&got); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exp, err := New(Config{
		APIKey:              "key",
		Endpoint:            server.URL,
		MLApp:               "app",
		ValidateCredentials: true,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if exp == nil {
		t.Fatalf("New() exporter = nil")
	}
	if requests != 1 {
		t.Fatalf("constructor sent %d requests, want 1", requests)
	}
	if len(got.Spans) != 1 || got.Spans[0].Name != "credential_validation" {
		t.Fatalf("validation payload = %#v", got)
	}
}

func TestValidateCredentialsAllowsExplicitZeroTimeout(t *testing.T) {
	clearEnv(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if err := r.Context().Err(); err != nil {
			t.Fatalf("request context error = %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	exp, err := New(Config{
		APIKey:              "key",
		Endpoint:            server.URL,
		MLApp:               "app",
		ValidateCredentials: true,
		TimeoutOverride:     Duration(0),
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if exp.Config().Timeout != 0 {
		t.Fatalf("Timeout = %v, want 0", exp.Config().Timeout)
	}
}

func TestValidateCredentialsClassifiesAuth(t *testing.T) {
	clearEnv(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()

	_, err := New(Config{
		APIKey:              "key",
		Endpoint:            server.URL,
		MLApp:               "app",
		ValidateCredentials: true,
	})
	var obsErr einoobs.ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("New() error = %T, want ObservationError", err)
	}
	if obsErr.Operation != "credential_validation" || obsErr.Classification != "auth" || obsErr.Retryable || !obsErr.Dropped {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
}

type countingTransport struct {
	closeIdleCalls int
}

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("unexpected request")
}

func (t *countingTransport) CloseIdleConnections() {
	t.closeIdleCalls++
}

var _ http.RoundTripper = (*countingTransport)(nil)
var _ interface{ CloseIdleConnections() } = (*countingTransport)(nil)

func TestOwnedClientShutdownClosesIdleConnections(t *testing.T) {
	clearEnv(t)
	exp, err := New(Config{APIKey: "key", MLApp: "app"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := exp.Shutdown(t.Context()); err != nil {
		t.Fatalf("Shutdown() error = %v", err)
	}
}

func TestConstructorDoesNotDialEndpoint(t *testing.T) {
	clearEnv(t)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen() error = %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	if _, err := New(Config{
		APIKey:   "key",
		Endpoint: "http://" + addr,
		MLApp:    "app",
	}); err != nil {
		t.Fatalf("New() unexpectedly dialed closed endpoint or failed config: %v", err)
	}
}

func TestExportPostsMappedPayload(t *testing.T) {
	clearEnv(t)
	var got struct {
		Method    string
		APIKey    string
		UserAgent string
		Path      string
		Content   string
		Encoding  string
		Payload   payload
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Method = r.Method
		got.APIKey = r.Header.Get("DD-API-KEY")
		got.UserAgent = r.Header.Get("User-Agent")
		got.Path = r.URL.Path
		got.Content = r.Header.Get("Content-Type")
		got.Encoding = r.Header.Get("Content-Encoding")
		body := io.Reader(r.Body)
		if got.Encoding == "gzip" {
			gzipReader, err := gzip.NewReader(r.Body)
			if err != nil {
				t.Fatalf("gzip.NewReader() error = %v", err)
			}
			defer gzipReader.Close()
			body = gzipReader
		}
		if err := json.NewDecoder(body).Decode(&got.Payload); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	exp, err := New(Config{APIKey: "key", Endpoint: server.URL, MLApp: "app", Version: "v1"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	start := time.Date(2026, 6, 26, 12, 0, 0, 0, time.UTC)

	err = exp.Export(t.Context(), []einoobs.Observation{{
		ID:            "span-1",
		TraceID:       "trace-1",
		Kind:          "run",
		Name:          "run",
		Status:        "ok",
		Timestamp:     start,
		Duration:      time.Millisecond,
		DurationKnown: true,
		Attributes:    map[string]any{"correlation.session_id": "session-1"},
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if got.Method != http.MethodPost ||
		got.Path != intakePath ||
		got.APIKey != "key" ||
		got.UserAgent != "eino-obs/v1" ||
		got.Content != "application/json" ||
		got.Encoding != "gzip" {
		t.Fatalf("request metadata = %#v", got)
	}
	if len(got.Payload.Spans) != 1 {
		t.Fatalf("payload spans = %d, want 1", len(got.Payload.Spans))
	}
	span := got.Payload.Spans[0]
	if span.SpanID != "span-1" || span.TraceID != "trace-1" || span.MLApp != "app" || span.Meta["kind"] != "workflow" {
		t.Fatalf("payload span = %#v", span)
	}
}

func TestExportAppliesRedactionBeforePost(t *testing.T) {
	clearEnv(t)
	var got payload
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gzipReader, err := gzip.NewReader(r.Body)
		if err != nil {
			t.Fatalf("gzip.NewReader() error = %v", err)
		}
		defer gzipReader.Close()
		if err := json.NewDecoder(gzipReader).Decode(&got); err != nil {
			t.Fatalf("Decode() error = %v", err)
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	exp, err := New(Config{APIKey: "key", Endpoint: server.URL, MLApp: "app"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = exp.Export(t.Context(), []einoobs.Observation{{
		ID:            "span-1",
		TraceID:       "trace-1",
		Kind:          "run",
		Name:          "run",
		Status:        "ok",
		Timestamp:     time.Now().UTC(),
		Duration:      time.Millisecond,
		DurationKnown: true,
		Attributes: map[string]any{
			"metadata.api_key": "secret",
			"prompt.text":      "raw prompt",
			"metadata.safe":    "safe",
		},
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if len(got.Spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(got.Spans))
	}
	meta := got.Spans[0].Meta
	if _, ok := meta["metadata.api_key"]; ok {
		t.Fatalf("sensitive metadata leaked: %#v", meta)
	}
	if _, ok := meta["prompt.text"]; ok {
		t.Fatalf("raw prompt leaked: %#v", meta)
	}
	if meta["metadata.safe"] != "safe" {
		t.Fatalf("safe metadata = %#v", meta)
	}
	if _, ok := meta["metadata.redaction.records"]; !ok {
		t.Fatalf("redaction records missing: %#v", meta)
	}
}

func TestExportSkipsActiveSpans(t *testing.T) {
	clearEnv(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()
	exp, err := New(Config{APIKey: "key", Endpoint: server.URL, MLApp: "app"})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = exp.Export(t.Context(), []einoobs.Observation{{
		ID:        "span-1",
		TraceID:   "trace-1",
		Kind:      "run",
		Name:      "run",
		Status:    "ok",
		Timestamp: time.Now(),
	}})
	if err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if requests != 0 {
		t.Fatalf("requests = %d, want 0", requests)
	}
}

func TestExportClassifiesHTTPStatus(t *testing.T) {
	clearEnv(t)
	tests := []struct {
		name           string
		status         int
		classification string
		retryable      bool
		dropped        bool
	}{
		{name: "auth", status: http.StatusForbidden, classification: "auth", dropped: true},
		{name: "rate limit", status: http.StatusTooManyRequests, classification: "rate_limit", retryable: true},
		{name: "server", status: http.StatusInternalServerError, classification: "exporter_failure", retryable: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			defer server.Close()
			exp, err := New(Config{APIKey: "key", Endpoint: server.URL, MLApp: "app", MaxRetriesOverride: Int(0)})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			err = exp.Export(t.Context(), []einoobs.Observation{endedRunObservation()})
			var obsErr einoobs.ObservationError
			if !errors.As(err, &obsErr) {
				t.Fatalf("Export() error = %T, want ObservationError", err)
			}
			if obsErr.Operation != "export" || obsErr.Classification != tt.classification || obsErr.Retryable != tt.retryable || obsErr.Dropped != tt.dropped {
				t.Fatalf("ObservationError = %#v", obsErr)
			}
		})
	}
}

func TestExportClassifiesTransportErrors(t *testing.T) {
	clearEnv(t)
	tests := []struct {
		name           string
		err            error
		classification string
		retryable      bool
		dropped        bool
	}{
		{name: "timeout", err: timeoutErr{}, classification: "timeout", retryable: true},
		{name: "temporary", err: temporaryErr{}, classification: "exporter_failure", retryable: true},
		{name: "permanent", err: errors.New("permanent"), classification: "exporter_failure", dropped: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			exp, err := New(Config{
				APIKey:             "key",
				Endpoint:           "https://example.test",
				MLApp:              "app",
				HTTPClient:         &http.Client{Transport: errorTransport{err: tt.err}},
				MaxRetriesOverride: Int(0),
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			err = exp.Export(t.Context(), []einoobs.Observation{endedRunObservation()})
			var obsErr einoobs.ObservationError
			if !errors.As(err, &obsErr) {
				t.Fatalf("Export() error = %T, want ObservationError", err)
			}
			if obsErr.Classification != tt.classification || obsErr.Retryable != tt.retryable || obsErr.Dropped != tt.dropped {
				t.Fatalf("ObservationError = %#v", obsErr)
			}
		})
	}
}

func TestExportRetriesRetryableStatusWithBackoff(t *testing.T) {
	clearEnv(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		if requests < 3 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer server.Close()
	sleeper := &recordingSleeper{}
	exp, err := New(Config{
		APIKey:                 "key",
		Endpoint:               server.URL,
		MLApp:                  "app",
		MaxRetriesOverride:     Int(3),
		RetryBaseDelayOverride: Duration(10 * time.Millisecond),
		RetryMaxDelayOverride:  Duration(25 * time.Millisecond),
		RetryJitterSeed:        7,
		Sleeper:                sleeper,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	if err := exp.Export(t.Context(), []einoobs.Observation{endedRunObservation()}); err != nil {
		t.Fatalf("Export() error = %v", err)
	}
	if requests != 3 {
		t.Fatalf("requests = %d, want 3", requests)
	}
	if len(sleeper.delays) != 2 {
		t.Fatalf("delays = %v, want 2 entries", sleeper.delays)
	}
	if sleeper.delays[0] < 10*time.Millisecond || sleeper.delays[0] > 15*time.Millisecond {
		t.Fatalf("first delay = %v, want base delay plus bounded jitter", sleeper.delays[0])
	}
	if sleeper.delays[1] < 20*time.Millisecond || sleeper.delays[1] > 25*time.Millisecond {
		t.Fatalf("second delay = %v, want exponential delay capped by max", sleeper.delays[1])
	}
}

func TestExportDoesNotRetryPermanentStatus(t *testing.T) {
	clearEnv(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusForbidden)
	}))
	defer server.Close()
	sleeper := &recordingSleeper{}
	exp, err := New(Config{
		APIKey:             "key",
		Endpoint:           server.URL,
		MLApp:              "app",
		MaxRetriesOverride: Int(3),
		Sleeper:            sleeper,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = exp.Export(t.Context(), []einoobs.Observation{endedRunObservation()})
	var obsErr einoobs.ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("Export() error = %T, want ObservationError", err)
	}
	if requests != 1 || len(sleeper.delays) != 0 {
		t.Fatalf("requests = %d delays = %v, want one send and no sleeps", requests, sleeper.delays)
	}
	if obsErr.Classification != "auth" || obsErr.Retryable || !obsErr.Dropped {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
}

func TestExportStopsAfterRetryExhaustion(t *testing.T) {
	clearEnv(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer server.Close()
	sleeper := &recordingSleeper{}
	exp, err := New(Config{
		APIKey:                 "key",
		Endpoint:               server.URL,
		MLApp:                  "app",
		MaxRetriesOverride:     Int(2),
		RetryBaseDelayOverride: Duration(time.Millisecond),
		RetryMaxDelayOverride:  Duration(time.Millisecond),
		RetryJitterSeed:        3,
		Sleeper:                sleeper,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = exp.Export(t.Context(), []einoobs.Observation{endedRunObservation()})
	var obsErr einoobs.ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("Export() error = %T, want ObservationError", err)
	}
	if requests != 3 || len(sleeper.delays) != 2 {
		t.Fatalf("requests = %d delays = %v, want three sends and two sleeps", requests, sleeper.delays)
	}
	if obsErr.Classification != "rate_limit" || !obsErr.Retryable || obsErr.Dropped {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
}

func TestExportStopsWhenRetrySleepIsCanceled(t *testing.T) {
	clearEnv(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()
	exp, err := New(Config{
		APIKey:                 "key",
		Endpoint:               server.URL,
		MLApp:                  "app",
		MaxRetriesOverride:     Int(3),
		RetryBaseDelayOverride: Duration(time.Millisecond),
		Sleeper:                &recordingSleeper{err: context.Canceled},
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	err = exp.Export(t.Context(), []einoobs.Observation{endedRunObservation()})
	var obsErr einoobs.ObservationError
	if !errors.As(err, &obsErr) {
		t.Fatalf("Export() error = %T, want ObservationError", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
	if obsErr.Operation != "export" || obsErr.Classification != "canceled" || obsErr.Retryable || obsErr.Dropped {
		t.Fatalf("ObservationError = %#v", obsErr)
	}
}

type recordingSleeper struct {
	delays []time.Duration
	err    error
}

func (s *recordingSleeper) Sleep(_ context.Context, delay time.Duration) error {
	s.delays = append(s.delays, delay)
	return s.err
}

type errorTransport struct {
	err error
}

func (t errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, t.err
}

type timeoutErr struct{}

func (timeoutErr) Error() string   { return "timeout" }
func (timeoutErr) Timeout() bool   { return true }
func (timeoutErr) Temporary() bool { return true }

type temporaryErr struct{}

func (temporaryErr) Error() string   { return "temporary" }
func (temporaryErr) Timeout() bool   { return false }
func (temporaryErr) Temporary() bool { return true }

func TestDatadogExporterDoesNotAffectDefaultNoNetworkMode(t *testing.T) {
	observer := einoobs.New(einoobs.Config{})
	if _, ok := observer.Exporter().(*einoobs.NoNetworkExporter); !ok {
		t.Fatalf("default exporter = %T, want no-network exporter", observer.Exporter())
	}
}

func endedRunObservation() einoobs.Observation {
	return einoobs.Observation{
		ID:            "span-1",
		TraceID:       "trace-1",
		Kind:          "run",
		Name:          "run",
		Status:        "ok",
		Timestamp:     time.Now().UTC(),
		Duration:      time.Millisecond,
		DurationKnown: true,
	}
}

func clearEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{
		"DD_API_KEY",
		"DD_SITE",
		"EINO_OBS_DATADOG_ENDPOINT",
		"DD_LLMOBS_ML_APP",
		"EINO_OBS_ML_APP",
		"DD_SERVICE",
		"DD_ENV",
		"DD_VERSION",
		"EINO_OBS_EXPORT_TIMEOUT",
		"EINO_OBS_EXPORT_BATCH_SIZE",
		"EINO_OBS_EXPORT_MAX_PAYLOAD_BYTES",
		"EINO_OBS_EXPORT_MAX_RETRIES",
		"EINO_OBS_EXPORT_RETRY_BASE_DELAY",
		"EINO_OBS_EXPORT_RETRY_MAX_DELAY",
		"EINO_OBS_VALIDATE_CREDENTIALS",
		"EINO_OBS_EXPORT_DISABLE_COMPRESSION",
	} {
		t.Setenv(name, "")
	}
}
