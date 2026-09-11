package configuration

import (
	"context"
	"fmt"
	"strings"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/repository"
)

type Service struct {
	targets          repository.AlertTargetRepository
	configs          repository.AlertConfigRepository
	allowedSymbols   map[string]struct{}
	allowedIntervals map[domain.Interval]struct{}
	maxSymbols       int
	maxTargets       int
}

func NewService(
	targets repository.AlertTargetRepository,
	configs repository.AlertConfigRepository,
	allowedSymbols []string,
	allowedIntervals []domain.Interval,
	maxSymbols int,
	maxTargets int,
) (*Service, error) {
	if targets == nil || configs == nil || len(allowedSymbols) == 0 || len(allowedIntervals) == 0 || maxSymbols < 1 || maxTargets < 1 {
		return nil, fmt.Errorf("invalid configuration service settings")
	}
	symbols := make(map[string]struct{}, len(allowedSymbols))
	for _, symbol := range allowedSymbols {
		symbol = normalizeSymbol(symbol)
		if symbol == "" {
			return nil, fmt.Errorf("allowed symbols must not be empty")
		}
		symbols[symbol] = struct{}{}
	}
	intervals := make(map[domain.Interval]struct{}, len(allowedIntervals))
	for _, interval := range allowedIntervals {
		if err := interval.Validate(); err != nil {
			return nil, fmt.Errorf("allowed interval: %w", err)
		}
		intervals[interval] = struct{}{}
	}
	return &Service{
		targets:          targets,
		configs:          configs,
		allowedSymbols:   symbols,
		allowedIntervals: intervals,
		maxSymbols:       maxSymbols,
		maxTargets:       maxTargets,
	}, nil
}

func (s *Service) AllowedSymbols() []string {
	result := make([]string, 0, len(s.allowedSymbols))
	for symbol := range s.allowedSymbols {
		result = append(result, symbol)
	}
	return result
}

func (s *Service) AllowedIntervals() []domain.Interval {
	result := make([]domain.Interval, 0, len(s.allowedIntervals))
	for interval := range s.allowedIntervals {
		result = append(result, interval)
	}
	return result
}

func (s *Service) GetTarget(ctx context.Context, provider domain.ChatProvider, tenantID, externalChatID string) (domain.AlertTarget, error) {
	return s.targets.GetTarget(ctx, provider, tenantID, externalChatID)
}

func (s *Service) GetConfig(ctx context.Context, targetID string) (domain.AlertConfig, error) {
	return s.configs.GetAlertConfig(ctx, targetID)
}

func (s *Service) ReplaceConfig(
	ctx context.Context,
	targetID string,
	symbols []string,
	intervals []domain.Interval,
	enabled bool,
	updatedBy string,
	expectedVersion int64,
) (domain.AlertConfig, error) {
	normalizedSymbols, err := s.validateSymbols(symbols)
	if err != nil {
		return domain.AlertConfig{}, err
	}
	normalizedIntervals, err := s.validateIntervals(intervals)
	if err != nil {
		return domain.AlertConfig{}, err
	}
	if strings.TrimSpace(updatedBy) == "" {
		return domain.AlertConfig{}, fmt.Errorf("updated_by is required")
	}
	return s.configs.ReplaceAlertConfig(ctx, domain.AlertConfig{
		TargetID:  targetID,
		Enabled:   enabled,
		Symbols:   normalizedSymbols,
		Intervals: normalizedIntervals,
		Version:   expectedVersion,
		UpdatedBy: strings.TrimSpace(updatedBy),
	}, expectedVersion)
}

func (s *Service) CreateConfig(ctx context.Context, targetID string, symbols []string, intervals []domain.Interval, enabled bool, updatedBy string) error {
	normalizedSymbols, err := s.validateSymbols(symbols)
	if err != nil {
		return err
	}
	normalizedIntervals, err := s.validateIntervals(intervals)
	if err != nil {
		return err
	}
	if strings.TrimSpace(updatedBy) == "" {
		return fmt.Errorf("updated_by is required")
	}
	return s.configs.CreateAlertConfig(ctx, domain.AlertConfig{
		TargetID:  targetID,
		Enabled:   enabled,
		Symbols:   normalizedSymbols,
		Intervals: normalizedIntervals,
		Version:   0,
		UpdatedBy: strings.TrimSpace(updatedBy),
	})
}

func (s *Service) Enable(ctx context.Context, targetID, updatedBy string, expectedVersion int64) (domain.AlertConfig, error) {
	return s.setEnabled(ctx, targetID, updatedBy, expectedVersion, true)
}

func (s *Service) Pause(ctx context.Context, targetID, updatedBy string, expectedVersion int64) (domain.AlertConfig, error) {
	return s.setEnabled(ctx, targetID, updatedBy, expectedVersion, false)
}

func (s *Service) setEnabled(ctx context.Context, targetID, updatedBy string, expectedVersion int64, enabled bool) (domain.AlertConfig, error) {
	if strings.TrimSpace(updatedBy) == "" {
		return domain.AlertConfig{}, fmt.Errorf("updated_by is required")
	}
	return s.configs.SetAlertConfigEnabled(ctx, targetID, enabled, strings.TrimSpace(updatedBy), expectedVersion)
}
