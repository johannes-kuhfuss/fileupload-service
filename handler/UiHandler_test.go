package handler

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
)

func TestUiHandlerPagesRender(t *testing.T) {
	gin.SetMode(gin.TestMode)

	var cfg config.AppConfig
	cfg.Auth.Issuer = "http://localhost:8081/realms/mam-dev"
	cfg.Auth.Audience = "fileupload-service"
	cfg.RunTime.UploadList = []domain.Upload{
		{
			UploadDate:  "2026-05-23 09:00:00",
			FileName:    "track.wav",
			Status:      "quarantined",
			Size:        "1MB",
			NewFilePath: "upload-id/track.wav",
		},
	}
	ui := NewUiHandler(&cfg)

	router := gin.New()
	router.LoadHTMLGlob(filepath.Join("..", "templates", "*.tmpl"))
	router.GET("/", ui.UploadPage)
	router.GET("/files", ui.UploadListPage)
	router.GET("/about", ui.AboutPage)

	tests := []struct {
		name        string
		path        string
		contains    []string
		notContains []string
	}{
		{
			name:        "upload",
			path:        "/",
			contains:    []string{"Media Upload", "startLogin", "code_challenge_method", "calculateChecksum", "/uploads", "http://localhost:8081/realms/mam-dev", "fileupload-service"},
			notContains: []string{"access-token", "Paste a Keycloak access token"},
		},
		{name: "files", path: "/files", contains: []string{"Files uploaded", "track.wav", "quarantined"}},
		{name: "about", path: "/about", contains: []string{"About"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, tt.path, nil)
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
			}
			for _, expected := range tt.contains {
				if !strings.Contains(w.Body.String(), expected) {
					t.Fatalf("body does not contain %q: %s", expected, w.Body.String())
				}
			}
			for _, unexpected := range tt.notContains {
				if strings.Contains(w.Body.String(), unexpected) {
					t.Fatalf("body contains %q: %s", unexpected, w.Body.String())
				}
			}
		})
	}
}
