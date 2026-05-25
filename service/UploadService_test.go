package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
)

type recordingPublisher struct {
	events []domain.UploadEvent
	err    error
}

func (p *recordingPublisher) PublishUploadQuarantined(_ context.Context, event domain.UploadEvent) error {
	if p.err != nil {
		return p.err
	}
	p.events = append(p.events, event)
	return nil
}

func TestResumableUploadCompletesIntoQuarantineAndPublishesEvent(t *testing.T) {
	tmp := t.TempDir()
	cfg := testConfig(tmp)
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName:    "track 01.wav",
		FileSize:    11,
		ContentType: "audio/wav",
		Checksum:    checksum("hello world"),
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
	if completed.Checksum != checksum("hello world") || completed.ComputedChecksum != checksum("hello world") {
		t.Fatalf("checksums = client %q server %q", completed.Checksum, completed.ComputedChecksum)
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

func TestCreateSessionValidatesInput(t *testing.T) {
	tests := []struct {
		name    string
		req     dto.CreateUploadRequest
		maxSize int64
		wantErr string
	}{
		{
			name:    "empty sanitized name",
			req:     dto.CreateUploadRequest{FileName: `<>:"/\|?*;[]().`, FileSize: 1, Checksum: checksum("x")},
			wantErr: "file_name is required",
		},
		{
			name:    "negative size",
			req:     dto.CreateUploadRequest{FileName: "track.wav", FileSize: -1, Checksum: checksum("x")},
			wantErr: "file_size must be greater than or equal to zero",
		},
		{
			name:    "exceeds max size",
			req:     dto.CreateUploadRequest{FileName: "track.wav", FileSize: 11, Checksum: checksum("x")},
			maxSize: 10,
			wantErr: "file_size exceeds max upload size",
		},
		{
			name:    "missing checksum",
			req:     dto.CreateUploadRequest{FileName: "track.wav", FileSize: 1},
			wantErr: "checksum must be a SHA-256 hex digest",
		},
		{
			name:    "invalid checksum",
			req:     dto.CreateUploadRequest{FileName: "track.wav", FileSize: 1, Checksum: "not-a-checksum"},
			wantErr: "checksum must be a SHA-256 hex digest",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := testConfig(t.TempDir())
			cfg.Upload.MaxUploadBytes = tt.maxSize
			svc := NewUploadService(&cfg)

			if _, err := svc.CreateSession(tt.req); err == nil || !strings.Contains(err.Error(), tt.wantErr) {
				t.Fatalf("CreateSession() error = %v, want containing %q", err, tt.wantErr)
			}
		})
	}
}

func TestCreateSessionSanitizesFileNameAndPersistsMetadata(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName:    " bad <track> 01 ;(draft).wav. ",
		FileSize:    42,
		ContentType: "audio/wav",
		Checksum:    "sha256:" + checksum("x"),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.FileName != "badtrack01draft.wav" {
		t.Fatalf("FileName = %q, want sanitized name", session.FileName)
	}
	if session.ContentType != "audio/wav" || session.Checksum != checksum("x") {
		t.Fatalf("metadata not preserved: %+v", session)
	}
	if _, err := os.Stat(sessionPath(&cfg, session.UploadID)); err != nil {
		t.Fatalf("metadata file missing: %v", err)
	}
	if _, err := os.Stat(filepath.Dir(filepath.Join(cfg.Upload.QuarantinePath, session.QuarantinePath))); err != nil {
		t.Fatalf("quarantine upload dir missing: %v", err)
	}
}

func TestCreateSessionNormalizesSHA256Checksum(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "track.wav",
		FileSize: 1,
		Checksum: "SHA256:" + strings.ToUpper(checksum("x")),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if session.Checksum != checksum("x") {
		t.Fatalf("Checksum = %q, want %q", session.Checksum, checksum("x"))
	}
}

func TestWriteChunkRejectsUnexpectedOffset(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "track.wav",
		FileSize: 5,
		Checksum: checksum("hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	if _, err := svc.WriteChunk(session.UploadID, 1, strings.NewReader("hello")); err == nil {
		t.Fatal("WriteChunk() expected offset error")
	}
}

func TestWriteChunkRejectsOversizedBodyWithoutAdvancingOffset(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "track.wav",
		FileSize: 5,
		Checksum: checksum("hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	offset, err := svc.WriteChunk(session.UploadID, 0, strings.NewReader("hello!"))
	if err == nil || !strings.Contains(err.Error(), "chunk exceeds remaining upload size") {
		t.Fatalf("WriteChunk() error = %v, want oversized chunk error", err)
	}
	if offset != 0 {
		t.Fatalf("offset after rejected chunk = %d, want 0", offset)
	}

	resumed, err := svc.GetSession(session.UploadID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if resumed.BytesReceived != 0 {
		t.Fatalf("BytesReceived = %d, want 0", resumed.BytesReceived)
	}
	contents, err := os.ReadFile(filepath.Join(cfg.Upload.QuarantinePath, session.QuarantinePath))
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if string(contents) != "hello" {
		t.Fatalf("truncated file contents = %q, want %q", string(contents), "hello")
	}
}

func TestCompleteSessionRequiresFullUploadAndDoesNotPublish(t *testing.T) {
	cfg := testConfig(t.TempDir())
	publisher := &recordingPublisher{}
	svc := NewUploadService(&cfg)
	svc.Publisher = publisher

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "track.wav",
		FileSize: 10,
		Checksum: checksum("short"),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := svc.WriteChunk(session.UploadID, 0, strings.NewReader("short")); err != nil {
		t.Fatalf("WriteChunk() error = %v", err)
	}

	if _, err := svc.CompleteSession(context.Background(), session.UploadID); err == nil || !strings.Contains(err.Error(), "is incomplete") {
		t.Fatalf("CompleteSession() error = %v, want incomplete error", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("published events = %d, want 0", len(publisher.events))
	}
}

func TestCompleteSessionReturnsPublisherError(t *testing.T) {
	cfg := testConfig(t.TempDir())
	publisher := &recordingPublisher{err: errors.New("broker unavailable")}
	svc := NewUploadService(&cfg)
	svc.Publisher = publisher

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "track.wav",
		FileSize: 5,
		Checksum: checksum("hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := svc.WriteChunk(session.UploadID, 0, strings.NewReader("hello")); err != nil {
		t.Fatalf("WriteChunk() error = %v", err)
	}
	if _, err := svc.CompleteSession(context.Background(), session.UploadID); err == nil || !strings.Contains(err.Error(), "broker unavailable") {
		t.Fatalf("CompleteSession() error = %v, want publisher error", err)
	}
}

func TestCompleteSessionPublishesExpectedEvent(t *testing.T) {
	cfg := testConfig(t.TempDir())
	publisher := &recordingPublisher{}
	svc := NewUploadService(&cfg)
	svc.Publisher = publisher

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName:    "track.wav",
		FileSize:    5,
		ContentType: "audio/wav",
		Checksum:    checksum("hello"),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := svc.WriteChunk(session.UploadID, 0, strings.NewReader("hello")); err != nil {
		t.Fatalf("WriteChunk() error = %v", err)
	}

	completed, err := svc.CompleteSession(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("CompleteSession() error = %v", err)
	}
	if len(publisher.events) != 1 {
		t.Fatalf("published events = %d, want 1", len(publisher.events))
	}
	event := publisher.events[0]
	if event.Type != "media.asset.uploaded.quarantined" ||
		event.Source != "test" ||
		event.UploadID != completed.UploadID ||
		event.FileName != "track.wav" ||
		event.FileSize != 5 ||
		event.ContentType != "audio/wav" ||
		event.Checksum != checksum("hello") ||
		event.QuarantinePath != completed.QuarantinePath {
		t.Fatalf("unexpected event: %+v", event)
	}
	if event.EventID == "" || event.OccurredAt.IsZero() {
		t.Fatalf("event id/time missing: %+v", event)
	}
}

func TestWriteChunkRejectsUnknownInvalidAndCompletedSessions(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)

	if _, err := svc.WriteChunk("not-a-uuid", 0, strings.NewReader("x")); err == nil || !strings.Contains(err.Error(), "invalid upload_id") {
		t.Fatalf("WriteChunk(invalid id) error = %v, want invalid upload_id", err)
	}

	session, err := svc.CreateSession(dto.CreateUploadRequest{FileName: "track.wav", FileSize: 1, Checksum: checksum("x")})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := svc.WriteChunk(session.UploadID, 0, strings.NewReader("x")); err != nil {
		t.Fatalf("WriteChunk() error = %v", err)
	}
	if _, err := svc.CompleteSession(context.Background(), session.UploadID); err != nil {
		t.Fatalf("CompleteSession() error = %v", err)
	}
	if _, err := svc.WriteChunk(session.UploadID, 1, strings.NewReader("y")); err == nil || !strings.Contains(err.Error(), "is not accepting data") {
		t.Fatalf("WriteChunk(completed) error = %v, want not accepting data", err)
	}
}

func TestCompleteSessionRejectsChecksumMismatchAndDoesNotPublish(t *testing.T) {
	cfg := testConfig(t.TempDir())
	publisher := &recordingPublisher{}
	svc := NewUploadService(&cfg)
	svc.Publisher = publisher

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "track.wav",
		FileSize: 5,
		Checksum: checksum("other"),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}
	if _, err := svc.WriteChunk(session.UploadID, 0, strings.NewReader("hello")); err != nil {
		t.Fatalf("WriteChunk() error = %v", err)
	}

	if _, err := svc.CompleteSession(context.Background(), session.UploadID); err == nil || !strings.Contains(err.Error(), "checksum mismatch") {
		t.Fatalf("CompleteSession() error = %v, want checksum mismatch", err)
	}
	if len(publisher.events) != 0 {
		t.Fatalf("published events = %d, want 0", len(publisher.events))
	}

	failed, err := svc.GetSession(session.UploadID)
	if err != nil {
		t.Fatalf("GetSession() error = %v", err)
	}
	if failed.Status != domain.UploadStatusChecksumFailed {
		t.Fatalf("status = %s, want %s", failed.Status, domain.UploadStatusChecksumFailed)
	}
	if failed.ComputedChecksum != checksum("hello") {
		t.Fatalf("computed checksum = %q, want %q", failed.ComputedChecksum, checksum("hello"))
	}
}

func TestZeroByteUploadCompletesWithChecksum(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)

	session, err := svc.CreateSession(dto.CreateUploadRequest{
		FileName: "empty.wav",
		FileSize: 0,
		Checksum: checksum(""),
	})
	if err != nil {
		t.Fatalf("CreateSession() error = %v", err)
	}

	completed, err := svc.CompleteSession(context.Background(), session.UploadID)
	if err != nil {
		t.Fatalf("CompleteSession() error = %v", err)
	}
	if completed.Status != domain.UploadStatusQuarantined {
		t.Fatalf("status = %s, want %s", completed.Status, domain.UploadStatusQuarantined)
	}
	if completed.ComputedChecksum != checksum("") {
		t.Fatalf("ComputedChecksum = %q, want %q", completed.ComputedChecksum, checksum(""))
	}
}

func TestGetAndCompleteRejectUnknownUpload(t *testing.T) {
	cfg := testConfig(t.TempDir())
	svc := NewUploadService(&cfg)
	uploadID := "00000000-0000-0000-0000-000000000001"

	if _, err := svc.GetSession(uploadID); err == nil {
		t.Fatal("GetSession() expected error")
	}
	if _, err := svc.CompleteSession(context.Background(), uploadID); err == nil {
		t.Fatal("CompleteSession() expected error")
	}
}

func TestUploadRootDefaultsToQuarantineUnderUploadPath(t *testing.T) {
	var cfg config.AppConfig
	cfg.Upload.UploadPath = filepath.Join(t.TempDir(), "uploads")

	if got := uploadRoot(&cfg); got != filepath.Join(cfg.Upload.UploadPath, "quarantine") {
		t.Fatalf("uploadRoot() = %q, want default quarantine path", got)
	}
}

func TestParseUploadOffset(t *testing.T) {
	tests := []struct {
		name    string
		value   string
		want    int64
		wantErr bool
	}{
		{name: "zero", value: "0", want: 0},
		{name: "positive", value: "42", want: 42},
		{name: "missing", value: "", wantErr: true},
		{name: "negative", value: "-1", wantErr: true},
		{name: "not numeric", value: "abc", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseUploadOffset(tt.value)
			if tt.wantErr {
				if err == nil {
					t.Fatal("ParseUploadOffset() expected error")
				}
				return
			}
			if err != nil {
				t.Fatalf("ParseUploadOffset() error = %v", err)
			}
			if got != tt.want {
				t.Fatalf("ParseUploadOffset() = %d, want %d", got, tt.want)
			}
		})
	}
}

