package app

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestSetupOtelDisablesTelemetryWhenEndpointIsMissing(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	cfg = config.AppConfig{}

	setupOtel()

	if cfg.RunTime.OTelEnabled {
		t.Fatal("OTelEnabled = true, want false")
	}
	if cfg.Metrics.UploadSuccessCounter != nil || cfg.Metrics.UploadFailureCounter != nil {
		t.Fatal("metrics counters initialized while OTEL is disabled")
	}
	if otelShutdown == nil {
		t.Fatal("otelShutdown is nil, want no-op shutdown")
	}
	if err := otelShutdown(context.Background()); err != nil {
		t.Fatalf("otelShutdown() error = %v", err)
	}
}

func TestInitServerConfiguresPlainHTTPServer(t *testing.T) {
	cfg = config.AppConfig{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = "18080"
	cfg.Server.ReadTimeoutSeconds = 10
	cfg.Server.WriteTimeoutSeconds = 20
	cfg.Server.IdleTimeoutSeconds = 30
	cfg.RunTime.Router = gin.New()

	initServer()

	if server.Addr != "127.0.0.1:18080" {
		t.Fatalf("server.Addr = %q", server.Addr)
	}
	if server.Handler != cfg.RunTime.Router {
		t.Fatal("server.Handler was not set to runtime router")
	}
	if server.ReadTimeout != 10*time.Second || server.WriteTimeout != 20*time.Second || server.IdleTimeout != 30*time.Second {
		t.Fatalf("timeouts = read %v write %v idle %v", server.ReadTimeout, server.WriteTimeout, server.IdleTimeout)
	}
	if server.TLSConfig != nil {
		t.Fatal("TLSConfig set for plain HTTP server")
	}
}

func TestInitServerConfiguresTLS(t *testing.T) {
	cfg = config.AppConfig{}
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.TlsPort = "18443"
	cfg.Server.UseTls = true
	cfg.RunTime.Router = gin.New()

	initServer()

	if server.Addr != "127.0.0.1:18443" {
		t.Fatalf("server.Addr = %q", server.Addr)
	}
	if server.TLSConfig == nil {
		t.Fatal("TLSConfig is nil")
	}
	if server.TLSConfig.MinVersion == 0 {
		t.Fatal("TLS MinVersion was not configured")
	}
	if server.TLSNextProto == nil {
		t.Fatal("TLSNextProto should be initialized to disable HTTP/2")
	}
}

func TestInitRouterAndMapUrls(t *testing.T) {
	gin.SetMode(gin.TestMode)
	cfg = config.AppConfig{}
	cfg.Gin.Mode = gin.TestMode
	cfg.Gin.TemplatePath = filepath.Join("..", "templates")
	cfg.Upload.UploadPath = t.TempDir()
	cfg.Upload.QuarantinePath = filepath.Join(t.TempDir(), "quarantine")
	cfg.Upload.AllowedExtensions = []string{".wav"}
	cfg.Events.Source = "test"
	cfg.Events.OutboxPath = filepath.Join(t.TempDir(), "events.ndjson")

	initRouter()
	wireApp()
	mapUrls()

	routes := cfg.RunTime.Router.Routes()
	registered := make(map[string]bool, len(routes))
	for _, route := range routes {
		registered[route.Method+" "+route.Path] = true
	}

	expectedRoutes := []string{
		http.MethodPost + " /uploads",
		http.MethodGet + " /uploads/:uploadID",
		http.MethodPatch + " /uploads/:uploadID",
		http.MethodPost + " /uploads/:uploadID/complete",
		http.MethodGet + " /",
		http.MethodGet + " /about",
	}
	for _, route := range expectedRoutes {
		if !registered[route] {
			t.Fatalf("route %s is not registered; routes=%v", route, routes)
		}
	}
	if cfg.RunTime.Router == nil || !slices.Contains(cfg.Upload.AllowedExtensions, ".wav") {
		t.Fatal("router or config unexpectedly changed during wiring")
	}
}

func TestCreateSanitizers(t *testing.T) {
	cfg = config.AppConfig{}

	createSanitizers()

	if cfg.RunTime.Sani == nil {
		t.Fatal("sanitizer was not initialized")
	}
}

func TestRegisterForOsSignalsInitializesChannel(t *testing.T) {
	RegisterForOsSignals()

	if appEnd == nil {
		t.Fatal("appEnd is nil")
	}
}

func TestCleanUpCreatesShutdownContextAndRunsOtelShutdown(t *testing.T) {
	cfg = config.AppConfig{}
	cfg.Server.GracefulShutdownTime = 1
	called := false
	otelShutdown = func(context.Context) error {
		called = true
		return nil
	}

	cleanUp()

	if ctx == nil || cancel == nil {
		t.Fatal("shutdown context was not initialized")
	}
	if !called {
		t.Fatal("otelShutdown was not called")
	}
}

func TestSetupDefaultLogger(t *testing.T) {
	setupDefaultLogger()

	if slog.Default() == nil {
		t.Fatal("default logger is nil")
	}
}

func TestObservableMetricHelpers(t *testing.T) {
	root := t.TempDir()
	cfg = config.AppConfig{}
	cfg.Upload.UploadPath = filepath.Join(root, "uploads")
	cfg.Upload.QuarantinePath = filepath.Join(root, "quarantine")
	cfg.Events.OutboxPath = filepath.Join(root, "events", "upload-events.ndjson")

	if got := uploadMetricRoot(); got != cfg.Upload.QuarantinePath {
		t.Fatalf("uploadMetricRoot() = %q, want %q", got, cfg.Upload.QuarantinePath)
	}
	if got := outboxFileSize(); got != 0 {
		t.Fatalf("outboxFileSize(missing) = %d, want 0", got)
	}

	if err := os.MkdirAll(filepath.Dir(cfg.Events.OutboxPath), 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(cfg.Events.OutboxPath, []byte("event\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outbox) error = %v", err)
	}
	if got := outboxFileSize(); got != 6 {
		t.Fatalf("outboxFileSize() = %d, want 6", got)
	}
}

func TestSessionCountsReportsActiveAndStaleReceivingSessions(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	cfg = config.AppConfig{}
	cfg.Upload.QuarantinePath = filepath.Join(root, "quarantine")

	writeSessionMetadata(t, "active", domain.UploadSession{
		Status:    domain.UploadStatusReceiving,
		UpdatedAt: now.Add(-30 * time.Minute),
	})
	writeSessionMetadata(t, "stale", domain.UploadSession{
		Status:    domain.UploadStatusReceiving,
		UpdatedAt: now.Add(-2 * time.Hour),
	})
	writeSessionMetadata(t, "completed", domain.UploadSession{
		Status:    domain.UploadStatusQuarantined,
		UpdatedAt: now.Add(-2 * time.Hour),
	})

	active, stale := sessionCounts(now)
	if active != 2 || stale != 1 {
		t.Fatalf("sessionCounts() = active %d stale %d, want active 2 stale 1", active, stale)
	}
}

func TestSessionCountsAndOutboxDefaultsHandleMissingPaths(t *testing.T) {
	root := t.TempDir()
	cfg = config.AppConfig{}
	cfg.Upload.UploadPath = filepath.Join(root, "uploads")

	if got := uploadMetricRoot(); got != filepath.Join(cfg.Upload.UploadPath, "quarantine") {
		t.Fatalf("uploadMetricRoot(default) = %q", got)
	}
	if got := outboxFileSize(); got != 0 {
		t.Fatalf("outboxFileSize(default missing) = %d, want 0", got)
	}
	active, stale := sessionCounts(time.Now().UTC())
	if active != 0 || stale != 0 {
		t.Fatalf("sessionCounts(missing) = active %d stale %d, want 0/0", active, stale)
	}
}

func TestRegisterObservableMetricsCollectsObservableValues(t *testing.T) {
	root := t.TempDir()
	cfg = config.AppConfig{}
	cfg.Upload.QuarantinePath = filepath.Join(root, "quarantine")
	cfg.Events.OutboxPath = filepath.Join(root, "events", "upload-events.ndjson")

	if err := os.MkdirAll(filepath.Dir(cfg.Events.OutboxPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(outbox) error = %v", err)
	}
	if err := os.WriteFile(cfg.Events.OutboxPath, []byte("event\n"), 0o644); err != nil {
		t.Fatalf("WriteFile(outbox) error = %v", err)
	}
	writeSessionMetadata(t, "active", domain.UploadSession{
		Status:    domain.UploadStatusReceiving,
		UpdatedAt: time.Now().UTC(),
	})

	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	meter := provider.Meter("test")
	cfg.RunTime.OMeter = meter
	cfg.Metrics.OutboxFileSizeGauge, _ = meter.Int64ObservableGauge("test.outbox.size")
	cfg.Metrics.ActiveSessionsGauge, _ = meter.Int64ObservableGauge("test.sessions.active")
	cfg.Metrics.StaleSessionsGauge, _ = meter.Int64ObservableGauge("test.sessions.stale")

	registerObservableMetrics()

	var metrics metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &metrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
}

func writeSessionMetadata(t *testing.T, uploadID string, session domain.UploadSession) {
	t.Helper()
	sessionPath := filepath.Join(cfg.Upload.QuarantinePath, "_sessions", uploadID, "metadata.json")
	if err := os.MkdirAll(filepath.Dir(sessionPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(session) error = %v", err)
	}
	contents, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("Marshal(session) error = %v", err)
	}
	if err := os.WriteFile(sessionPath, contents, 0o644); err != nil {
		t.Fatalf("WriteFile(session) error = %v", err)
	}
}
