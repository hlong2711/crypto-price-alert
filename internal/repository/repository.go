package repository

import (
	"context"
	"crypto-price-alert/internal/domain"
	"time"
)

type JobRepository interface {
	CreateIfNotExists(ctx context.Context, job domain.Job) (domain.Job, bool, error)
	MarkSent(ctx context.Context, id string, sentAt time.Time) error
	MarkFailed(ctx context.Context, id string, reason string) error
}
