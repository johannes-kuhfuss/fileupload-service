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
		cfg.RunTime.OLog.Error(msg)
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
		cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		return
	}
	resp, err := http.Post(xcodeUrl.String(), "application/json", bytes.NewBuffer(reqJson))
	if err != nil {
		msg := "Could not send transcode request"
		logger.Error(msg, err)
		cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		return
	}
	defer resp.Body.Close()
	msg := fmt.Sprintf("Transcode request response Status: %v", resp.Status)
	logger.Info(msg)
	cfg.RunTime.OLog.Info(msg)
}
