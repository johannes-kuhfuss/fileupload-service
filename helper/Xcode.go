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
)

type XcodeRequest struct {
	SourceFilePath string `json:"source_file_path"`
}

func StartXcode(cfg *config.AppConfig, filePath string) {
	var (
		req XcodeRequest
	)
	if cfg.Xcode.Host == "" {
		cfg.RunTime.OLog.Error("No Xcode host configured. Cannot start XCode")
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
		cfg.RunTime.OLog.Error("Could not create transcode request", slog.String("Error Message", err.Error()))
		return
	}
	resp, err := http.Post(xcodeUrl.String(), "application/json", bytes.NewBuffer(reqJson))
	if err != nil {
		cfg.RunTime.OLog.Error("Could not send transcode request", slog.String("Error Message", err.Error()))
		return
	}
	defer resp.Body.Close()
	cfg.RunTime.OLog.Info(fmt.Sprintf("Transcode request response Status: %v", resp.Status))
}
