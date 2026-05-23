package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/johannes-kuhfuss/fileupload-service/domain"
)

type EventPublisher interface {
	PublishUploadQuarantined(context.Context, domain.UploadEvent) error
}

type OutboxEventPublisher struct {
	path string
	mu   sync.Mutex
}

func NewOutboxEventPublisher(path string) OutboxEventPublisher {
	return OutboxEventPublisher{
		path: path,
	}
}

func (p *OutboxEventPublisher) PublishUploadQuarantined(_ context.Context, event domain.UploadEvent) error {
	if p.path == "" {
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		return err
	}

	f, err := os.OpenFile(p.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(event); err != nil {
		return err
	}
	return f.Sync()
}
