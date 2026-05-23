package app

import (
	"context"
	"testing"

	"github.com/johannes-kuhfuss/fileupload-service/config"
)

func TestSetupOtelDisablesTelemetryWhenEndpointIsMissing(t *testing.T) {
	t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	cfg = config.AppConfig{}

	setupOtel()

	if cfg.RunTime.OTelEnabled {
		t.Fatal("OTelEnabled = true, want false")
	}
	if cfg.RunTime.OLog == nil {
		t.Fatal("OLog is nil, want regular logger")
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
