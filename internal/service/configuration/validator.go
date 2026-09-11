package configuration

import (
	"fmt"
	"strings"

	"crypto-price-alert/internal/domain"
)

func (s *Service) validateSymbols(symbols []string) ([]string, error) {
	if len(symbols) == 0 {
		return nil, fmt.Errorf("at least one symbol is required")
	}
	if len(symbols) > s.maxSymbols {
		return nil, fmt.Errorf("at most %d symbols are allowed", s.maxSymbols)
	}
	result := make([]string, 0, len(symbols))
	seen := make(map[string]struct{}, len(symbols))
	for _, symbol := range symbols {
		symbol = normalizeSymbol(symbol)
		if _, ok := s.allowedSymbols[symbol]; !ok {
			return nil, fmt.Errorf("unsupported symbol %q", symbol)
		}
		if _, ok := seen[symbol]; ok {
			return nil, fmt.Errorf("duplicate symbol %q", symbol)
		}
		seen[symbol] = struct{}{}
		result = append(result, symbol)
	}
	return result, nil
}

func (s *Service) validateIntervals(intervals []domain.Interval) ([]domain.Interval, error) {
	if len(intervals) == 0 {
		return nil, fmt.Errorf("at least one interval is required")
	}
	result := make([]domain.Interval, 0, len(intervals))
	seen := make(map[domain.Interval]struct{}, len(intervals))
	for _, interval := range intervals {
		if _, ok := s.allowedIntervals[interval]; !ok {
			return nil, fmt.Errorf("unsupported interval %q", interval)
		}
		if _, ok := seen[interval]; ok {
			return nil, fmt.Errorf("duplicate interval %q", interval)
		}
		seen[interval] = struct{}{}
		result = append(result, interval)
	}
	return result, nil
}

func normalizeSymbol(symbol string) string {
	return strings.ToUpper(strings.TrimSpace(symbol))
}
