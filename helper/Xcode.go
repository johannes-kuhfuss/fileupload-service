package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
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
	if cfg.Xcode.Host == "" {
		msg := "No Xcode host configured. Cannot start XCode"
		logger.Error(msg, nil)
		return
	}
	xcodeUrl := url.URL{
		Scheme: "http",
		Host:   net.JoinHostPort(cfg.Xcode.Host, cfg.Xcode.Port),
		Path:   "/xcode",
	}
	req.SourceFilePath = filePath
	reqJson, err := json.Marshal(req)
	if err != nil {
		msg := "Could not create transcode request"
		logger.Error(msg, err)
		return
	}
	hc := http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	resp, err := hc.Post(xcodeUrl.String(), "application/json", bytes.NewBuffer(reqJson))
	if err != nil {
		msg := "Could not send transcode request"
		logger.Error(msg, err)
		return
	}
	defer resp.Body.Close()
	msg := fmt.Sprintf("Transcode request response Status: %v", resp.Status)
	logger.Info(msg)
}
