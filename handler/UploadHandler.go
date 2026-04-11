package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
	"github.com/johannes-kuhfuss/fileupload-service/helper"
	"github.com/johannes-kuhfuss/fileupload-service/service"
	"github.com/johannes-kuhfuss/services_utils/api_error"
	"github.com/johannes-kuhfuss/services_utils/logger"
	"github.com/johannes-kuhfuss/services_utils/misc"
)

type UploadHandler struct {
	Svc service.DefaultUploadService
	Cfg *config.AppConfig
}

func NewUploadHandler(cfg *config.AppConfig, svc service.DefaultUploadService) UploadHandler {
	return UploadHandler{
		Cfg: cfg,
		Svc: svc,
	}
}

func (uh UploadHandler) Receive(c *gin.Context) {
	var (
		fd dto.FileDta
	)

	fd.FileId = uuid.New()

	msg := fmt.Sprintf("Upload request %v for file %v received .", fd.FileId.String(), fd.Header.Filename)
	logger.Info(msg)
	uh.Cfg.RunTime.OLog.Info(msg)

	err := c.Request.ParseMultipartForm(32 << 20)
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := fmt.Sprintf("error getting form for request %v, file %v", fd.FileId.String(), fd.Header.Filename)
		logger.Error(msg, err)
		uh.Cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	fd.File, fd.Header, err = c.Request.FormFile("file")
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := fmt.Sprintf("cannot read remote file fo%v for request %v", fd.Header.Filename, fd.FileId.String())
		logger.Error(msg, err)
		uh.Cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	defer fd.File.Close()

	if !misc.SliceContainsString(uh.Cfg.Upload.AllowedExtensions, filepath.Ext(fd.Header.Filename)) {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := fmt.Sprintf("Cannot upload file %v with extension %v", fd.Header.Filename, filepath.Ext(fd.Header.Filename))
		helper.AddToUploadList(uh.Cfg, fd, msg, "")
		logger.Warn(msg)
		uh.Cfg.RunTime.OLog.Warn(msg)
		apiErr := api_error.NewBadRequestError(msg)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	newFilePath, written, err := uh.Svc.Upload(fd)
	fd.FileSize = written
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := fmt.Sprintf("Could not complete the upload request %v for file %v", fd.FileId.String(), fd.Header.Filename)
		helper.AddToUploadList(uh.Cfg, fd, msg, "")
		logger.Error(msg, err)
		uh.Cfg.RunTime.OLog.Error(msg, slog.String("Error Message", err.Error()))
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	helper.AddToUploadList(uh.Cfg, fd, "Successfully completed", newFilePath)
	msg = fmt.Sprintf("Upload request %v for file %v sucessfully completed.", fd.FileId.String(), fd.Header.Filename)
	logger.Info(msg)
	uh.Cfg.RunTime.OLog.Info(msg)
	uh.Cfg.Metrics.UploadSuccessCounter.Add(c.Copy().Request.Context(), 1)
	helper.StartXcode(uh.Cfg, newFilePath)

	ret := dto.FileRet{
		FileName:     fd.Header.Filename,
		BytesWritten: fd.FileSize,
		NewFilePath:  newFilePath,
	}
	c.JSON(http.StatusCreated, ret)
}
