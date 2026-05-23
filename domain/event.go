package domain

import "time"

type UploadEvent struct {
	EventID        string    `json:"event_id"`
	Type           string    `json:"type"`
	Source         string    `json:"source"`
	OccurredAt     time.Time `json:"occurred_at"`
	UploadID       string    `json:"upload_id"`
	TenantID       string    `json:"tenant_id"`
	ActorID        string    `json:"actor_id"`
	FileName       string    `json:"file_name"`
	FileSize       int64     `json:"file_size"`
	ContentType    string    `json:"content_type,omitempty"`
	Checksum       string    `json:"checksum,omitempty"`
	QuarantinePath string    `json:"quarantine_path"`
}
