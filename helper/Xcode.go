package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"

	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/services_utils/logger"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
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
	if cfg.Xcode.Host == "" {
		msg := "No Xcode host configured. Cannot start XCode"
		logger.Error(msg, nil)
		cfg.RunTime.OLog.Error(msg)
		return
	}

	req.SourceFilePath = filePath
	reqJson, err := json.Marshal(req)
	if err != nil {
		msg := "Could not create transcode request"
		logger.Error(msg, err)
		cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		return
	}
	hc := http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	hreq, err := http.NewRequest("POST", xcodeUrl.String(), bytes.NewBuffer(reqJson))
	if err != nil {
		msg := "Could not create transcode request"
		logger.Error(msg, err)
		cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		return
	}
	hreq.Header.Add("Content-Type", "application/json")
	resp, err := hc.Do(hreq)
	if err != nil {
		msg := "Could not send transcode request"
		logger.Error(msg, err)
		cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		return
	}
	defer resp.Body.Close()
	msg := fmt.Sprintf("Transcode request response Status: %v", resp.Status)
	logger.Info(msg)
}
