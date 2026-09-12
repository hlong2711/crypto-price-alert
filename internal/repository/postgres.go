package repository

import (
	"context"
	"errors"
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

	targetID, err := parseTargetID(job.TargetID)
	if err != nil {
		return domain.Job{}, false, err
	}
	model := database.NotificationJob{
		ID:           id,
		TargetID:     targetID,
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
		if err := r.db.WithContext(ctx).Where("target_id = ? AND symbol = ? AND interval = ? AND period_start = ?",
			model.TargetID,
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
		TargetID:     job.TargetID.String(),
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

func parseTargetID(value string) (uuid.UUID, error) {
	if value == "" {
		return uuid.Parse(database.LegacyTargetID)
	}
	return uuid.Parse(value)
}

func (r *PostgresRepository) FindOrCreateTarget(ctx context.Context, target domain.AlertTarget) (domain.AlertTarget, error) {
	if err := target.Validate(); err != nil {
		return domain.AlertTarget{}, fmt.Errorf("validate alert target: %w", err)
	}
	id, err := uuid.Parse(target.ID)
	if err != nil {
		return domain.AlertTarget{}, fmt.Errorf("parse target ID: %w", err)
	}
	model := database.AlertTarget{
		ID:             id,
		Provider:       target.Provider,
		TenantID:       target.TenantID,
		ExternalChatID: target.ExternalChatID,
		DisplayName:    target.DisplayName,
		CreatorUserID:  target.CreatorUserID,
		Enabled:        target.Enabled,
		CreatedAt:      target.CreatedAt,
		UpdatedAt:      target.UpdatedAt,
	}
	if model.CreatedAt.IsZero() {
		model.CreatedAt = time.Now().UTC()
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = model.CreatedAt
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
	if result.Error != nil {
		return domain.AlertTarget{}, fmt.Errorf("create alert target: %w", result.Error)
	}
	var existing database.AlertTarget
	if err := r.db.WithContext(ctx).Where("provider = ? AND tenant_id = ? AND external_chat_id = ?", target.Provider, target.TenantID, target.ExternalChatID).First(&existing).Error; err != nil {
		return domain.AlertTarget{}, fmt.Errorf("load alert target: %w", err)
	}
	return toAlertTargetDomain(existing), nil
}

func (r *PostgresRepository) GetTarget(ctx context.Context, provider domain.ChatProvider, tenantID, externalChatID string) (domain.AlertTarget, error) {
	var model database.AlertTarget
	if err := r.db.WithContext(ctx).Where("provider = ? AND tenant_id = ? AND external_chat_id = ?", provider, tenantID, externalChatID).First(&model).Error; err != nil {
		return domain.AlertTarget{}, err
	}
	return toAlertTargetDomain(model), nil
}

func (r *PostgresRepository) ListEnabledTargets(ctx context.Context) ([]domain.AlertTarget, error) {
	var models []database.AlertTarget
	if err := r.db.WithContext(ctx).Where("enabled = ?", true).Find(&models).Error; err != nil {
		return nil, fmt.Errorf("list enabled alert targets: %w", err)
	}
	result := make([]domain.AlertTarget, 0, len(models))
	for _, model := range models {
		result = append(result, toAlertTargetDomain(model))
	}
	return result, nil
}

func (r *PostgresRepository) CountTargets(ctx context.Context) (int64, error) {
	var count int64
	if err := r.db.WithContext(ctx).Model(&database.AlertTarget{}).Count(&count).Error; err != nil {
		return 0, fmt.Errorf("count alert targets: %w", err)
	}
	return count, nil
}

func (r *PostgresRepository) CreateAlertConfig(ctx context.Context, config domain.AlertConfig) error {
	if err := config.Validate(); err != nil {
		return fmt.Errorf("validate alert config: %w", err)
	}
	targetID, err := uuid.Parse(config.TargetID)
	if err != nil {
		return fmt.Errorf("parse target ID: %w", err)
	}
	model := database.AlertConfig{
		TargetID:  targetID,
		Enabled:   config.Enabled,
		Version:   config.Version,
		UpdatedBy: config.UpdatedBy,
		UpdatedAt: config.UpdatedAt,
	}
	if model.UpdatedAt.IsZero() {
		model.UpdatedAt = time.Now().UTC()
	}
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(&model).Error; err != nil {
			return err
		}
		return insertConfigChildren(tx, targetID, config)
	})
	if err != nil {
		return fmt.Errorf("create alert config: %w", err)
	}
	return nil
}

func (r *PostgresRepository) GetAlertConfig(ctx context.Context, targetID string) (domain.AlertConfig, error) {
	parsedTargetID, err := uuid.Parse(targetID)
	if err != nil {
		return domain.AlertConfig{}, fmt.Errorf("parse target ID: %w", err)
	}
	var config database.AlertConfig
	if err := r.db.WithContext(ctx).Where("target_id = ?", parsedTargetID).First(&config).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return domain.AlertConfig{}, fmt.Errorf("%w: %s", ErrAlertConfigNotFound, targetID)
		}
		return domain.AlertConfig{}, err
	}
	var symbols []database.AlertConfigSymbol
	if err := r.db.WithContext(ctx).Where("target_id = ?", parsedTargetID).Order("symbol").Find(&symbols).Error; err != nil {
		return domain.AlertConfig{}, fmt.Errorf("load alert symbols: %w", err)
	}
	var intervals []database.AlertConfigInterval
	if err := r.db.WithContext(ctx).Where("target_id = ?", parsedTargetID).Order("interval").Find(&intervals).Error; err != nil {
		return domain.AlertConfig{}, fmt.Errorf("load alert intervals: %w", err)
	}
	return toAlertConfigDomain(config, symbols, intervals), nil
}

