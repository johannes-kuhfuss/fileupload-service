package app

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sanitize/sanitize"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/handler"
	"github.com/johannes-kuhfuss/fileupload-service/service"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

const (
	oTelName = "fileupload-service"
	eMsg     = "Error Message"
)

var (
	cfg           config.AppConfig
	server        http.Server
	appEnd        chan os.Signal
	ctx           context.Context
	cancel        context.CancelFunc
	uploadService service.DefaultUploadService
	uploadHandler handler.UploadHandler
	uiHandler     handler.UiHandler
	otelShutdown  func(context.Context) error
)

func StartApp() {
	setupDefaultLogger()
	slog.Info("Starting application...")

	getCmdLine()
	err := config.InitConfig(config.EnvFile, &cfg)
	if err != nil {
		panic(err)
	}
	setupOtel()

	initRouter()
	initServer()
	wireApp()
	mapUrls()
	RegisterForOsSignals()
	createSanitizers()
	go startServer()

	<-appEnd
	cleanUp()

	if err := server.Shutdown(ctx); err != nil {
		slog.Error("Graceful shutdown failed", slog.String(eMsg, err.Error()))
	} else {
		slog.Info("Graceful shutdown finished")
	}
}

func getCmdLine() {
	flag.StringVar(&config.EnvFile, "config.file", ".env", "Specify location of config file. Default is .env")
	flag.Parse()
}

