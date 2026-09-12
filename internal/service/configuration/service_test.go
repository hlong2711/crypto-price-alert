package configuration

import (
	"context"
	"errors"
	"testing"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/repository"
)

type fakeTargetRepository struct{}

func (fakeTargetRepository) FindOrCreateTarget(context.Context, domain.AlertTarget) (domain.AlertTarget, error) {
	return domain.AlertTarget{}, nil
}
func (fakeTargetRepository) GetTarget(context.Context, domain.ChatProvider, string, string) (domain.AlertTarget, error) {
	return domain.AlertTarget{}, nil
}
func (fakeTargetRepository) ListEnabledTargets(context.Context) ([]domain.AlertTarget, error) {
	return nil, nil
}
func (fakeTargetRepository) CountTargets(context.Context) (int64, error) { return 0, nil }

type fakeConfigRepository struct {
	replaced domain.AlertConfig
	created  domain.AlertConfig
	missing  bool
}

func (f *fakeConfigRepository) CreateAlertConfig(_ context.Context, config domain.AlertConfig) error {
	f.created = config
	return nil
}

func (f *fakeConfigRepository) GetAlertConfig(_ context.Context, targetID string) (domain.AlertConfig, error) {
	if f.missing && f.created.TargetID == "" {
		return domain.AlertConfig{}, errors.Join(repository.ErrAlertConfigNotFound, errors.New("missing"))
	}
	if f.created.TargetID != "" {
		f.created.TargetID = targetID
		return f.created, nil
	}
	return domain.AlertConfig{}, nil
}
func (f *fakeConfigRepository) ReplaceAlertConfig(_ context.Context, config domain.AlertConfig, expectedVersion int64) (domain.AlertConfig, error) {
	config.Version = expectedVersion + 1
	f.replaced = config
	return config, nil
}
func (f *fakeConfigRepository) SetAlertConfigEnabled(context.Context, string, bool, string, int64) (domain.AlertConfig, error) {
	return domain.AlertConfig{}, nil
}

func newTestService(t *testing.T) (*Service, *fakeConfigRepository) {
	t.Helper()
	configs := &fakeConfigRepository{}
	service, err := NewService(fakeTargetRepository{}, configs, []string{"BTCUSDT", "ETHUSDT"}, []domain.Interval{domain.Interval1H, domain.Interval4H}, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	return service, configs
}

func TestReplaceConfigNormalizesAndValidates(t *testing.T) {
	service, configs := newTestService(t)
	result, err := service.ReplaceConfig(context.Background(), "target", []string{" btcusdt "}, []domain.Interval{domain.Interval1H}, true, "creator", 0)
	if err != nil {
		t.Fatal(err)
	}
	if result.Version != 1 || len(configs.replaced.Symbols) != 1 || configs.replaced.Symbols[0] != "BTCUSDT" {
		t.Fatalf("unexpected replacement: %+v", result)
	}
}

func TestReplaceConfigRejectsInvalidValues(t *testing.T) {
	service, _ := newTestService(t)
	tests := []struct {
		name      string
		symbols   []string
		intervals []domain.Interval
	}{
		{"unknown symbol", []string{"DOGEUSDT"}, []domain.Interval{domain.Interval1H}},
		{"duplicate symbol", []string{"BTCUSDT", "BTCUSDT"}, []domain.Interval{domain.Interval1H}},
		{"unsupported interval", []string{"BTCUSDT"}, []domain.Interval{domain.Interval("15m")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := service.ReplaceConfig(context.Background(), "target", tt.symbols, tt.intervals, true, "creator", 0); err == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}

func TestGetOrCreateConfigCreatesDisabledDefault(t *testing.T) {
	configs := &fakeConfigRepository{missing: true}
	service, err := NewService(fakeTargetRepository{}, configs, []string{"BTCUSDT", "ETHUSDT"}, []domain.Interval{domain.Interval1H, domain.Interval4H}, 2, 10)
	if err != nil {
		t.Fatal(err)
	}
	config, err := service.GetOrCreateConfig(context.Background(), "target", "creator")
	if err != nil {
		t.Fatal(err)
	}
	if config.Enabled || config.TargetID != "target" || len(config.Symbols) != 1 || config.Symbols[0] != "BTCUSDT" || len(config.Intervals) != 1 || config.Intervals[0] != domain.Interval1H {
		t.Fatalf("unexpected created config: %+v", config)
	}
}
