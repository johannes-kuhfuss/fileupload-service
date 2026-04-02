package service

import (
	"io"
	"os"
	"path"

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
	localFile := buildFileName(s.Cfg.Upload.Path, fd.FileId.String(), fd.Header.Filename)
	dst, err := os.Create(localFile)
	if err != nil {
		return "", 0, err
	}
	defer dst.Close()
	bw, err := io.Copy(dst, fd.File)
	if err != nil {
		return "", 0, err
	}
	return path.Join(fd.FileId.String(), fd.Header.Filename), bw, nil
}

func buildFileName(uploadPath, fileId, fileName string) string {
	os.MkdirAll(path.Join(uploadPath, fileId), os.ModePerm)
	return path.Join(uploadPath, fileId, fileName)
}
