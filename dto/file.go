package dto

type CreateUploadRequest struct {
	FileName    string `json:"file_name"`
	FileSize    int64  `json:"file_size"`
	ContentType string `json:"content_type,omitempty"`
	Checksum    string `json:"checksum,omitempty"`
}

type UploadSessionResponse struct {
	UploadID         string `json:"upload_id"`
	TenantID         string `json:"tenant_id"`
	FileName         string `json:"file_name"`
	FileSize         int64  `json:"file_size"`
	BytesReceived    int64  `json:"bytes_received"`
	Status           string `json:"status"`
	QuarantinePath   string `json:"quarantine_path,omitempty"`
	Checksum         string `json:"checksum,omitempty"`
	ComputedChecksum string `json:"computed_checksum,omitempty"`
}

type CompleteUploadResponse struct {
	UploadID         string `json:"upload_id"`
	TenantID         string `json:"tenant_id"`
	FileName         string `json:"file_name"`
	FileSize         int64  `json:"file_size"`
	BytesReceived    int64  `json:"bytes_received"`
	Status           string `json:"status"`
	QuarantinePath   string `json:"quarantine_path"`
	Checksum         string `json:"checksum"`
	ComputedChecksum string `json:"computed_checksum"`
}
