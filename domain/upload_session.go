package domain

import "time"

const (
	UploadStatusReceiving   = "receiving"
	UploadStatusQuarantined = "quarantined"
)

type UploadSession struct {
	UploadID       string    `json:"upload_id"`
	FileName       string    `json:"file_name"`
	FileSize       int64     `json:"file_size"`
	BytesReceived  int64     `json:"bytes_received"`
	ContentType    string    `json:"content_type,omitempty"`
	Checksum       string    `json:"checksum,omitempty"`
	Status         string    `json:"status"`
	QuarantinePath string    `json:"quarantine_path"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}