func (r *PostgresRepository) ReplaceAlertConfig(ctx context.Context, config domain.AlertConfig, expectedVersion int64) (domain.AlertConfig, error) {
	if err := config.Validate(); err != nil {
		return domain.AlertConfig{}, fmt.Errorf("validate alert config: %w", err)
	}
	targetID, err := uuid.Parse(config.TargetID)
	if err != nil {
		return domain.AlertConfig{}, fmt.Errorf("parse target ID: %w", err)
	}
	var result domain.AlertConfig
	err = r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var current database.AlertConfig
		if err := tx.Where("target_id = ?", targetID).First(&current).Error; err != nil {
			return err
		}
		if current.Version != expectedVersion {
			return fmt.Errorf("alert config version conflict: expected %d, got %d", expectedVersion, current.Version)
		}
		updatedAt := time.Now().UTC()
		updates := map[string]any{
			"enabled":    config.Enabled,
			"version":    current.Version + 1,
			"updated_by": config.UpdatedBy,
			"updated_at": updatedAt,
		}
		updateResult := tx.Model(&database.AlertConfig{}).Where("target_id = ? AND version = ?", targetID, expectedVersion).Updates(updates)
		if updateResult.Error != nil {
			return updateResult.Error
		}
		if updateResult.RowsAffected != 1 {
			return fmt.Errorf("alert config version conflict")
		}
		if err := tx.Where("target_id = ?", targetID).Delete(&database.AlertConfigSymbol{}).Error; err != nil {
			return err
		}
		if err := tx.Where("target_id = ?", targetID).Delete(&database.AlertConfigInterval{}).Error; err != nil {
			return err
		}
		if err := insertConfigChildren(tx, targetID, config); err != nil {
			return err
		}
		result = config
		result.Version = current.Version + 1
		result.UpdatedAt = updatedAt
		return nil
	})
	if err != nil {
		return domain.AlertConfig{}, fmt.Errorf("replace alert config: %w", err)
	}
	return result, nil
}

func (r *PostgresRepository) SetAlertConfigEnabled(ctx context.Context, targetID string, enabled bool, updatedBy string, expectedVersion int64) (domain.AlertConfig, error) {
	config, err := r.GetAlertConfig(ctx, targetID)
	if err != nil {
		return domain.AlertConfig{}, err
	}
	config.Enabled = enabled
	config.UpdatedBy = updatedBy
	return r.ReplaceAlertConfig(ctx, config, expectedVersion)
}

func (r *PostgresRepository) ClaimInboundEvent(ctx context.Context, event domain.InboundEvent) (bool, error) {
	if err := event.Validate(); err != nil {
		return false, fmt.Errorf("validate inbound event: %w", err)
	}
	id, err := uuid.Parse(event.ID)
	if err != nil {
		return false, fmt.Errorf("parse inbound event ID: %w", err)
	}
	model := database.InboundEvent{
		ID:              id,
		Provider:        event.Provider,
		ExternalEventID: event.ExternalEventID,
		Message:         event.Message,
		ReceivedAt:      event.ReceivedAt,
		Status:          event.Status,
		ErrorMessage:    event.ErrorMessage,
	}
	result := r.db.WithContext(ctx).Clauses(clause.OnConflict{DoNothing: true}).Create(&model)
	if result.Error != nil {
		return false, fmt.Errorf("claim inbound event: %w", result.Error)
	}
	return result.RowsAffected == 1, nil
}

func (r *PostgresRepository) MarkInboundEventProcessed(ctx context.Context, provider domain.ChatProvider, externalEventID string, processedAt time.Time) error {
	return r.updateInboundEvent(ctx, provider, externalEventID, map[string]any{"status": "processed", "processed_at": processedAt})
}

func (r *PostgresRepository) MarkInboundEventFailed(ctx context.Context, provider domain.ChatProvider, externalEventID, reason string) error {
	return r.updateInboundEvent(ctx, provider, externalEventID, map[string]any{"status": "failed", "error_message": reason})
}

func (r *PostgresRepository) updateInboundEvent(ctx context.Context, provider domain.ChatProvider, externalEventID string, updates map[string]any) error {
	result := r.db.WithContext(ctx).Model(&database.InboundEvent{}).Where("provider = ? AND external_event_id = ?", provider, externalEventID).Updates(updates)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
