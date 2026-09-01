package database

import (
	"time"

	"crypto-price-alert/internal/domain"
	"github.com/google/uuid"
)

type NotificationJob struct {
	ID           uuid.UUID        `gorm:"type:uuid;primaryKey"`
	Symbol       string           `gorm:"type:varchar(32);not null;index:idx_notification_jobs_key,unique,priority:1"`
	Interval     domain.Interval  `gorm:"type:varchar(8);not null;index:idx_notification_jobs_key,unique,priority:2"`
	PeriodStart  time.Time        `gorm:"not null;index:idx_notification_jobs_key,unique,priority:3"`
	PeriodEnd    time.Time        `gorm:"not null"`
	Status       domain.JobStatus `gorm:"type:varchar(16);not null;index"`
	ErrorMessage string           `gorm:"type:text"`
	SentAt       *time.Time       `gorm:""`
	CreatedAt    time.Time        `gorm:"not null"`
	UpdatedAt    time.Time        `gorm:"not null"`
}

func (NotificationJob) TableName() string { return "notification_jobs" }
