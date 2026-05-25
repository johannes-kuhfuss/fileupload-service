package service

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/johannes-kuhfuss/fileupload-service/config"
	"github.com/johannes-kuhfuss/fileupload-service/domain"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type EventPublisher interface {
	PublishUploadQuarantined(context.Context, domain.UploadEvent) error
}

type OutboxEventPublisher struct {
	path string
	cfg  *config.AppConfig
	mu   sync.Mutex
}

func NewOutboxEventPublisher(path string, cfg ...*config.AppConfig) OutboxEventPublisher {
	var appConfig *config.AppConfig
	if len(cfg) > 0 {
		appConfig = cfg[0]
	}
	return OutboxEventPublisher{
		path: path,
		cfg:  appConfig,
	}
}

func (p *OutboxEventPublisher) PublishUploadQuarantined(ctx context.Context, event domain.UploadEvent) error {
	start := time.Now()
	defer p.recordPublishDuration(ctx, start)
	defer p.recordStageDuration(ctx, start)

	if p.path == "" {
		p.recordPublished(ctx, event)
		return nil
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if err := os.MkdirAll(filepath.Dir(p.path), 0o755); err != nil {
		p.recordFailure(ctx, "mkdir")
		return err
	}

	f, err := os.OpenFile(p.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		p.recordFailure(ctx, "open")
		return err
	}
	defer f.Close()

	if err := json.NewEncoder(f).Encode(event); err != nil {
		p.recordFailure(ctx, "encode")
		return err
	}
	if err := f.Sync(); err != nil {
		p.recordFailure(ctx, "sync")
		return err
	}
	p.recordPublished(ctx, event)
	return nil
}

func (p *OutboxEventPublisher) recordPublished(ctx context.Context, event domain.UploadEvent) {
	if p.cfg != nil && p.cfg.Metrics.OutboxPublishedCounter != nil {
		p.cfg.Metrics.OutboxPublishedCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("event.type", event.Type)))
	}
}

func (p *OutboxEventPublisher) recordFailure(ctx context.Context, reason string) {
	if p.cfg != nil && p.cfg.Metrics.OutboxFailureCounter != nil {
		p.cfg.Metrics.OutboxFailureCounter.Add(ctx, 1, metric.WithAttributes(attribute.String("failure.reason", reason)))
	}
}

func (p *OutboxEventPublisher) recordPublishDuration(ctx context.Context, start time.Time) {
	if p.cfg != nil && p.cfg.Metrics.OutboxPublishDuration != nil {
		p.cfg.Metrics.OutboxPublishDuration.Record(ctx, time.Since(start).Seconds())
	}
}

func (p *OutboxEventPublisher) recordStageDuration(ctx context.Context, start time.Time) {
	if p.cfg != nil && p.cfg.Metrics.StageDurationHistogram != nil {
		p.cfg.Metrics.StageDurationHistogram.Record(ctx, time.Since(start).Seconds(), metric.WithAttributes(attribute.String("stage", "publish_event")))
	}
}
