package service

import (
	"io"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
)

type Uploader interface {
	Upload(dto.FileDta)
}

type DefaultUploadService struct {
	Cfg *config.AppConfig
}

func NewUploadService(cfg *config.AppConfig) DefaultUploadService {
	return DefaultUploadService{
		Cfg: cfg,
	}
}

func (s DefaultUploadService) Upload(fd dto.FileDta) (newFilePath string, written int64, err error) {

	fileName := sanitizeFileName(fd.Header.Filename)
	localFile := buildFileName(s.Cfg.Upload.UploadPath, fd.FileId.String(), fileName)
	dst, err := os.Create(localFile)
	if err != nil {
		return "", 0, err
	}
	defer dst.Close()
	bw, err := io.Copy(dst, fd.File)
	if err != nil {
		return "", 0, err
	}
	return path.Join(fd.FileId.String(), fileName), bw, nil
}

func buildFileName(uploadPath, fileId, fileName string) string {
	os.MkdirAll(path.Join(uploadPath, fileId), os.ModePerm)
	return path.Join(uploadPath, fileId, fileName)
}

func sanitizeFileName(fileName string) string {
	var (
		newName      string
		invalidChars = regexp.MustCompile(`[<>:"/\\|?*;\[\]\x00-\x1F]`)
		spaces       = regexp.MustCompile(`\s+`)
	)
	newName = strings.TrimSpace(fileName)
	newName = invalidChars.ReplaceAllString(newName, "_")
	newName = strings.TrimRight(newName, ".")
	newName = spaces.ReplaceAllString(newName, "_")

	return newName
}