func setupOtel() {
	var err error
	otelShutdown = func(context.Context) error { return nil }
	if strings.TrimSpace(os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")) == "" {
		slog.Info("OTEL_EXPORTER_OTLP_ENDPOINT is not set. OpenTelemetry is disabled.")
		cfg.RunTime.OTelEnabled = false
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	otelShutdown, err = setupOTelSDK(ctx)
	if err != nil {
		slog.Error("OpenTelemetry setup went wrong", slog.String(eMsg, err.Error()))
		cfg.RunTime.OTelEnabled = false
		return
	}
	cfg.RunTime.OTelEnabled = true
	cfg.RunTime.OTrace = otel.Tracer(oTelName)
	cfg.RunTime.OMeter = otel.Meter(oTelName)
	slog.SetDefault(otelslog.NewLogger(oTelName))

	cfg.Metrics.UploadSuccessCounter, _ = cfg.RunTime.OMeter.Int64Counter("fileupload.uploads.completed",
		metric.WithDescription("Number of uploads completed and quarantined"),
		metric.WithUnit("{count}"))
	cfg.Metrics.UploadFailureCounter, _ = cfg.RunTime.OMeter.Int64Counter("fileupload.uploads.failed",
		metric.WithDescription("Number of upload workflow failures"),
		metric.WithUnit("{count}"))
	cfg.Metrics.SessionsStartedCounter, _ = cfg.RunTime.OMeter.Int64Counter("fileupload.sessions.started",
		metric.WithDescription("Number of upload sessions created"),
		metric.WithUnit("{count}"))
	cfg.Metrics.ChunksAcceptedCounter, _ = cfg.RunTime.OMeter.Int64Counter("fileupload.chunks.accepted",
		metric.WithDescription("Number of upload chunks accepted"),
		metric.WithUnit("{count}"))
	cfg.Metrics.BytesReceivedCounter, _ = cfg.RunTime.OMeter.Int64Counter("fileupload.bytes.received",
		metric.WithDescription("Number of upload bytes written to quarantine storage"),
		metric.WithUnit("By"))
	cfg.Metrics.UploadSizeHistogram, _ = cfg.RunTime.OMeter.Int64Histogram("fileupload.upload.size",
		metric.WithDescription("Declared upload file size"),
		metric.WithUnit("By"))
	cfg.Metrics.ChunkSizeHistogram, _ = cfg.RunTime.OMeter.Int64Histogram("fileupload.chunk.size",
		metric.WithDescription("Accepted upload chunk size"),
		metric.WithUnit("By"))
	cfg.Metrics.UploadDurationHistogram, _ = cfg.RunTime.OMeter.Float64Histogram("fileupload.upload.duration",
		metric.WithDescription("Time from upload session creation to terminal result"),
		metric.WithUnit("s"))
	cfg.Metrics.StageDurationHistogram, _ = cfg.RunTime.OMeter.Float64Histogram("fileupload.stage.duration",
		metric.WithDescription("Duration of internal upload workflow stages"),
		metric.WithUnit("s"))
	cfg.Metrics.OutboxPublishedCounter, _ = cfg.RunTime.OMeter.Int64Counter("fileupload.outbox.events.published",
		metric.WithDescription("Number of upload events written to the outbox"),
		metric.WithUnit("{count}"))
	cfg.Metrics.OutboxFailureCounter, _ = cfg.RunTime.OMeter.Int64Counter("fileupload.outbox.publish.failures",
		metric.WithDescription("Number of failed outbox publish attempts"),
		metric.WithUnit("{count}"))
	cfg.Metrics.OutboxPublishDuration, _ = cfg.RunTime.OMeter.Float64Histogram("fileupload.outbox.publish.duration",
		metric.WithDescription("Duration of outbox publish attempts"),
		metric.WithUnit("s"))
	cfg.Metrics.OutboxFileSizeGauge, _ = cfg.RunTime.OMeter.Int64ObservableGauge("fileupload.outbox.file.size",
		metric.WithDescription("Current size of the upload outbox file"),
		metric.WithUnit("By"))
	cfg.Metrics.ActiveSessionsGauge, _ = cfg.RunTime.OMeter.Int64ObservableGauge("fileupload.sessions.active",
		metric.WithDescription("Current number of upload sessions still receiving data"),
		metric.WithUnit("{count}"))
	cfg.Metrics.StaleSessionsGauge, _ = cfg.RunTime.OMeter.Int64ObservableGauge("fileupload.sessions.stale",
		metric.WithDescription("Current number of receiving upload sessions not updated for more than one hour"),
		metric.WithUnit("{count}"))
	registerObservableMetrics()
}

func initRouter() {
	gin.SetMode(cfg.Gin.Mode)
	router := gin.New()
	router.Use(gin.Recovery())
	if cfg.RunTime.OTelEnabled {
		router.Use(otelgin.Middleware(oTelName))
	}
	router.SetTrustedProxies(nil)
	globPath := filepath.Join(cfg.Gin.TemplatePath, "*.tmpl")
	router.LoadHTMLGlob(globPath)
	router.Static("/bootstrap", "./bootstrap")

	cfg.RunTime.Router = router
}

func initServer() {
	var tlsConfig tls.Config

	if cfg.Server.UseTls {
		tlsConfig = tls.Config{
			CipherSuites: []uint16{
				tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
				tls.TLS_RSA_WITH_AES_256_GCM_SHA384,
			},
			PreferServerCipherSuites: true,
			MinVersion:               tls.VersionTLS12,
			CurvePreferences:         []tls.CurveID{tls.CurveP521, tls.CurveP384, tls.CurveP256},
		}
	}
	if cfg.Server.UseTls {
		cfg.RunTime.ListenAddr = fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.TlsPort)
	} else {
		cfg.RunTime.ListenAddr = fmt.Sprintf("%s:%s", cfg.Server.Host, cfg.Server.Port)
	}

	server = http.Server{
		Addr:              cfg.RunTime.ListenAddr,
		Handler:           cfg.RunTime.Router,
		ReadTimeout:       time.Duration(cfg.Server.ReadTimeoutSeconds) * time.Second,
		ReadHeaderTimeout: 0,
		WriteTimeout:      time.Duration(cfg.Server.WriteTimeoutSeconds) * time.Second,
		IdleTimeout:       time.Duration(cfg.Server.IdleTimeoutSeconds) * time.Second,
		MaxHeaderBytes:    0,
	}
	if cfg.Server.UseTls {
		server.TLSConfig = &tlsConfig
		server.TLSNextProto = make(map[string]func(*http.Server, *tls.Conn, http.Handler))
	}
}

func wireApp() {
	uploadService = service.NewUploadService(&cfg)
	uploadHandler = handler.NewUploadHandler(&cfg, uploadService)
	uiHandler = handler.NewUiHandler(&cfg)
}

