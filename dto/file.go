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
	UploadID     string `json:"upload_id,omitempty"`
	Status       string `json:"status,omitempty"`
}

type CreateUploadRequest struct {
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
}

type UploadSessionResponse struct {
	UploadID       string `json:"upload_id"`
	FileName       string `json:"file_name"`
	FileSize       int64  `json:"file_size"`
	BytesReceived  int64  `json:"bytes_received"`
	Status         string `json:"status"`
	QuarantinePath string `json:"quarantine_path,omitempty"`
}

type CompleteUploadResponse struct {
	UploadID       string `json:"upload_id"`
	FileName       string `json:"file_name"`
	FileSize       int64  `json:"file_size"`
	BytesReceived  int64  `json:"bytes_received"`
	Status         string `json:"status"`
	QuarantinePath string `json:"quarantine_path"`
}
