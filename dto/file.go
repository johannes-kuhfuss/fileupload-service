package dto

import (
	"mime/multipart"

	"github.com/google/uuid"
)

type FileDta struct {
	File     multipart.File
	Header   *multipart.FileHeader
	FileSize int64
	FileId   uuid.UUID
}

type FileRet struct {
	FileName     string `json:"file_name"`
	BytesWritten int64  `json:"bytes_written"`
	NewFilePath  string `json:"new_file_path"`
}