func TestOutboxEventPublisherWritesJSONLines(t *testing.T) {
	outboxPath := filepath.Join(t.TempDir(), "events", "outbox.ndjson")
	publisher := NewOutboxEventPublisher(outboxPath)
	event := domain.UploadEvent{
		EventID:        "event-1",
		Type:           "media.asset.uploaded.quarantined",
		Source:         "test",
		OccurredAt:     time.Now().UTC(),
		UploadID:       "upload-1",
		FileName:       "track.wav",
		FileSize:       5,
		QuarantinePath: "upload-1/track.wav",
	}

	if err := publisher.PublishUploadQuarantined(context.Background(), event); err != nil {
		t.Fatalf("PublishUploadQuarantined() error = %v", err)
	}
	if err := publisher.PublishUploadQuarantined(context.Background(), event); err != nil {
		t.Fatalf("PublishUploadQuarantined(second) error = %v", err)
	}

	contents, err := os.ReadFile(outboxPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(contents)), "\n")
	if len(lines) != 2 {
		t.Fatalf("outbox lines = %d, want 2: %s", len(lines), string(contents))
	}
	var decoded domain.UploadEvent
	if err := json.Unmarshal([]byte(lines[0]), &decoded); err != nil {
		t.Fatalf("Unmarshal() error = %v", err)
	}
	if decoded.EventID != event.EventID || decoded.Type != event.Type {
		t.Fatalf("decoded event = %+v, want %+v", decoded, event)
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

func checksum(value string) string {
	return fmt.Sprintf("%x", sha256.Sum256([]byte(value)))
}
