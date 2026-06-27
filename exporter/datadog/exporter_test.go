package datadog

import (
	"errors"
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

func TestValidateCredentialsFailsClosedWithoutSendingRequest(t *testing.T) {
	clearEnv(t)
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		requests++
	}))
	defer server.Close()

	_, err := New(Config{
		APIKey:              "key",
		Endpoint:            server.URL,
		MLApp:               "app",
		ValidateCredentials: true,
	})
	if err == nil {
		t.Fatalf("New() validation error = nil")
	}
	var obsErr einoobs.ObservationError
	if !errors.As(err, &obsErr) || obsErr.Operation != "credential_validation" || obsErr.Classification != "invalid_config" {
		t.Fatalf("validation error = %#v", err)
	}
	if requests != 0 {
		t.Fatalf("constructor sent %d requests, want none", requests)
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
