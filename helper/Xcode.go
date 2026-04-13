package helper

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
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

const (
	eMsg = "Error Message"
)

type XcodeRequest struct {
	SourceFilePath string `json:"source_file_path"`
}

func StartXcode(cfg *config.AppConfig, ctx context.Context, filePath string) error {
	var (
		req XcodeRequest
	)
	msg := "Calling transcode service..."
	logger.Info(msg)
	cfg.RunTime.OLog.InfoContext(ctx, msg)
	xcodeUrl := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(cfg.Xcode.Host, cfg.Xcode.Port),
		Path:   "/xcode",
	}
	if cfg.Xcode.Host == "" {
		msg := "No Xcode host configured. Cannot start XCode"
		logger.Error(msg, nil)
		cfg.RunTime.OLog.ErrorContext(ctx, msg)
		return errors.New(msg)
	}
	tracer := otel.Tracer("fileupload-service")
	ctx, span := tracer.Start(ctx, "transcode_request",
		trace.WithAttributes(
			attribute.String("http.url", xcodeUrl.String()),
		),
	)
	defer span.End()

	req.SourceFilePath = filePath
	reqJson, err := json.Marshal(req)
	if err != nil {
		msg := "Could not create transcode request"
		logger.Error(msg, err)
		cfg.RunTime.OLog.ErrorContext(ctx, msg, slog.String(eMsg, err.Error()))
		span.RecordError(err)
		return err
	}
	hc := http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	hreq, err := http.NewRequestWithContext(ctx, "POST", xcodeUrl.String(), bytes.NewBuffer(reqJson))
	if err != nil {
		msg := "Could not create transcode request"
		logger.Error(msg, err)
		cfg.RunTime.OLog.ErrorContext(ctx, msg, slog.String(eMsg, err.Error()))
		span.RecordError(err)
		return err
	}
	hreq.Header.Add("Content-Type", "application/json")
	resp, err := hc.Do(hreq)
	if err != nil {
		msg := "Could not send transcode request"
		logger.Error(msg, err)
		cfg.RunTime.OLog.ErrorContext(ctx, msg, slog.String(eMsg, err.Error()))
		span.RecordError(err)
		return err
	}
	defer resp.Body.Close()
	msg = fmt.Sprintf("Transcode request response Status: %v", resp.Status)
	logger.Info(msg)
	cfg.RunTime.OLog.InfoContext(ctx, msg)
	return nil
}
