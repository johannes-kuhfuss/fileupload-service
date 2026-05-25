package config

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestInitConfigAppliesDefaultsAndEnvironment(t *testing.T) {
	setValidBaseEnv(t)
	t.Setenv("SERVER_HOST", "127.0.0.1")
	t.Setenv("SERVER_PORT", "18080")
	t.Setenv("UPLOAD_PATH", filepath.Join(t.TempDir(), "uploads"))
	t.Setenv("QUARANTINE_PATH", filepath.Join(t.TempDir(), "quarantine"))
	t.Setenv("ALLOWED_EXTENSIONS", ".wav,.flac")
	t.Setenv("EVENT_SOURCE", "test-source")

	var cfg AppConfig
	if err := InitConfig(filepath.Join(t.TempDir(), "missing.env"), &cfg); err != nil {
		t.Fatalf("InitConfig() error = %v", err)
	}

	if cfg.Server.Host != "127.0.0.1" || cfg.Server.Port != "18080" {
		t.Fatalf("server config = %+v", cfg.Server)
	}
	if cfg.Server.TlsPort != "8443" || cfg.Server.ReadTimeoutSeconds != 1800 || cfg.Server.IdleTimeoutSeconds != 120 {
		t.Fatalf("server defaults not applied: %+v", cfg.Server)
	}
	if !reflect.DeepEqual(cfg.Upload.AllowedExtensions, []string{".wav", ".flac"}) {
		t.Fatalf("AllowedExtensions = %#v", cfg.Upload.AllowedExtensions)
	}
	if cfg.Events.Source != "test-source" {
		t.Fatalf("Events.Source = %q", cfg.Events.Source)
	}
}

func TestInitConfigLoadsEnvFile(t *testing.T) {
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	setValidBaseEnv(t)
	unsetEnv(t, "SERVER_PORT")
	unsetEnv(t, "UPLOAD_PATH")
	if err := os.WriteFile(envFile, []byte("SERVER_PORT=19090\nUPLOAD_PATH="+filepath.Join(dir, "uploads")+"\n"), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	var cfg AppConfig
	if err := InitConfig(envFile, &cfg); err != nil {
		t.Fatalf("InitConfig() error = %v", err)
	}
	if cfg.Server.Port != "19090" {
		t.Fatalf("Server.Port = %q, want 19090", cfg.Server.Port)
	}
	if cfg.Upload.UploadPath != filepath.Join(dir, "uploads") {
		t.Fatalf("Upload.UploadPath = %q", cfg.Upload.UploadPath)
	}
}

func TestInitConfigReturnsEnvconfigError(t *testing.T) {
	setValidBaseEnv(t)
	t.Setenv("SERVER_READ_TIMEOUT_SECONDS", "not-an-int")

	var cfg AppConfig
	if err := InitConfig(filepath.Join(t.TempDir(), "missing.env"), &cfg); err == nil {
		t.Fatal("InitConfig() expected error")
	}
}

func setValidBaseEnv(t *testing.T) {
	t.Helper()
	t.Setenv("GRACEFUL_SHUTDOWN_TIME", "10")
	t.Setenv("SERVER_READ_TIMEOUT_SECONDS", "1800")
	t.Setenv("SERVER_WRITE_TIMEOUT_SECONDS", "1800")
	t.Setenv("SERVER_IDLE_TIMEOUT_SECONDS", "120")
	t.Setenv("USE_TLS", "false")
	t.Setenv("MAX_UPLOAD_BYTES", "0")
}

func TestLoadConfigReturnsMissingFileError(t *testing.T) {
	if err := loadConfig(filepath.Join(t.TempDir(), "missing.env")); err == nil {
		t.Fatal("loadConfig() expected missing file error")
	}
}

func unsetEnv(t *testing.T, key string) {
	t.Helper()
	value, ok := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("Unsetenv(%s) error = %v", key, err)
	}
	t.Cleanup(func() {
		if ok {
			_ = os.Setenv(key, value)
		} else {
			_ = os.Unsetenv(key)
		}
	})
}
