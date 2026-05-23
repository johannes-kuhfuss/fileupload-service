package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"path/filepath"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
	"github.com/johannes-kuhfuss/fileupload-service/helper"
	"github.com/johannes-kuhfuss/fileupload-service/service"
	"github.com/johannes-kuhfuss/services_utils/api_error"
	"github.com/johannes-kuhfuss/services_utils/logger"
	"github.com/johannes-kuhfuss/services_utils/misc"
)

const (
	eMsg = "Error Message"
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

	msg := fmt.Sprintf("Upload request %v received.", fd.FileId.String())
	logger.Info(msg)
	uh.Cfg.RunTime.OLog.InfoContext(c.Request.Context(), msg)

	err := c.Request.ParseMultipartForm(32 << 20)
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Request.Context(), 1)
		msg := fmt.Sprintf("error getting form for request %v", fd.FileId.String())
		logger.Error(msg, err)
		uh.Cfg.RunTime.OLog.ErrorContext(c.Request.Context(), msg, slog.String(eMsg, err.Error()))
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	fd.File, fd.Header, err = c.Request.FormFile("file")
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Request.Context(), 1)
		msg := fmt.Sprintf("cannot read remote file for request %v", fd.FileId.String())
		logger.Error(msg, err)
		uh.Cfg.RunTime.OLog.ErrorContext(c.Request.Context(), msg, slog.String(eMsg, err.Error()))
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	defer fd.File.Close()

	if !misc.SliceContainsString(uh.Cfg.Upload.AllowedExtensions, filepath.Ext(fd.Header.Filename)) {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Request.Context(), 1)
		msg := fmt.Sprintf("Cannot upload file %v with extension %v", fd.Header.Filename, filepath.Ext(fd.Header.Filename))
		helper.AddToUploadList(uh.Cfg, fd, msg, "")
		logger.Warn(msg)
		uh.Cfg.RunTime.OLog.WarnContext(c.Request.Context(), msg)
		apiErr := api_error.NewBadRequestError(msg)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	newFilePath, uploadID, written, err := uh.Svc.Upload(c.Request.Context(), fd)
	fd.FileSize = written
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Request.Context(), 1)
		msg := fmt.Sprintf("Could not complete the upload request %v for file %v", fd.FileId.String(), fd.Header.Filename)
		helper.AddToUploadList(uh.Cfg, fd, msg, "")
		logger.Error(msg, err)
		uh.Cfg.RunTime.OLog.ErrorContext(c.Request.Context(), msg, slog.String(eMsg, err.Error()))
		apiErr := api_error.NewInternalServerError(msg, err)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	helper.AddToUploadList(uh.Cfg, fd, "Successfully completed", newFilePath)
	msg = fmt.Sprintf("Upload request %v for file %v sucessfully completed.", fd.FileId.String(), fd.Header.Filename)
	logger.Info(msg)
	uh.Cfg.RunTime.OLog.InfoContext(c.Request.Context(), msg)
	uh.Cfg.Metrics.UploadSuccessCounter.Add(c.Copy().Request.Context(), 1)

	ret := dto.FileRet{
		FileName:     fd.Header.Filename,
		BytesWritten: fd.FileSize,
		NewFilePath:  newFilePath,
		UploadID:     uploadID,
		Status:       domain.UploadStatusQuarantined,
	}
	c.JSON(http.StatusCreated, ret)
}

func (uh UploadHandler) CreateUpload(c *gin.Context) {
	var req dto.CreateUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		apiErr := api_error.NewBadRequestError("invalid upload session request")
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	if !uh.allowedExtension(req.FileName) {
		msg := fmt.Sprintf("Cannot upload file %v with extension %v", req.FileName, filepath.Ext(req.FileName))
		apiErr := api_error.NewBadRequestError(msg)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	session, err := uh.Svc.CreateSession(req)
	if err != nil {
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	c.JSON(http.StatusCreated, sessionResponse(session))
}

func (uh UploadHandler) UploadChunk(c *gin.Context) {
	uploadID := c.Param("uploadID")
	offset, err := service.ParseUploadOffset(c.GetHeader("Upload-Offset"))
	if err != nil {
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	session, err := uh.Svc.GetSession(uploadID)
	if err != nil {
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	if c.Request.ContentLength > session.FileSize-session.BytesReceived {
		apiErr := api_error.NewBadRequestError("chunk exceeds remaining upload size")
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	bytesReceived, err := uh.Svc.WriteChunk(uploadID, offset, c.Request.Body)
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Request.Context(), 1)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	c.Header("Upload-Offset", fmt.Sprintf("%d", bytesReceived))
	c.Status(http.StatusNoContent)
}

func (uh UploadHandler) GetUpload(c *gin.Context) {
	session, err := uh.Svc.GetSession(c.Param("uploadID"))
	if err != nil {
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	c.Header("Upload-Offset", fmt.Sprintf("%d", session.BytesReceived))
	c.JSON(http.StatusOK, sessionResponse(session))
}

func (uh UploadHandler) CompleteUpload(c *gin.Context) {
	session, err := uh.Svc.CompleteSession(c.Request.Context(), c.Param("uploadID"))
	if err != nil {
		uh.Cfg.Metrics.UploadFailureCounter.Add(c.Request.Context(), 1)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	uh.Cfg.Metrics.UploadSuccessCounter.Add(c.Request.Context(), 1)
	c.JSON(http.StatusOK, dto.CompleteUploadResponse{
		UploadID:       session.UploadID,
		FileName:       session.FileName,
		FileSize:       session.FileSize,
		BytesReceived:  session.BytesReceived,
		Status:         session.Status,
		QuarantinePath: session.QuarantinePath,
	})
}

func (uh UploadHandler) allowedExtension(fileName string) bool {
	return misc.SliceContainsString(uh.Cfg.Upload.AllowedExtensions, filepath.Ext(fileName))
}

func sessionResponse(session domain.UploadSession) dto.UploadSessionResponse {
	return dto.UploadSessionResponse{
		UploadID:       session.UploadID,
		FileName:       session.FileName,
		FileSize:       session.FileSize,
		BytesReceived:  session.BytesReceived,
		Status:         session.Status,
		QuarantinePath: session.QuarantinePath,
	}
}
