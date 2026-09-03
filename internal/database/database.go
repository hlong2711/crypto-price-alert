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
	return db.AutoMigrate(&NotificationJob{})
}

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
