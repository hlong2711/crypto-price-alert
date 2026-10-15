package chat

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"crypto-price-alert/internal/domain"
)

type chatFundingFunc func(context.Context, int) (domain.FundingRateResult, error)

func (f chatFundingFunc) FundingRates(ctx context.Context, n int) (domain.FundingRateResult, error) {
	return f(ctx, n)
}

func TestFundingReadOnlyDispatch(t *testing.T) {
	s, configuration, sessions, target := newChatService(t)
	target.Provider = domain.ChatProviderTelegram
	calls := 0
	now := time.Now().UTC()
	reader := chatFundingFunc(func(ctx context.Context, n int) (domain.FundingRateResult, error) {
		calls++
		if n != 10 {
			t.Error(n)
		}
		return domain.FundingRateResult{RequestedLimit: n, ReturnedCount: 1, Shortfall: true, GeneratedAt: now, Data: []domain.FundingRate{{Symbol: "BTCUSDT", BaseAsset: "BTC", QuoteAsset: "USDT", Rate: "0.0005", IntervalHours: 4, NextFundingTime: now.Add(time.Hour)}}}, nil
	})
	var err error
	s, err = NewService(configuration, CreatorAuthorizer{}, sessions, time.Minute, WithFundingReader(reader))
	if err != nil {
		t.Fatal(err)
	}
	message, err := s.Handle(context.Background(), Command{Target: target, ActorUserID: "member", Action: ActionFunding, Arguments: []string{"10"}})
	if err != nil || calls != 1 || configuration.replaceCalls != 0 || !strings.Contains(message, "+0.05%") || !strings.Contains(message, "every 4h") || !strings.Contains(message, "UTC") || !strings.Contains(message, "Only 1") {
		t.Fatal(message, err, calls)
	}
	for _, args := range [][]string{{"4"}, {"5", "10"}, {"5.0"}, {""}} {
		message, err = s.Handle(context.Background(), Command{Target: target, ActorUserID: "member", Action: ActionFunding, Arguments: args})
		if err != nil || message != fundingUsage || calls != 1 {
			t.Fatal(message, err, calls)
		}
	}
	target.Provider = domain.ChatProviderSlack
	message, _ = s.Handle(context.Background(), Command{Target: target, ActorUserID: "member", Action: ActionFunding})
	if !strings.Contains(message, "only in Telegram") || calls != 1 {
		t.Fatal(message, calls)
	}
}

func TestFundingEmptyErrorAndDisabled(t *testing.T) {
	s, _, _, target := newChatService(t)
	target.Provider = domain.ChatProviderTelegram
	command := Command{Target: target, ActorUserID: "member", Action: ActionFunding}
	message, err := s.Handle(context.Background(), command)
	if err != nil || !strings.Contains(message, "disabled") {
		t.Fatal(message, err)
	}
	s.funding = chatFundingFunc(func(context.Context, int) (domain.FundingRateResult, error) {
		return domain.FundingRateResult{}, errors.New("sensitive upstream detail")
	})
	message, err = s.Handle(context.Background(), command)
	if err != nil || message != fundingTemporary || strings.Contains(message, "sensitive") {
		t.Fatal(message, err)
	}
	s.funding = chatFundingFunc(func(context.Context, int) (domain.FundingRateResult, error) {
		return domain.FundingRateResult{RequestedLimit: 5, Shortfall: true, GeneratedAt: time.Now()}, nil
	})
	message, err = s.Handle(context.Background(), command)
	if err != nil || !strings.Contains(message, "No eligible positive-rate pairs") {
		t.Fatal(message, err)
	}
}

func TestFundingMessageBoundAndTinyRates(t *testing.T) {
	now := time.Now().UTC()
	result := domain.FundingRateResult{RequestedLimit: 20, ReturnedCount: 20, GeneratedAt: now}
	for i := 0; i < 20; i++ {
		result.Data = append(result.Data, domain.FundingRate{Symbol: strings.Repeat("币", 32), BaseAsset: "币", QuoteAsset: "USDC", Rate: "0.000000000000000000000000000000000000000000000000000000000001", IntervalHours: 24, NextFundingTime: now.Add(time.Hour)})
	}
	message, err := formatFunding(result)
	if err != nil || len(utf16.Encode([]rune(message))) > 4096 || strings.Contains(message, "| 0%") || !strings.Contains(message, "e-") {
		t.Fatal(len(message), err, message)
	}
}
