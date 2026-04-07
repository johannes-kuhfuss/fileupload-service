package helper

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"math"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
)

type XcodeRequest struct {
	SourceFilePath string `json:"source_file_path"`
}

func AddToUploadList(cfg *config.AppConfig, fd dto.FileDta, status string, newFilePath string) {
	t := time.Now()
	ul := domain.Upload{
		UploadDate:  t.Format("2006-01-02 15:04:05"),
		FileName:    fd.Header.Filename,
		Status:      status,
		NewFilePath: newFilePath,
	}
	if fd.FileSize == 0 {
		ul.Size = ""
	} else {
		sizekb := float64(fd.FileSize) / (1 << 20)
		sizeStr := strconv.FormatInt(int64((math.Round(sizekb))), 10) + "MB"
		ul.Size = sizeStr
	}

	cfg.RunTime.UploadList = append(cfg.RunTime.UploadList, ul)
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
