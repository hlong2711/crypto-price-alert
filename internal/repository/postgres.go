package repository

import (
	"context"
	"fmt"
	"time"

	"crypto-price-alert/internal/database"
	"crypto-price-alert/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type PostgresRepository struct {
	db *gorm.DB
}

func NewPostgresRepository(db *gorm.DB) (*PostgresRepository, error) {
	if db == nil {
		return nil, fmt.Errorf("database handle is nil")
	}
	return &PostgresRepository{db: db}, nil
}

func (r *PostgresRepository) CreateIfNotExists(ctx context.Context, job domain.Job) (domain.Job, bool, error) {
	if err := job.Validate(); err != nil {
		return domain.Job{}, false, fmt.Errorf("validate job: %w", err)
	}
	id, err := uuid.Parse(job.ID)
	if err != nil {
		return domain.Job{}, false, fmt.Errorf("parse job ID: %w", err)
	}

	model := database.NotificationJob{
		ID:           id,
		Symbol:       job.Symbol,
		Interval:     job.Interval,
		PeriodStart:  job.PeriodStart,
		PeriodEnd:    job.PeriodEnd,
		Status:       job.Status,
		ErrorMessage: job.ErrorMessage,
		SentAt:       job.SentAt,
		CreatedAt:    job.CreatedAt,
		UpdatedAt:    job.UpdatedAt,
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now().UTC()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = model.CreatedAt
	}

	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
	if result.Error != nil {
		return domain.Job{}, false, fmt.Errorf("create notification job: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		var existing database.NotificationJob
		if err := r.db.WithContext(ctx).Where("symbol = ? AND interval = ? AND period_start = ?",
			job.Symbol,
			job.Interval,
			job.PeriodStart).First(&existing).Error; err != nil {
			return domain.Job{}, false, fmt.Errorf("load existing notification job: %w", err)
		}
		return toDomain(existing), false, nil
	}
	return toDomain(model), true, nil
}

func (r *PostgresRepository) MarkSent(ctx context.Context, id string, sentAt time.Time) error {
	return r.updateStatus(ctx, id, domain.JobSent, "", &sentAt)
}

func (r *PostgresRepository) MarkFailed(ctx context.Context, id string, reason string) error {
	return r.updateStatus(ctx, id, domain.JobFailed, reason, nil)
}

func (r *PostgresRepository) updateStatus(ctx context.Context,
	id string,
	status domain.JobStatus,
	reason string,
	sentAt *time.Time) error {
	jobID, err := uuid.Parse(id)
	if err != nil {
		return fmt.Errorf("parse job ID: %w", err)
	}
	updates := map[string]any{"status": status, "error_message": reason, "updated_at": time.Now().UTC()}
	if status == domain.JobSent {
		updates["sent_at"] = sentAt
	}
	result := r.db.WithContext(ctx).Model(&database.NotificationJob{}).Where("id = ?", jobID).Updates(updates)
	if result.Error != nil {
		return fmt.Errorf("update notification job: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

func toDomain(job database.NotificationJob) domain.Job {
	return domain.Job{
		ID:           job.ID.String(),
		Symbol:       job.Symbol,
		Interval:     job.Interval,
		PeriodStart:  job.PeriodStart,
		PeriodEnd:    job.PeriodEnd,
		Status:       job.Status,
		ErrorMessage: job.ErrorMessage,
		SentAt:       job.SentAt,
		CreatedAt:    job.CreatedAt,
		UpdatedAt:    job.UpdatedAt,
	}
}
