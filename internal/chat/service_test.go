package chat

import (
	"context"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
)

type fakeConfigurationService struct {
	config       domain.AlertConfig
	replaceCalls int
}

func (f *fakeConfigurationService) GetConfig(context.Context, string) (domain.AlertConfig, error) {
	return f.config, nil
}
func (f *fakeConfigurationService) GetOrCreateConfig(context.Context, string, string) (domain.AlertConfig, error) {
	return f.config, nil
}
func (f *fakeConfigurationService) ReplaceConfig(_ context.Context, targetID string, symbols []string, intervals []domain.Interval, enabled bool, updatedBy string, expectedVersion int64) (domain.AlertConfig, error) {
	if f.config.Version != expectedVersion {
		return domain.AlertConfig{}, context.DeadlineExceeded
	}
	f.replaceCalls++
	f.config = domain.AlertConfig{TargetID: targetID, Enabled: enabled, Symbols: symbols, Intervals: intervals, Version: expectedVersion + 1, UpdatedBy: updatedBy}
	return f.config, nil
}
func (f *fakeConfigurationService) Enable(context.Context, string, string, int64) (domain.AlertConfig, error) {
	return f.config, nil
}
func (f *fakeConfigurationService) Pause(context.Context, string, string, int64) (domain.AlertConfig, error) {
	return f.config, nil
}

func newChatService(t *testing.T) (*Service, *fakeConfigurationService, *MemorySessionStore, domain.AlertTarget) {
	t.Helper()
	configuration := &fakeConfigurationService{config: domain.AlertConfig{TargetID: "target", Enabled: true, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}, Version: 2}}
	sessions := NewMemorySessionStore()
	service, err := NewService(configuration, CreatorAuthorizer{}, sessions, time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	return service, configuration, sessions, domain.AlertTarget{ID: "target", CreatorUserID: "creator"}
}

func TestServiceRejectsNonCreatorMutation(t *testing.T) {
	service, configuration, _, target := newChatService(t)
	_, err := service.Handle(context.Background(), Command{Target: target, ActorUserID: "member", Action: ActionPause})
	if err == nil || configuration.replaceCalls != 0 {
		t.Fatal("expected mutation to be rejected before configuration service")
	}
}

func TestServiceSessionOwnershipAndStaleVersion(t *testing.T) {
	service, configuration, sessions, target := newChatService(t)
	response, err := service.Handle(context.Background(), Command{Target: target, ActorUserID: "creator", Action: ActionConfigure})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateSession(context.Background(), Command{Target: target, ActorUserID: "member", Action: ActionSave, Arguments: []string{response}}, []string{"ETHUSDT"}, []domain.Interval{domain.Interval4H}); err == nil {
		t.Fatal("expected wrong actor to be rejected")
	}
	configuration.config.Version++
	_, err = service.Handle(context.Background(), Command{Target: target, ActorUserID: "creator", Action: ActionSave, Arguments: []string{response}})
	if err == nil || configuration.replaceCalls != 0 {
		t.Fatal("expected stale session to be rejected")
	}
	if _, err := sessions.Get(context.Background(), response); err != nil {
		t.Fatal("stale session should remain available for user restart")
	}
}

func TestServiceSaveConsumesSession(t *testing.T) {
	service, configuration, sessions, target := newChatService(t)
	response, err := service.Handle(context.Background(), Command{Target: target, ActorUserID: "creator", Action: ActionConfigure})
	if err != nil {
		t.Fatal(err)
	}
	if err := service.UpdateSession(context.Background(), Command{Target: target, ActorUserID: "creator", Action: ActionSave, Arguments: []string{response}}, []string{"ETHUSDT"}, []domain.Interval{domain.Interval4H}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Handle(context.Background(), Command{Target: target, ActorUserID: "creator", Action: ActionSave, Arguments: []string{response}}); err != nil {
		t.Fatal(err)
	}
	if _, err := service.Handle(context.Background(), Command{Target: target, ActorUserID: "creator", Action: ActionSave, Arguments: []string{response}}); err == nil {
		t.Fatal("expected replayed save to fail")
	}
	if configuration.replaceCalls != 1 {
		t.Fatalf("expected one replacement, got %d", configuration.replaceCalls)
	}
	if _, err := sessions.Get(context.Background(), response); err == nil {
		t.Fatal("expected saved session to be deleted")
	}
}
