package handler

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/auth"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
	"github.com/johannes-kuhfuss/fileupload-service/service"
	"github.com/johannes-kuhfuss/services_utils/api_error"
	"go.opentelemetry.io/otel/metric"
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

func (uh UploadHandler) CreateUpload(c *gin.Context) {
	identity, ok := auth.IdentityFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "missing identity"})
		return
	}
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

	session, err := uh.Svc.CreateSession(identity, req)
	if err != nil {
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	c.JSON(http.StatusCreated, sessionResponse(session))
}

func (uh UploadHandler) UploadChunk(c *gin.Context) {
	identity, ok := auth.IdentityFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "missing identity"})
		return
	}
	uploadID := c.Param("uploadID")
	offset, err := service.ParseUploadOffset(c.GetHeader("Upload-Offset"))
	if err != nil {
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	session, err := uh.Svc.GetSession(identity, uploadID)
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

	bytesReceived, err := uh.Svc.WriteChunk(identity, uploadID, offset, c.Request.Body)
	if err != nil {
		addMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	c.Header("Upload-Offset", fmt.Sprintf("%d", bytesReceived))
	c.Status(http.StatusNoContent)
}

func (uh UploadHandler) GetUpload(c *gin.Context) {
	identity, ok := auth.IdentityFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "missing identity"})
		return
	}
	session, err := uh.Svc.GetSession(identity, c.Param("uploadID"))
	if err != nil {
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	c.Header("Upload-Offset", fmt.Sprintf("%d", session.BytesReceived))
	c.JSON(http.StatusOK, sessionResponse(session))
}

func (uh UploadHandler) CompleteUpload(c *gin.Context) {
	identity, ok := auth.IdentityFromContext(c)
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"message": "missing identity"})
		return
	}
	session, err := uh.Svc.CompleteSession(c.Request.Context(), identity, c.Param("uploadID"))
	if err != nil {
		addMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	addMetric(c.Request.Context(), uh.Cfg.Metrics.UploadSuccessCounter)
	c.JSON(http.StatusOK, dto.CompleteUploadResponse{
		UploadID:         session.UploadID,
		TenantID:         session.TenantID,
		FileName:         session.FileName,
		FileSize:         session.FileSize,
		BytesReceived:    session.BytesReceived,
		Status:           session.Status,
		QuarantinePath:   session.QuarantinePath,
		Checksum:         session.Checksum,
		ComputedChecksum: session.ComputedChecksum,
	})
}

func (uh UploadHandler) allowedExtension(fileName string) bool {
	return slices.Contains(uh.Cfg.Upload.AllowedExtensions, filepath.Ext(fileName))
}

func sessionResponse(session domain.UploadSession) dto.UploadSessionResponse {
	return dto.UploadSessionResponse{
		UploadID:         session.UploadID,
		TenantID:         session.TenantID,
		FileName:         session.FileName,
		FileSize:         session.FileSize,
		BytesReceived:    session.BytesReceived,
		Status:           session.Status,
		QuarantinePath:   session.QuarantinePath,
		Checksum:         session.Checksum,
		ComputedChecksum: session.ComputedChecksum,
	}
}

func addMetric(ctx context.Context, counter metric.Int64Counter) {
	if counter != nil {
		counter.Add(ctx, 1)
	}
}
