package domain

import "time"

const (
	UploadStatusReceiving      = "receiving"
	UploadStatusQuarantined    = "quarantined"
	UploadStatusChecksumFailed = "checksum_failed"
)

type UploadSession struct {
	UploadID         string    `json:"upload_id"`
	TenantID         string    `json:"tenant_id"`
	CreatedBy        string    `json:"created_by"`
	FileName         string    `json:"file_name"`
	FileSize         int64     `json:"file_size"`
	BytesReceived    int64     `json:"bytes_received"`
	ContentType      string    `json:"content_type,omitempty"`
	Checksum         string    `json:"checksum,omitempty"`
	ComputedChecksum string    `json:"computed_checksum,omitempty"`
	Status           string    `json:"status"`
	QuarantinePath   string    `json:"quarantine_path"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}
