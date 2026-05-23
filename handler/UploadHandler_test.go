package handler

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
	"github.com/johannes-kuhfuss/fileupload-service/service"
)

func TestCreateUploadEndpoint(t *testing.T) {
	router, cfg := testRouter(t)

	w := performRequest(router, http.MethodPost, "/uploads", "application/json", strings.NewReader(`{"file_name":"track 01.wav","file_size":11,"content_type":"audio/wav","checksum":"`+checksum("hello world")+`"}`), nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp dto.UploadSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if resp.UploadID == "" || resp.FileName != "track01.wav" || resp.FileSize != 11 || resp.Status != domain.UploadStatusReceiving {
		t.Fatalf("unexpected response: %+v", resp)
	}
	if resp.Checksum != checksum("hello world") {
		t.Fatalf("checksum = %q, want %q", resp.Checksum, checksum("hello world"))
	}
	if _, err := os.Stat(filepath.Join(cfg.Upload.QuarantinePath, "_sessions", resp.UploadID, "metadata.json")); err != nil {
		t.Fatalf("metadata was not created: %v", err)
	}
}

func TestCreateUploadEndpointRejectsInvalidRequests(t *testing.T) {
	tests := []struct {
		name string
		body string
	}{
		{name: "invalid json", body: `not-json`},
		{name: "disallowed extension", body: `{"file_name":"cover.jpg","file_size":1,"checksum":"` + checksum("x") + `"}`},
		{name: "negative size", body: `{"file_name":"track.wav","file_size":-1,"checksum":"` + checksum("x") + `"}`},
		{name: "missing checksum", body: `{"file_name":"track.wav","file_size":1}`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := testRouter(t)
			w := performRequest(router, http.MethodPost, "/uploads", "application/json", strings.NewReader(tt.body), nil)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestResumableUploadEndpoints(t *testing.T) {
	router, cfg := testRouter(t)
	session := createUploadViaHTTP(t, router, "track.wav", "hello world")

	w := performRequest(router, http.MethodPatch, "/uploads/"+session.UploadID, "application/octet-stream", strings.NewReader("hello "), map[string]string{"Upload-Offset": "0"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("first patch status = %d, body = %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Upload-Offset") != "6" {
		t.Fatalf("first Upload-Offset = %q, want 6", w.Header().Get("Upload-Offset"))
	}

	w = performRequest(router, http.MethodGet, "/uploads/"+session.UploadID, "", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("get status = %d, body = %s", w.Code, w.Body.String())
	}
	if w.Header().Get("Upload-Offset") != "6" {
		t.Fatalf("get Upload-Offset = %q, want 6", w.Header().Get("Upload-Offset"))
	}

	w = performRequest(router, http.MethodPatch, "/uploads/"+session.UploadID, "application/octet-stream", strings.NewReader("world"), map[string]string{"Upload-Offset": "6"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("second patch status = %d, body = %s", w.Code, w.Body.String())
	}

	w = performRequest(router, http.MethodPost, "/uploads/"+session.UploadID+"/complete", "", nil, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("complete status = %d, body = %s", w.Code, w.Body.String())
	}
	var completed dto.CompleteUploadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &completed); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if completed.Status != domain.UploadStatusQuarantined || completed.BytesReceived != 11 {
		t.Fatalf("unexpected completion response: %+v", completed)
	}
	if completed.Checksum != checksum("hello world") || completed.ComputedChecksum != checksum("hello world") {
		t.Fatalf("checksums = client %q server %q", completed.Checksum, completed.ComputedChecksum)
	}

	contents, err := os.ReadFile(filepath.Join(cfg.Upload.QuarantinePath, completed.QuarantinePath))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "hello world" {
		t.Fatalf("contents = %q, want hello world", string(contents))
	}
}

func TestUploadChunkEndpointRejectsBadOffsetsAndOversizedChunks(t *testing.T) {
	tests := []struct {
		name    string
		body    string
		headers map[string]string
	}{
		{name: "missing offset", body: "abc", headers: nil},
		{name: "unexpected offset", body: "abc", headers: map[string]string{"Upload-Offset": "1"}},
		{name: "oversized chunk", body: "abcdef", headers: map[string]string{"Upload-Offset": "0"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			router, _ := testRouter(t)
			session := createUploadViaHTTP(t, router, "track.wav", "hello")
			w := performRequest(router, http.MethodPatch, "/uploads/"+session.UploadID, "application/octet-stream", strings.NewReader(tt.body), tt.headers)
			if w.Code != http.StatusBadRequest {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestCompleteUploadEndpointRejectsIncompleteUpload(t *testing.T) {
	router, _ := testRouter(t)
	session := createUploadViaHTTP(t, router, "track.wav", "hello")

	w := performRequest(router, http.MethodPatch, "/uploads/"+session.UploadID, "application/octet-stream", strings.NewReader("he"), map[string]string{"Upload-Offset": "0"})
	if w.Code != http.StatusNoContent {
		t.Fatalf("patch status = %d, body = %s", w.Code, w.Body.String())
	}

	w = performRequest(router, http.MethodPost, "/uploads/"+session.UploadID+"/complete", "", nil, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("complete status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestGetUploadEndpointRejectsUnknownUpload(t *testing.T) {
	router, _ := testRouter(t)

	w := performRequest(router, http.MethodGet, "/uploads/00000000-0000-0000-0000-000000000001", "", nil, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestUploadChunkEndpointRejectsUnknownUpload(t *testing.T) {
	router, _ := testRouter(t)

	w := performRequest(router, http.MethodPatch, "/uploads/00000000-0000-0000-0000-000000000001", "application/octet-stream", strings.NewReader("hello"), map[string]string{"Upload-Offset": "0"})
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func TestCompleteUploadEndpointRejectsUnknownUpload(t *testing.T) {
	router, _ := testRouter(t)

	w := performRequest(router, http.MethodPost, "/uploads/00000000-0000-0000-0000-000000000001/complete", "", nil, nil)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
}

func testRouter(t *testing.T) (*gin.Engine, config.AppConfig) {
	t.Helper()
	gin.SetMode(gin.TestMode)

	root := t.TempDir()
	var cfg config.AppConfig
	cfg.Upload.UploadPath = root
	cfg.Upload.QuarantinePath = filepath.Join(root, "quarantine")
	cfg.Upload.AllowedExtensions = []string{".wav", ".mp3", ".m4a"}
	cfg.Events.Source = "test"
	cfg.Events.OutboxPath = filepath.Join(root, "events", "upload-events.ndjson")

	svc := service.NewUploadService(&cfg)
	handler := NewUploadHandler(&cfg, svc)

	router := gin.New()
	router.POST("/uploads", handler.CreateUpload)
	router.GET("/uploads/:uploadID", handler.GetUpload)
	router.PATCH("/uploads/:uploadID", handler.UploadChunk)
	router.POST("/uploads/:uploadID/complete", handler.CompleteUpload)
	return router, cfg
}

func createUploadViaHTTP(t *testing.T, router *gin.Engine, fileName string, contents string) dto.UploadSessionResponse {
	t.Helper()
	body := strings.NewReader(`{"file_name":"` + fileName + `","file_size":` + strconv.FormatInt(int64(len(contents)), 10) + `,"checksum":"` + checksum(contents) + `"}`)
	w := performRequest(router, http.MethodPost, "/uploads", "application/json", body, nil)
	if w.Code != http.StatusCreated {
		t.Fatalf("create status = %d, body = %s", w.Code, w.Body.String())
	}
	var session dto.UploadSessionResponse
	if err := json.Unmarshal(w.Body.Bytes(), &session); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	return session
}

func performRequest(router *gin.Engine, method, target, contentType string, body io.Reader, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, body)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	for key, value := range headers {
		req.Header.Set(key, value)
	}
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func checksum(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}
