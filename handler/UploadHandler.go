package handler

import (
	"fmt"
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

	logger.Info(fmt.Sprintf("Upload request %v received.", fd.FileId.String()))

	err := c.Request.ParseMultipartForm(32 << 20)
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := "error getting form"
		logger.Error(msg, err)
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	fd.File, fd.Header, err = c.Request.FormFile("file")
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := "cannot read remote file"
		logger.Error(msg, err)
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	defer fd.File.Close()

	if !misc.SliceContainsString(uh.Cfg.Upload.AllowedExtensions, filepath.Ext(fd.Header.Filename)) {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := fmt.Sprintf("Cannot upload file with extension %v", filepath.Ext(fd.Header.Filename))
		helper.AddToUploadList(uh.Cfg, fd, msg, "")
		logger.Warn(msg)
		apiErr := api_error.NewBadRequestError(msg)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	logger.Info(fmt.Sprintf("Upload request %v, File: %v", fd.FileId.String(), fd.Header.Filename))

	logger.Info(fmt.Sprintf("request %v metadata.", fd.FileId.String()))

	newFilePath, written, err := uh.Svc.Upload(fd)
	fd.FileSize = written
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Copy().Request.Context(), 1)
		msg := "Could not complete the upload"
		helper.AddToUploadList(uh.Cfg, fd, msg, "")
		logger.Error(msg, err)
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	helper.AddToUploadList(uh.Cfg, fd, "Successfully completed", newFilePath)
	logger.Info(fmt.Sprintf("Upload request %v (file: %v) sucessfully completed.", fd.FileId.String(), fd.Header.Filename))
	uh.Cfg.Metrics.UploadSuccessCounter.Add(c.Copy().Request.Context(), 1)
	helper.StartXcode(uh.Cfg, newFilePath)

	ret := dto.FileRet{
		FileName:     fd.Header.Filename,
		BytesWritten: fd.FileSize,
		NewFilePath:  newFilePath,
	}
	c.JSON(http.StatusCreated, ret)
}
