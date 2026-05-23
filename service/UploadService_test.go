package service

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
)

func TestResumableUploadCompletesIntoQuarantineAndPublishesEvent(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName:    "track 01.wav",
		FileSize:    11,
		ContentType: "audio/wav",
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	offset, err := svc.WriteChunk(session.UploadID, 0, strings.NewReader("hello "))
	if err != nil {
		t.Fatalf("WriteChunk(first) error = %v", err)
	}
	if offset != 6 {
		t.Fatalf("first offset = %d, want 6", offset)
	}

	resumed, err := svc.GetSession(session.UploadID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if resumed.BytesReceived != 6 || resumed.Status != domain.UploadStatusReceiving {
		t.Fatalf("resumed session = %+v", resumed)
	}

	offset, err = svc.WriteChunk(session.UploadID, 6, strings.NewReader("world"))
	if err != nil {
		t.Fatalf("WriteChunk(second) error = %v", err)
	}
	if offset != 11 {
		t.Fatalf("second offset = %d, want 11", offset)
	}

	completed, err := svc.CompleteSession(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("CompleteSession() error = %v", err)
	}
	if completed.Status != domain.UploadStatusQuarantined {
		t.Fatalf("status = %s, want %s", completed.Status, domain.UploadStatusQuarantined)
	}

	contents, err := os.ReadFile(filepath.Join(cfg.Upload.QuarantinePath, completed.QuarantinePath))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "hello world" {
		t.Fatalf("uploaded contents = %q", string(contents))
	}

	events, err := os.ReadFile(cfg.Events.OutboxPath)
	if err != nil {
		t.Fatalf("ReadFile(outbox) error = %v", err)
	}
	if !strings.Contains(string(events), "media.asset.uploaded.quarantined") {
		t.Fatalf("outbox event missing upload type: %s", string(events))
	}
}

func TestWriteChunkRejectsUnexpectedOffset(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "track.wav",
		FileSize: 5,
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := svc.WriteChunk(session.UploadID, 1, strings.NewReader("hello")); err == nil {
		t.Fatal("WriteChunk() expected offset error")
	}
}

func testConfig(root string) config.AppConfig {
	var cfg config.AppConfig
	cfg.Upload.UploadPath = root
	cfg.Upload.QuarantinePath = filepath.Join(root, "quarantine")
	cfg.Events.Source = "test"
	cfg.Events.OutboxPath = filepath.Join(root, "events", "upload-events.ndjson")
	return cfg
}
