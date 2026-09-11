package database

import (
	"testing"
	"time"

	"crypto-price-alert/internal/domain"

	"github.com/google/uuid"
)

func TestNotificationJobMapping(t *testing.T) {
	now := time.Now()
	job := NotificationJob{
		ID: uuid.New(), TargetID: uuid.New(), Symbol: "BTCUSDT", Interval: domain.Interval1H,
		PeriodStart: now, PeriodEnd: now.Add(time.Hour), Status: domain.JobPending,
	}
	if job.TableName() != "notification_jobs" {
		t.Fatalf("unexpected table name: %s", job.TableName())
	}
	if job.ID == uuid.Nil || job.TargetID == uuid.Nil || job.Symbol == "" || job.Interval != domain.Interval1H || job.Status != domain.JobPending {
		t.Fatal("job mapping has missing required values")
	}
}

func TestChatModelsUseExpectedTables(t *testing.T) {
	if (AlertTarget{}).TableName() != "alert_targets" ||
		(AlertConfig{}).TableName() != "alert_configs" ||
		(AlertConfigSymbol{}).TableName() != "alert_config_symbols" ||
		(AlertConfigInterval{}).TableName() != "alert_config_intervals" ||
		(InboundEvent{}).TableName() != "inbound_events" {
		t.Fatal("unexpected chat model table name")
	}
}

func TestOpenRejectsEmptyURL(t *testing.T) {
	if _, err := Open(""); err == nil {
		t.Fatal("expected empty database URL error")
	}
}

func TestMigrateRejectsNilDatabase(t *testing.T) {
	if err := Migrate(nil); err == nil {
		t.Fatal("expected nil database error")
	}
}
