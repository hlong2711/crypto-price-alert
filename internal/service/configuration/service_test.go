package configuration

import (
	"context"
	"testing"

	"crypto-price-alert/internal/domain"
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
}

func (f *fakeConfigRepository) CreateAlertConfig(context.Context, domain.AlertConfig) error {
	return nil
}
func (f *fakeConfigRepository) GetAlertConfig(context.Context, string) (domain.AlertConfig, error) {
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
