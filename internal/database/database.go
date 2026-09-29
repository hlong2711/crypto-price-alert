package database

import (
	"fmt"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func Open(url string) (*gorm.DB, error) {
	if url == "" {
		return nil, fmt.Errorf("database URL is required")
	}
	db, err := gorm.Open(postgres.Open(url), &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("open postgres database: %w", err)
	}
	return db, nil
}

func Migrate(db *gorm.DB) error {
	if db == nil {
		return fmt.Errorf("database handle is nil")
	}

	if err := db.AutoMigrate(
		&AlertTarget{},
		&AlertConfig{},
		&AlertConfigSymbol{},
		&AlertConfigInterval{},
		&InboundEvent{},
	); err != nil {
		return err
	}

	if db.Migrator().HasTable(&NotificationJob{}) {
		if err := db.Exec("ALTER TABLE notification_jobs ADD COLUMN IF NOT EXISTS target_id uuid").Error; err != nil {
			return fmt.Errorf("add notification job target ID: %w", err)
		}
		if err := db.Exec("UPDATE notification_jobs SET target_id = ? WHERE target_id IS NULL", LegacyTargetID).Error; err != nil {
			return fmt.Errorf("backfill notification job target ID: %w", err)
		}
		if err := db.Exec("ALTER TABLE notification_jobs ALTER COLUMN target_id SET NOT NULL").Error; err != nil {
			return fmt.Errorf("require notification job target ID: %w", err)
		}
		if err := db.Exec("DROP INDEX IF EXISTS idx_notification_jobs_key").Error; err != nil {
			return fmt.Errorf("drop legacy notification job index: %w", err)
		}
	}

	if err := db.AutoMigrate(
		&NotificationJob{},
	); err != nil {
		return err
	}

	return nil
}

const LegacyTargetID = "00000000-0000-0000-0000-000000000001"

func Close(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return fmt.Errorf("get sql database: %w", err)
	}
	return sqlDB.Close()
}

func InitDatabase(url string) (*gorm.DB, error) {
	db, err := Open(url)
	if err != nil {
		return nil, fmt.Errorf("Open DB failed %w", err)
	}
	sql, err := db.DB()
	sql.SetMaxIdleConns(2)
	sql.SetMaxOpenConns(10)

	err = Migrate(db)
	if err != nil {
		return nil, fmt.Errorf("Migrate DB failed %w", err)
	}

	return db, nil
}
