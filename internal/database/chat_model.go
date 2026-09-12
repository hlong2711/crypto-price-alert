package database

import (
	"time"

	"crypto-price-alert/internal/domain"

	"github.com/google/uuid"
)

type AlertTarget struct {
	ID             uuid.UUID           `gorm:"type:uuid;primaryKey"`
	Provider       domain.ChatProvider `gorm:"type:varchar(16);not null;index:idx_alert_targets_identity,unique,priority:1"`
	TenantID       string              `gorm:"type:varchar(128);not null;index:idx_alert_targets_identity,unique,priority:2"`
	ExternalChatID string              `gorm:"type:varchar(128);not null;index:idx_alert_targets_identity,unique,priority:3"`
	DisplayName    string              `gorm:"type:varchar(255)"`
	CreatorUserID  string              `gorm:"type:varchar(128);not null"`
	Enabled        bool                `gorm:"not null;default:false;index"`
	CreatedAt      time.Time           `gorm:"not null"`
	UpdatedAt      time.Time           `gorm:"not null"`
}

func (AlertTarget) TableName() string { return "alert_targets" }

type AlertConfig struct {
	TargetID  uuid.UUID `gorm:"type:uuid;primaryKey"`
	Enabled   bool      `gorm:"not null;default:false"`
	Version   int64     `gorm:"not null;default:0"`
	UpdatedBy string    `gorm:"type:varchar(128);not null"`
	UpdatedAt time.Time `gorm:"not null"`
}

func (AlertConfig) TableName() string { return "alert_configs" }

type AlertConfigSymbol struct {
	TargetID uuid.UUID `gorm:"type:uuid;primaryKey;index:idx_alert_config_symbols_key,unique,priority:1"`
	Symbol   string    `gorm:"type:varchar(32);primaryKey;index:idx_alert_config_symbols_key,unique,priority:2"`
}

func (AlertConfigSymbol) TableName() string { return "alert_config_symbols" }

type AlertConfigInterval struct {
	TargetID uuid.UUID       `gorm:"type:uuid;primaryKey;index:idx_alert_config_intervals_key,unique,priority:1"`
	Interval domain.Interval `gorm:"type:varchar(8);primaryKey;index:idx_alert_config_intervals_key,unique,priority:2"`
}

func (AlertConfigInterval) TableName() string { return "alert_config_intervals" }

type InboundEvent struct {
	ID              uuid.UUID           `gorm:"type:uuid;primaryKey"`
	Provider        domain.ChatProvider `gorm:"type:varchar(16);not null;index:idx_inbound_events_identity,unique,priority:1"`
	ExternalEventID string              `gorm:"type:varchar(255);not null;index:idx_inbound_events_identity,unique,priority:2"`
	Message         string              `gorm:"type:text"`
	ReceivedAt      time.Time           `gorm:"not null"`
	ProcessedAt     *time.Time          `gorm:""`
	Status          string              `gorm:"type:varchar(16);not null;index"`
	ErrorMessage    string              `gorm:"type:text"`
}

func (InboundEvent) TableName() string { return "inbound_events" }
