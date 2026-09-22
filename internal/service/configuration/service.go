package configuration

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/market"
	"crypto-price-alert/internal/repository"
)

type Service struct {
	targets          repository.AlertTargetRepository
	configs          repository.AlertConfigRepository
	defaultSymbols   []string
	defaultIntervals []domain.Interval
	allowedSymbols   map[string]struct{}
	allowedIntervals map[domain.Interval]struct{}
	maxSymbols       int
	maxTargets       int
	providers        market.ProviderResolver
	defaultProvider  domain.MarketProvider
}

func NewService(
	targets repository.AlertTargetRepository,
	configs repository.AlertConfigRepository,
	allowedSymbols []string,
	allowedIntervals []domain.Interval,
	maxSymbols int,
	maxTargets int,
	providers market.ProviderResolver,
	defaultProvider domain.MarketProvider,
) (*Service, error) {
	if targets == nil || configs == nil || len(allowedSymbols) == 0 || len(allowedIntervals) == 0 || maxSymbols < 1 || maxTargets < 1 || providers == nil {
		return nil, fmt.Errorf("invalid configuration service settings")
	}
	if _, err := providers.Get(defaultProvider); err != nil {
		return nil, fmt.Errorf("default market provider: %w", err)
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
		defaultSymbols:   append([]string(nil), allowedSymbols...),
		defaultIntervals: append([]domain.Interval(nil), allowedIntervals...),
		allowedSymbols:   symbols,
		allowedIntervals: intervals,
		maxSymbols:       maxSymbols,
		maxTargets:       maxTargets,
		providers:        providers,
		defaultProvider:  defaultProvider,
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

// GetOrCreateConfig returns an existing configuration or creates a disabled
// first-time configuration using the first allowed symbol and interval.
func (s *Service) GetOrCreateConfig(ctx context.Context, targetID, updatedBy string) (domain.AlertConfig, error) {
	config, err := s.GetConfig(ctx, targetID)
	if err == nil {
		config.MarketProvider = s.effectiveProvider(config.MarketProvider)
		return config, nil
	}
	if !errors.Is(err, repository.ErrAlertConfigNotFound) {
		return domain.AlertConfig{}, err
	}
	if err := s.CreateConfig(ctx, targetID, s.defaultProvider, []string{s.defaultSymbols[0]}, []domain.Interval{s.defaultIntervals[0]}, false, updatedBy); err != nil {
		// Another concurrent configure request may have created it first.
		if existing, getErr := s.GetConfig(ctx, targetID); getErr == nil {
			existing.MarketProvider = s.effectiveProvider(existing.MarketProvider)
			return existing, nil
		}
		return domain.AlertConfig{}, err
	}
	return s.GetConfig(ctx, targetID)
}

func (s *Service) ReplaceConfig(
	ctx context.Context,
	targetID string,
	provider domain.MarketProvider,
	symbols []string,
	intervals []domain.Interval,
	enabled bool,
	updatedBy string,
	expectedVersion int64,
) (domain.AlertConfig, error) {
	provider = s.effectiveProvider(provider)
	normalizedSymbols, err := s.validateSymbols(symbols)
	if err != nil {
		return domain.AlertConfig{}, err
	}
	normalizedIntervals, err := s.validateIntervals(intervals)
	if err != nil {
		return domain.AlertConfig{}, err
	}
	if err := s.validateProviderSelection(provider, normalizedSymbols, normalizedIntervals); err != nil {
		return domain.AlertConfig{}, err
	}
	if strings.TrimSpace(updatedBy) == "" {
		return domain.AlertConfig{}, fmt.Errorf("updated_by is required")
	}
	return s.configs.ReplaceAlertConfig(ctx, domain.AlertConfig{
		TargetID:       targetID,
		MarketProvider: provider,
		Enabled:        enabled,
		Symbols:        normalizedSymbols,
		Intervals:      normalizedIntervals,
		Version:        expectedVersion,
		UpdatedBy:      strings.TrimSpace(updatedBy),
	}, expectedVersion)
}

func (s *Service) CreateConfig(ctx context.Context, targetID string, provider domain.MarketProvider, symbols []string, intervals []domain.Interval, enabled bool, updatedBy string) error {
	provider = s.effectiveProvider(provider)
	normalizedSymbols, err := s.validateSymbols(symbols)
	if err != nil {
		return err
	}
	normalizedIntervals, err := s.validateIntervals(intervals)
	if err != nil {
		return err
	}
	if err := s.validateProviderSelection(provider, normalizedSymbols, normalizedIntervals); err != nil {
		return err
	}
	if strings.TrimSpace(updatedBy) == "" {
		return fmt.Errorf("updated_by is required")
	}
	return s.configs.CreateAlertConfig(ctx, domain.AlertConfig{
		TargetID:       targetID,
		MarketProvider: provider,
		Enabled:        enabled,
		Symbols:        normalizedSymbols,
		Intervals:      normalizedIntervals,
		Version:        0,
		UpdatedBy:      strings.TrimSpace(updatedBy),
	})
}

func (s *Service) EnabledMarketProviders() []domain.MarketProvider {
	return s.providers.Enabled()
}

func (s *Service) effectiveProvider(provider domain.MarketProvider) domain.MarketProvider {
	if _, err := s.providers.Get(provider); err == nil {
		return provider
	}
	return s.defaultProvider
}

func (s *Service) validateProviderSelection(provider domain.MarketProvider, symbols []string, intervals []domain.Interval) error {
	if _, err := s.providers.Get(provider); err != nil {
		return err
	}
	for _, symbol := range symbols {
		for _, interval := range intervals {
			if !s.providers.Supports(provider, symbol, interval) {
				return fmt.Errorf("market provider %q does not support %s/%s", provider, symbol, interval)
			}
		}
	}
	return nil
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
