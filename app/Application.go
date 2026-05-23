package app

import (
	"context"
	"crypto/tls"
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
	msg := "Starting application..."
	slog.Info(msg)

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
		msg := "Graceful shutdown failed"
		slog.Error(msg, slog.String(eMsg, err.Error()))
	} else {
		msg := "Graceful shutdown finished"
		slog.Info(msg)
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
		msg := "OTEL_EXPORTER_OTLP_ENDPOINT is not set. OpenTelemetry is disabled."
		slog.Info(msg)
		cfg.RunTime.OTelEnabled = false
		return
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	otelShutdown, err = setupOTelSDK(ctx)
	if err != nil {
		slog.Error("Otel setup went wrong", slog.String(eMsg, err.Error()))
		cfg.RunTime.OTelEnabled = false
		return
	}
	cfg.RunTime.OTelEnabled = true
	cfg.RunTime.OTrace = otel.Tracer(oTelName)
	cfg.RunTime.OMeter = otel.Meter(oTelName)
	slog.SetDefault(otelslog.NewLogger(oTelName))

	cfg.Metrics.UploadSuccessCounter, _ = cfg.RunTime.OMeter.Int64Counter("uploadsuccess.counter",
		metric.WithDescription("Number of Successful Uploads"),
		metric.WithUnit("{count}"))
	cfg.Metrics.UploadFailureCounter, _ = cfg.RunTime.OMeter.Int64Counter("uploadfailure.counter",
		metric.WithDescription("Number of Failed Uploads"),
		metric.WithUnit("{count}"))
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
	cfg.RunTime.Router.GET("/files", uiHandler.UploadListPage)
	cfg.RunTime.Router.GET("/about", uiHandler.AboutPage)
}

func RegisterForOsSignals() {
	appEnd = make(chan os.Signal, 1)
	signal.Notify(appEnd, os.Interrupt, syscall.SIGINT, syscall.SIGTERM)
}

func createSanitizers() {
	sani, err := sanitize.New()
	if err != nil {
		msg := "Error creating sanitizer"
		slog.Error(msg, slog.String(eMsg, err.Error()))
		panic(err)
	}
	cfg.RunTime.Sani = sani
}

func startServer() {
	msg := fmt.Sprintf("Listening on %v", cfg.RunTime.ListenAddr)
	slog.Info(msg)
	cfg.RunTime.StartDate = time.Now().UTC()
	if cfg.Server.UseTls {
		if err := server.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile); err != nil && err != http.ErrServerClosed {
			msg := "Error while starting https server"
			slog.Error(msg, slog.String(eMsg, err.Error()))
			panic(err)
		}
	} else {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			msg := "Error while starting http server"
			slog.Error(msg, slog.String(eMsg, err.Error()))
			panic(err)
		}
	}
}

func cleanUp() {
	shutdownTime := time.Duration(cfg.Server.GracefulShutdownTime) * time.Second
	ctx, cancel = context.WithTimeout(context.Background(), shutdownTime)
	defer cancel()
	defer func() {
		msg := "Cleaning up..."
		slog.Info(msg)
		if otelShutdown != nil {
			otelShutdown(ctx)
		}
	}()
}

func setupDefaultLogger() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
}
