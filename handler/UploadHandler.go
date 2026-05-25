package handler

import (
	"context"
	"fmt"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
	"github.com/johannes-kuhfuss/fileupload-service/service"
	"github.com/johannes-kuhfuss/services_utils/api_error"
	"go.opentelemetry.io/otel/attribute"
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
	start := time.Now()
	var req dto.CreateUploadRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "create_session", "invalid_request")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "create_session", start)
		apiErr := api_error.NewBadRequestError("invalid upload session request")
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	if !uh.allowedExtension(req.FileName) {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "create_session", "disallowed_extension")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "create_session", start)
		msg := fmt.Sprintf("Cannot upload file %v with extension %v", req.FileName, filepath.Ext(req.FileName))
		apiErr := api_error.NewBadRequestError(msg)
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	session, err := uh.Svc.CreateSession(req)
	if err != nil {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "create_session", "service_error")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "create_session", start)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	attrs := uploadAttributes(session)
	if uh.Cfg.Metrics.SessionsStartedCounter != nil {
		uh.Cfg.Metrics.SessionsStartedCounter.Add(c.Request.Context(), 1, metric.WithAttributes(attrs...))
	}
	if uh.Cfg.Metrics.UploadSizeHistogram != nil {
		uh.Cfg.Metrics.UploadSizeHistogram.Record(c.Request.Context(), session.FileSize, metric.WithAttributes(attrs...))
	}
	recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "create_session", start)
	c.JSON(http.StatusCreated, sessionResponse(session))
}

func (uh UploadHandler) UploadChunk(c *gin.Context) {
	start := time.Now()
	uploadID := c.Param("uploadID")
	offset, err := service.ParseUploadOffset(c.GetHeader("Upload-Offset"))
	if err != nil {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "write_chunk", "invalid_offset")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "write_chunk", start)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	session, err := uh.Svc.GetSession(uploadID)
	if err != nil {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "write_chunk", "session_not_found")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "write_chunk", start)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	if c.Request.ContentLength > session.FileSize-session.BytesReceived {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "write_chunk", "chunk_too_large")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "write_chunk", start)
		apiErr := api_error.NewBadRequestError("chunk exceeds remaining upload size")
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}

	bytesReceived, err := uh.Svc.WriteChunk(uploadID, offset, c.Request.Body)
	if err != nil {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "write_chunk", "service_error")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "write_chunk", start)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	written := bytesReceived - session.BytesReceived
	if uh.Cfg.Metrics.ChunksAcceptedCounter != nil {
		uh.Cfg.Metrics.ChunksAcceptedCounter.Add(c.Request.Context(), 1)
	}
	if uh.Cfg.Metrics.BytesReceivedCounter != nil && written > 0 {
		uh.Cfg.Metrics.BytesReceivedCounter.Add(c.Request.Context(), written)
	}
	if uh.Cfg.Metrics.ChunkSizeHistogram != nil {
		uh.Cfg.Metrics.ChunkSizeHistogram.Record(c.Request.Context(), written)
	}
	recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "write_chunk", start)
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
	start := time.Now()
	session, err := uh.Svc.CompleteSession(c.Request.Context(), c.Param("uploadID"))
	if err != nil {
		addFailureMetric(c.Request.Context(), uh.Cfg.Metrics.UploadFailureCounter, "complete_upload", "service_error")
		recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "complete_upload", start)
		apiErr := api_error.NewBadRequestError(err.Error())
		c.JSON(apiErr.StatusCode(), apiErr)
		return
	}
	attrs := uploadAttributes(session)
	if uh.Cfg.Metrics.UploadSuccessCounter != nil {
		uh.Cfg.Metrics.UploadSuccessCounter.Add(c.Request.Context(), 1, metric.WithAttributes(attrs...))
	}
	if uh.Cfg.Metrics.UploadDurationHistogram != nil {
		durationAttrs := append(attrs, attribute.String("result", "completed"))
		uh.Cfg.Metrics.UploadDurationHistogram.Record(c.Request.Context(), time.Since(session.CreatedAt).Seconds(), metric.WithAttributes(durationAttrs...))
	}
	recordStageDuration(c.Request.Context(), uh.Cfg.Metrics.StageDurationHistogram, "complete_upload", start)
	c.JSON(http.StatusOK, dto.CompleteUploadResponse{
		UploadID:         session.UploadID,
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
		FileName:         session.FileName,
		FileSize:         session.FileSize,
		BytesReceived:    session.BytesReceived,
		Status:           session.Status,
		QuarantinePath:   session.QuarantinePath,
		Checksum:         session.Checksum,
		ComputedChecksum: session.ComputedChecksum,
	}
}

func addFailureMetric(ctx context.Context, counter metric.Int64Counter, stage string, reason string) {
	if counter != nil {
		counter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("stage", stage),
			attribute.String("failure.reason", reason),
		))
	}
}

func recordStageDuration(ctx context.Context, histogram metric.Float64Histogram, stage string, start time.Time) {
	if histogram != nil {
		histogram.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String("stage", stage)))
	}
}

func uploadAttributes(session domain.UploadSession) []attribute.KeyValue {
	contentType := session.ContentType
	if contentType == "" {
		contentType = "unknown"
	} else {
		contentType = strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	}
	extension := strings.ToLower(filepath.Ext(session.FileName))
	if extension == "" {
		extension = "unknown"
	}
	return []attribute.KeyValue{
		attribute.String("content_type", contentType),
		attribute.String("extension", extension),
	}
}
