package helper

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"

	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/services_utils/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

type XcodeRequest struct {
	SourceFilePath string `json:"source_file_path"`
}

func StartXcode(cfg *config.AppConfig, filePath string) {
	var (
		req XcodeRequest
	)
	logger.Info("Starting to call xcode service")
	xcodeUrl := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(cfg.Xcode.Host, cfg.Xcode.Port),
		Path:   "/xcode",
	}
	ctx := cfg.RunTime.Ctx
	tracer := otel.Tracer("http-client")
	ctx, span := tracer.Start(ctx, "http_request",
		trace.WithAttributes(
			attribute.String("http.url", xcodeUrl.String()),
		),
	)
	defer span.End()

	if cfg.Xcode.Host == "" {
		msg := "No Xcode host configured. Cannot start XCode"
		span.RecordError(errors.New(msg))
		logger.Error(msg, nil)
		return
	}

	req.SourceFilePath = filePath
	reqJson, err := json.Marshal(req)
	if err != nil {
		msg := "Could not create transcode request"
		span.RecordError(err)
		logger.Error(msg, err)
		return
	}
	hc := http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	hreq, err := http.NewRequestWithContext(ctx, "POST", xcodeUrl.String(), bytes.NewBuffer(reqJson))
	if err != nil {
		msg := "Could not create transcode request"
		logger.Error(msg, err)
		span.RecordError(err)
		return
	}
	hreq.Header.Add("Content-Type", "application/json")
	resp, err := hc.Do(hreq)
	if err != nil {
		msg := "Could not send transcode request"
		logger.Error(msg, err)
		span.RecordError(err)
		return
	}
	defer resp.Body.Close()
	msg := fmt.Sprintf("Transcode request response Status: %v", resp.Status)
	logger.Info(msg)
}
