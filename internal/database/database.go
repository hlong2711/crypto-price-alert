package database

import (
	"fmt"

	"crypto-price-alert/internal/domain"

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
		&AlertConfigSymbol{},
		&AlertConfigInterval{},
		&InboundEvent{},
	); err != nil {
		return err
	}

	if db.Migrator().HasTable(&AlertConfig{}) {
		if err := db.Exec("ALTER TABLE alert_configs ADD COLUMN IF NOT EXISTS market_provider varchar(32)").Error; err != nil {
			return fmt.Errorf("add alert config market provider: %w", err)
		}
		if err := db.Exec("UPDATE alert_configs SET market_provider = ? WHERE market_provider IS NULL", domain.MarketProviderBinance).Error; err != nil {
			return fmt.Errorf("backfill alert config market provider: %w", err)
		}
		if err := db.Exec("ALTER TABLE alert_configs ALTER COLUMN market_provider SET NOT NULL").Error; err != nil {
			return fmt.Errorf("require alert config market provider: %w", err)
		}
	}
	if err := db.AutoMigrate(&AlertConfig{}); err != nil {
		return err
	}

	if db.Migrator().HasTable(&NotificationJob{}) {
		if err := db.Exec("ALTER TABLE notification_jobs ADD COLUMN IF NOT EXISTS market_provider varchar(32)").Error; err != nil {
			return fmt.Errorf("add notification job market provider: %w", err)
		}
		if err := db.Exec("UPDATE notification_jobs SET market_provider = ? WHERE market_provider IS NULL", domain.MarketProviderBinance).Error; err != nil {
			return fmt.Errorf("backfill notification job market provider: %w", err)
		}
		if err := db.Exec("ALTER TABLE notification_jobs ALTER COLUMN market_provider SET NOT NULL").Error; err != nil {
			return fmt.Errorf("require notification job market provider: %w", err)
		}
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
		if err := db.Exec("DROP INDEX IF EXISTS idx_notification_jobs_target_key").Error; err != nil {
			return fmt.Errorf("drop notification job index: %w", err)
		}
	}

	if err := db.AutoMigrate(
		&NotificationJob{},
	); err != nil {
		return err
	}
	if db.Migrator().HasTable(&NotificationJob{}) {
		if err := db.Exec("CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_jobs_target_key ON notification_jobs (target_id, market_provider, symbol, interval, period_start)").Error; err != nil {
			return fmt.Errorf("create notification job index: %w", err)
		}
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
