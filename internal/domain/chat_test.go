package domain

import (
	"testing"
	"time"
)

func TestAlertTargetValidation(t *testing.T) {
	target := AlertTarget{
		ID: "target-1", Provider: ChatProviderTelegram, TenantID: "tenant-1",
		ExternalChatID: "chat-1", CreatorUserID: "user-1",
	}
	if err := target.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := target
	invalid.Provider = ChatProvider("discord")
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected unsupported provider to be rejected")
	}
}

func TestAlertConfigValidation(t *testing.T) {
	config := AlertConfig{
		TargetID: "target-1", Symbols: []string{"BTCUSDT"},
		Intervals: []Interval{Interval1H}, UpdatedBy: "user-1",
	}
	if err := config.Validate(); err != nil {
		t.Fatal(err)
	}
	duplicate := config
	duplicate.Symbols = []string{"BTCUSDT", "BTCUSDT"}
	if err := duplicate.Validate(); err == nil {
		t.Fatal("expected duplicate symbols to be rejected")
	}
}

func TestInboundEventValidation(t *testing.T) {
	event := InboundEvent{
		ID: "event-1", Provider: ChatProviderSlack, ExternalEventID: "slack-event-1",
		ReceivedAt: time.Now(), Status: "received",
	}
	if err := event.Validate(); err != nil {
		t.Fatal(err)
	}
	invalid := event
	invalid.Status = ""
	if err := invalid.Validate(); err == nil {
		t.Fatal("expected missing status to be rejected")
	}
}