func mapUrls() {
	cfg.RunTime.Router.POST("/uploads", uploadHandler.CreateUpload)
	cfg.RunTime.Router.GET("/uploads/:uploadID", uploadHandler.GetUpload)
	cfg.RunTime.Router.PATCH("/uploads/:uploadID", uploadHandler.UploadChunk)
	cfg.RunTime.Router.POST("/uploads/:uploadID/complete", uploadHandler.CompleteUpload)
	cfg.RunTime.Router.GET("/", uiHandler.UploadPage)
	cfg.RunTime.Router.GET("/about", uiHandler.AboutPage)
}

func RegisterForOsSignals() {
	appEnd = make(chan os.Signal, 1)
	signal.Notify(appEnd, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
}

func createSanitizers() {
	sani, err := sanitize.New()
	if err != nil {
		slog.Error("Error creating sanitizer", slog.String(eMsg, err.Error()))
		panic(err)
	}
	cfg.RunTime.Sani = sani
}

func startServer() {
	slog.Info(fmt.Sprintf("Listening on %v", cfg.RunTime.ListenAddr))
	cfg.RunTime.StartDate = time.Now().UTC()
	if cfg.Server.UseTls {
		if err := server.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile); err != nil && err != http.ErrServerClosed {
			slog.Error("Error while starting https server", slog.String(eMsg, err.Error()))
			panic(err)
		}
	} else {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("Error while starting http server", slog.String(eMsg, err.Error()))
			panic(err)
		}
	}
}

func cleanUp() {
	shutdownTime := time.Duration(cfg.Server.GracefulShutdownTime) * time.Second
	ctx, cancel = context.WithTimeout(context.Background(), shutdownTime)
	defer cancel()
	defer func() {
		slog.Info("Cleaning up...")
		if otelShutdown != nil {
			otelShutdown(ctx)
		}
	}()
}

func setupDefaultLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}

func registerObservableMetrics() {
	_, err := cfg.RunTime.OMeter.RegisterCallback(func(ctx context.Context, observer metric.Observer) error {
		if cfg.Metrics.OutboxFileSizeGauge != nil {
			observer.ObserveInt64(cfg.Metrics.OutboxFileSizeGauge, outboxFileSize())
		}
		active, stale := sessionCounts(time.Now().UTC())
		if cfg.Metrics.ActiveSessionsGauge != nil {
			observer.ObserveInt64(cfg.Metrics.ActiveSessionsGauge, active)
		}
		if cfg.Metrics.StaleSessionsGauge != nil {
			observer.ObserveInt64(cfg.Metrics.StaleSessionsGauge, stale)
		}
		return nil
	}, cfg.Metrics.OutboxFileSizeGauge, cfg.Metrics.ActiveSessionsGauge, cfg.Metrics.StaleSessionsGauge)
	if err != nil {
		slog.Warn("Could not register observable metrics", slog.String(eMsg, err.Error()))
	}
}

func outboxFileSize() int64 {
	outboxPath := cfg.Events.OutboxPath
	if outboxPath == "" {
		outboxPath = filepath.Join(uploadMetricRoot(), "events", "upload-events.ndjson")
	}
	info, err := os.Stat(outboxPath)
	if err != nil {
		return 0
	}
	return info.Size()
}

func sessionCounts(now time.Time) (int64, int64) {
	sessionRoot := filepath.Join(uploadMetricRoot(), "_sessions")
	entries, err := os.ReadDir(sessionRoot)
	if err != nil {
		return 0, 0
	}

	var active int64
	var stale int64
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		sessionPath := filepath.Join(sessionRoot, entry.Name(), "metadata.json")
		f, err := os.Open(sessionPath)
		if err != nil {
			continue
		}
		var session domain.UploadSession
		decodeErr := json.NewDecoder(f).Decode(&session)
		closeErr := f.Close()
		if decodeErr != nil || closeErr != nil || session.Status != domain.UploadStatusReceiving {
			continue
		}
		active++
		if now.Sub(session.UpdatedAt) > time.Hour {
			stale++
		}
	}
	return active, stale
}

func uploadMetricRoot() string {
	if cfg.Upload.QuarantinePath != "" {
		return cfg.Upload.QuarantinePath
	}
	return filepath.Join(cfg.Upload.UploadPath, "quarantine")
}
