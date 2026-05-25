package app

import (
	"context"
	"net/http"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
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
