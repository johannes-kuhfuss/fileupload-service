package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"github.com/johannes-kuhfuss/fileupload-service/dto"
)

type DefaultUploadService struct {
	Cfg       *config.AppConfig
	Publisher EventPublisher
}

func NewUploadService(cfg *config.AppConfig) DefaultUploadService {
	outboxPath := cfg.Events.OutboxPath
	if outboxPath == "" {
		outboxPath = filepath.Join(uploadRoot(cfg), "events", "upload-events.ndjson")
	}
	publisher := NewOutboxEventPublisher(outboxPath)
	return DefaultUploadService{
		Cfg:       cfg,
		Publisher: &publisher,
	}
}

func (s DefaultUploadService) CreateSession(identity domain.Identity, req dto.CreateUploadRequest) (domain.UploadSession, error) {
	if err := validateIdentity(identity); err != nil {
		return domain.UploadSession{}, err
	}
	fileName := sanitizeFileName(req.FileName)
	if fileName == "" {
		return domain.UploadSession{}, errors.New("file_name is required")
	}
	if req.FileSize < 0 {
		return domain.UploadSession{}, errors.New("file_size must be greater than or equal to zero")
	}
	if s.Cfg.Upload.MaxUploadBytes > 0 && req.FileSize > s.Cfg.Upload.MaxUploadBytes {
		return domain.UploadSession{}, fmt.Errorf("file_size exceeds max upload size of %d bytes", s.Cfg.Upload.MaxUploadBytes)
	}
	checksum, err := normalizeSHA256(req.Checksum)
	if err != nil {
		return domain.UploadSession{}, err
	}

	now := time.Now().UTC()
	uploadID := uuid.New().String()
	session := domain.UploadSession{
		UploadID:       uploadID,
		TenantID:       identity.TenantID,
		CreatedBy:      identity.Subject,
		FileName:       fileName,
		FileSize:       req.FileSize,
		ContentType:    req.ContentType,
		Checksum:       checksum,
		Status:         domain.UploadStatusReceiving,
		QuarantinePath: filepath.Join(identity.TenantID, uploadID, fileName),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	if err := os.MkdirAll(sessionDir(s.Cfg, identity.TenantID, uploadID), 0o755); err != nil {
		return domain.UploadSession{}, err
	}
	if err := os.MkdirAll(filepath.Dir(absoluteQuarantinePath(s.Cfg, session)), 0o755); err != nil {
		return domain.UploadSession{}, err
	}
	f, err := os.OpenFile(absoluteQuarantinePath(s.Cfg, session), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return domain.UploadSession{}, err
	}
	if err := f.Close(); err != nil {
		return domain.UploadSession{}, err
	}
	if err := persistSession(s.Cfg, session); err != nil {
		return domain.UploadSession{}, err
	}
	return session, nil
}

func (s DefaultUploadService) GetSession(identity domain.Identity, uploadID string) (domain.UploadSession, error) {
	if err := validateIdentity(identity); err != nil {
		return domain.UploadSession{}, err
	}
	return readSession(s.Cfg, identity.TenantID, uploadID)
}

func (s DefaultUploadService) WriteChunk(identity domain.Identity, uploadID string, offset int64, body io.Reader) (int64, error) {
	if err := validateIdentity(identity); err != nil {
		return 0, err
	}
	session, err := readSession(s.Cfg, identity.TenantID, uploadID)
	if err != nil {
		return 0, err
	}
	if err := authorizeTenant(identity, session); err != nil {
		return 0, err
	}
	if session.Status != domain.UploadStatusReceiving {
		return session.BytesReceived, fmt.Errorf("upload %s is not accepting data", uploadID)
	}
	if offset != session.BytesReceived {
		return session.BytesReceived, fmt.Errorf("invalid offset %d, expected %d", offset, session.BytesReceived)
	}

	dst, err := os.OpenFile(absoluteQuarantinePath(s.Cfg, session), os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return session.BytesReceived, err
	}
	defer dst.Close()
	if _, err := dst.Seek(offset, io.SeekStart); err != nil {
		return session.BytesReceived, err
	}

	remaining := session.FileSize - session.BytesReceived
	if remaining < 0 {
		return session.BytesReceived, errors.New("session has received more bytes than declared")
	}
	reader := body
	if session.FileSize > 0 {
		reader = io.LimitReader(body, remaining+1)
	}
	written, err := io.Copy(dst, reader)
	if err != nil {
		return session.BytesReceived, err
	}
	if session.FileSize > 0 && written > remaining {
		if err := dst.Truncate(session.FileSize); err != nil {
			return session.BytesReceived, err
		}
		return session.BytesReceived, fmt.Errorf("chunk exceeds remaining upload size")
	}

	session.BytesReceived += written
	session.UpdatedAt = time.Now().UTC()
	if err := persistSession(s.Cfg, session); err != nil {
		return session.BytesReceived, err
	}
	return session.BytesReceived, nil
}

func (s DefaultUploadService) CompleteSession(ctx context.Context, identity domain.Identity, uploadID string) (domain.UploadSession, error) {
	if err := validateIdentity(identity); err != nil {
		return domain.UploadSession{}, err
	}
	session, err := readSession(s.Cfg, identity.TenantID, uploadID)
	if err != nil {
		return domain.UploadSession{}, err
	}
	if err := authorizeTenant(identity, session); err != nil {
		return domain.UploadSession{}, err
	}
	if session.BytesReceived != session.FileSize {
		return domain.UploadSession{}, fmt.Errorf("upload %s is incomplete: received %d of %d bytes", uploadID, session.BytesReceived, session.FileSize)
	}

	computedChecksum, err := fileSHA256(absoluteQuarantinePath(s.Cfg, session))
	if err != nil {
		return domain.UploadSession{}, err
	}
	session.ComputedChecksum = computedChecksum
	if session.Checksum != computedChecksum {
		session.Status = domain.UploadStatusChecksumFailed
		session.UpdatedAt = time.Now().UTC()
		if err := persistSession(s.Cfg, session); err != nil {
			return domain.UploadSession{}, err
		}
		return domain.UploadSession{}, fmt.Errorf("checksum mismatch: client %s server %s", session.Checksum, computedChecksum)
	}

	session.Status = domain.UploadStatusQuarantined
	session.UpdatedAt = time.Now().UTC()
	if err := persistSession(s.Cfg, session); err != nil {
		return domain.UploadSession{}, err
	}

	event := domain.UploadEvent{
		EventID:        uuid.New().String(),
		Type:           "media.asset.uploaded.quarantined",
		Source:         s.Cfg.Events.Source,
		OccurredAt:     time.Now().UTC(),
		UploadID:       session.UploadID,
		TenantID:       session.TenantID,
		ActorID:        session.CreatedBy,
		FileName:       session.FileName,
		FileSize:       session.FileSize,
		ContentType:    session.ContentType,
		Checksum:       session.Checksum,
		QuarantinePath: session.QuarantinePath,
	}
	if err := s.Publisher.PublishUploadQuarantined(ctx, event); err != nil {
		return domain.UploadSession{}, err
	}
	return session, nil
}

func sanitizeFileName(fileName string) string {
	var (
		newName      string
		invalidChars = regexp.MustCompile(`[<>:"/\\|?*;\[\]()\x00-\x1F]`)
		spaces       = regexp.MustCompile(`\s+`)
	)
	newName = strings.TrimSpace(fileName)
	newName = invalidChars.ReplaceAllString(newName, "")
	newName = strings.TrimRight(newName, ".")
	newName = spaces.ReplaceAllString(newName, "")

	return newName
}

func uploadRoot(cfg *config.AppConfig) string {
	if cfg.Upload.QuarantinePath != "" {
		return cfg.Upload.QuarantinePath
	}
	return filepath.Join(cfg.Upload.UploadPath, "quarantine")
}

func sessionDir(cfg *config.AppConfig, tenantID, uploadID string) string {
	return filepath.Join(uploadRoot(cfg), "_sessions", tenantID, uploadID)
}

func sessionPath(cfg *config.AppConfig, tenantID, uploadID string) string {
	return filepath.Join(sessionDir(cfg, tenantID, uploadID), "metadata.json")
}

func absoluteQuarantinePath(cfg *config.AppConfig, session domain.UploadSession) string {
	return filepath.Join(uploadRoot(cfg), session.QuarantinePath)
}

func persistSession(cfg *config.AppConfig, session domain.UploadSession) error {
	if err := os.MkdirAll(sessionDir(cfg, session.TenantID, session.UploadID), 0o755); err != nil {
		return err
	}
	f, err := os.Create(sessionPath(cfg, session.TenantID, session.UploadID))
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(session)
}

func readSession(cfg *config.AppConfig, tenantID, uploadID string) (domain.UploadSession, error) {
	if err := validateTenantID(tenantID); err != nil {
		return domain.UploadSession{}, err
	}
	if _, err := uuid.Parse(uploadID); err != nil {
		return domain.UploadSession{}, fmt.Errorf("invalid upload_id: %w", err)
	}
	f, err := os.Open(sessionPath(cfg, tenantID, uploadID))
	if err != nil {
		return domain.UploadSession{}, err
	}
	defer f.Close()

	var session domain.UploadSession
	if err := json.NewDecoder(f).Decode(&session); err != nil {
		return domain.UploadSession{}, err
	}
	if session.TenantID != tenantID {
		return domain.UploadSession{}, errors.New("upload belongs to a different tenant")
	}
	return session, nil
}

func ParseUploadOffset(value string) (int64, error) {
	if value == "" {
		return 0, errors.New("Upload-Offset header is required")
	}
	offset, err := strconv.ParseInt(value, 10, 64)
	if err != nil || offset < 0 {
		return 0, errors.New("Upload-Offset header must be a non-negative integer")
	}
	return offset, nil
}

func normalizeSHA256(value string) (string, error) {
	checksum := strings.TrimSpace(strings.ToLower(value))
	checksum = strings.TrimPrefix(checksum, "sha256:")
	if len(checksum) != sha256.Size*2 {
		return "", errors.New("checksum must be a SHA-256 hex digest")
	}
	for _, r := range checksum {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return "", errors.New("checksum must be a SHA-256 hex digest")
		}
	}
	return checksum, nil
}

func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hash := sha256.New()
	if _, err := io.Copy(hash, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func validateIdentity(identity domain.Identity) error {
	if strings.TrimSpace(identity.Subject) == "" {
		return errors.New("identity subject is required")
	}
	return validateTenantID(identity.TenantID)
}

func validateTenantID(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return errors.New("tenant_id is required")
	}
	for _, r := range tenantID {
		if (r >= 'a' && r <= 'z') ||
			(r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') ||
			r == '-' || r == '_' || r == '.' {
			continue
		}
		return errors.New("tenant_id contains unsupported characters")
	}
	return nil
}

func authorizeTenant(identity domain.Identity, session domain.UploadSession) error {
	if session.TenantID != identity.TenantID {
		return errors.New("upload belongs to a different tenant")
	}
	return nil
}
