package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/johannes-kuhfuss/fileupload-service/config"
)

type UiHandler struct {
	Cfg *config.AppConfig
}

func NewUiHandler(cfg *config.AppConfig) UiHandler {
	return UiHandler{
		Cfg: cfg,
	}
}

func (uh *UiHandler) AboutPage(c *gin.Context) {
	c.HTML(http.StatusOK, "about.page.tmpl", gin.H{
		"title": "About",
		"data":  nil,
	})
}

func (uh *UiHandler) UploadPage(c *gin.Context) {
	c.HTML(http.StatusOK, "upload.page.tmpl", gin.H{
		"title": "Upload",
		"data":  nil,
	})
}

func (uh *UiHandler) UploadListPage(c *gin.Context) {
	files := uh.Cfg.RunTime.UploadList
	c.HTML(http.StatusOK, "uploadlist.page.tmpl", gin.H{
		"title": "Upload List",
		"data":  files,
	})
}
