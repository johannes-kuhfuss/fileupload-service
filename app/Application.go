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
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-sanitize/sanitize"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/handler"
	"github.com/johannes-kuhfuss/fileupload-service/service"
	"github.com/johannes-kuhfuss/services_utils/date"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/contrib/instrumentation/github.com/gin-gonic/gin/otelgin"
)

const oTelName = "fileupload-service"

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
	setupOtel()
	cfg.RunTime.OLog.Info("Starting application...")

	getCmdLine()
	err := config.InitConfig(config.EnvFile, &cfg)
	if err != nil {
		panic(err)
	}

	initRouter()
	initServer()
	wireApp()
	mapUrls()
	RegisterForOsSignals()
	createSanitizers()
	go startServer()

	<-appEnd
	cleanUp()

	if srvErr := server.Shutdown(ctx); srvErr != nil {
		cfg.RunTime.OLog.Error("Graceful shutdown failed", slog.String("error", srvErr.Error()))
	} else {
		cfg.RunTime.OLog.Error("Graceful shutdown finished")
	}
}

func getCmdLine() {
	flag.StringVar(&config.EnvFile, "config.file", ".env", "Specify location of config file. Default is .env")
	flag.Parse()
}

func setupOtel() {
	var err error
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	otelShutdown, err = setupOTelSDK(ctx)
	if err != nil {
		fmt.Println("Otel setup went wrong")
	}
	//cfg.RunTime.OTrace = otel.Tracer(oTelName)
	//cfg.RunTime.OMeter = otel.Meter(oTelName)
	cfg.RunTime.OLog = otelslog.NewLogger(oTelName)

	/*
		cfg.Metrics.UploadSuccessCounter, _ = cfg.RunTime.OMeter.Int64Counter("uploadsuccess.counter",
			metric.WithDescription("Number of Successful Uploads"),
			metric.WithUnit("{count}"))
		cfg.Metrics.UploadFailureCounter, _ = cfg.RunTime.OMeter.Int64Counter("uploadfailure.counter",
			metric.WithDescription("Number of Failed Uploads"),
			metric.WithUnit("{count}"))
	*/
}

func initRouter() {
	gin.SetMode(cfg.Gin.Mode)
	router := gin.New()
	router.Use(gin.Recovery())
	router.Use(otelgin.Middleware(oTelName))
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
		ReadTimeout:       5 * time.Second,
		ReadHeaderTimeout: 0,
		WriteTimeout:      5 * time.Second,
		IdleTimeout:       120 * time.Second,
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
	cfg.RunTime.Router.POST("/upload", uploadHandler.Receive)
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
		cfg.RunTime.OLog.Error("Error creating sanitizer", slog.String("Error Message", err.Error()))
		panic(err)
	}
	cfg.RunTime.Sani = sani
}

func startServer() {
	cfg.RunTime.OLog.Info(fmt.Sprintf("Listening on %v", cfg.RunTime.ListenAddr))
	cfg.RunTime.StartDate = date.GetNowUtc()
	if cfg.Server.UseTls {
		if err := server.ListenAndServeTLS(cfg.Server.CertFile, cfg.Server.KeyFile); err != nil && err != http.ErrServerClosed {
			cfg.RunTime.OLog.Error("Error while starting https server", slog.String("Error Message", err.Error()))
			panic(err)
		}
	} else {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			cfg.RunTime.OLog.Error("Error while starting http server", slog.String("Error Message", err.Error()))
			panic(err)
		}
	}
}

func cleanUp() {
	shutdownTime := time.Duration(cfg.Server.GracefulShutdownTime) * time.Second
	ctx, cancel = context.WithTimeout(context.Background(), shutdownTime)
	defer cancel()
	defer func() {
		cfg.RunTime.OLog.Info("Cleaning up")
		otelShutdown(ctx)
		cancel()
	}()
}
