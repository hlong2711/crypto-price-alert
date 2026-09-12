package repository

import (
	"context"
	"errors"
	"time"

	"crypto-price-alert/internal/database"
	"crypto-price-alert/internal/domain"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ErrAlertConfigNotFound distinguishes a missing first-time configuration from
// other database failures.
var ErrAlertConfigNotFound = errors.New("alert config not found")

type AlertTargetRepository interface {
	FindOrCreateTarget(ctx context.Context, target domain.AlertTarget) (domain.AlertTarget, error)
	GetTarget(ctx context.Context, provider domain.ChatProvider, tenantID, externalChatID string) (domain.AlertTarget, error)
	ListEnabledTargets(ctx context.Context) ([]domain.AlertTarget, error)
	CountTargets(ctx context.Context) (int64, error)
}

type AlertConfigRepository interface {
	CreateAlertConfig(ctx context.Context, config domain.AlertConfig) error
	GetAlertConfig(ctx context.Context, targetID string) (domain.AlertConfig, error)
	ReplaceAlertConfig(ctx context.Context, config domain.AlertConfig, expectedVersion int64) (domain.AlertConfig, error)
	SetAlertConfigEnabled(ctx context.Context, targetID string, enabled bool, updatedBy string, expectedVersion int64) (domain.AlertConfig, error)
}

type InboundEventRepository interface {
	ClaimInboundEvent(ctx context.Context, event domain.InboundEvent) (bool, error)
	MarkInboundEventProcessed(ctx context.Context, provider domain.ChatProvider, externalEventID string, processedAt time.Time) error
	MarkInboundEventFailed(ctx context.Context, provider domain.ChatProvider, externalEventID, reason string) error
}

func insertConfigChildren(tx *gorm.DB, targetID uuid.UUID, config domain.AlertConfig) error {
	for _, symbol := range config.Symbols {
		if err := tx.Create(&database.AlertConfigSymbol{TargetID: targetID, Symbol: symbol}).Error; err != nil {
			return err
		}
	}
	for _, interval := range config.Intervals {
		if err := tx.Create(&database.AlertConfigInterval{TargetID: targetID, Interval: interval}).Error; err != nil {
			return err
		}
	}
	return nil
}

func toAlertTargetDomain(target database.AlertTarget) domain.AlertTarget {
	return domain.AlertTarget{
		ID:             target.ID.String(),
		Provider:       target.Provider,
		TenantID:       target.TenantID,
		ExternalChatID: target.ExternalChatID,
		DisplayName:    target.DisplayName,
		CreatorUserID:  target.CreatorUserID,
		Enabled:        target.Enabled,
		CreatedAt:      target.CreatedAt,
		UpdatedAt:      target.UpdatedAt,
	}
}

func toAlertConfigDomain(config database.AlertConfig, symbols []database.AlertConfigSymbol, intervals []database.AlertConfigInterval) domain.AlertConfig {
	result := domain.AlertConfig{
		TargetID:  config.TargetID.String(),
		Enabled:   config.Enabled,
		Version:   config.Version,
		UpdatedBy: config.UpdatedBy,
		UpdatedAt: config.UpdatedAt,
		Symbols:   make([]string, 0, len(symbols)),
		Intervals: make([]domain.Interval, 0, len(intervals)),
	}
	for _, symbol := range symbols {
		result.Symbols = append(result.Symbols, symbol.Symbol)
	}
	for _, interval := range intervals {
		result.Intervals = append(result.Intervals, interval.Interval)
	}
	return result
}
