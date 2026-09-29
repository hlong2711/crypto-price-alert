package domain

import (
	"fmt"
	"time"
)

type Interval string

const (
	Interval1M  Interval = "1m"
	Interval3M  Interval = "3m"
	Interval5M  Interval = "5m"
	Interval10M Interval = "10m" // should not used for kline interval param
	Interval15M Interval = "15m"
	Interval30M Interval = "30m"
	Interval1H  Interval = "1h"
	Interval2H  Interval = "2h"
	Interval4H  Interval = "4h"
	Interval6H  Interval = "6h"
	Interval8H  Interval = "8h"
	Interval12H Interval = "12h"
	Interval1D  Interval = "1d"
	Interval3D  Interval = "3d"
	Interval1W  Interval = "1w"
)

func (i Interval) Duration() (time.Duration, error) {
	switch i {
	case Interval1M:
		return time.Minute, nil
	case Interval3M:
		return 3 * time.Minute, nil
	case Interval5M:
		return 5 * time.Minute, nil
	case Interval15M:
		return 15 * time.Minute, nil
	case Interval30M:
		return 30 * time.Minute, nil
	case Interval1H:
		return time.Hour, nil
	case Interval2H:
		return 2 * time.Hour, nil
	case Interval4H:
		return 4 * time.Hour, nil
	case Interval6H:
		return 6 * time.Hour, nil
	case Interval8H:
		return 8 * time.Hour, nil
	case Interval12H:
		return 12 * time.Hour, nil
	case Interval1D:
		return 24 * time.Hour, nil
	case Interval3D:
		return 3 * 24 * time.Hour, nil
	case Interval1W:
		return 7 * 24 * time.Hour, nil
	default:
		return 0, fmt.Errorf("unsupported interval %q", i)
	}
}

func (i Interval) Validate() error {
	_, err := i.Duration()
	return err
}

type Period struct {
	Interval Interval
	Start    time.Time
	End      time.Time
}

func (p Period) Validate() error {
	if err := p.Interval.Validate(); err != nil {
		return err
	}
	if p.Start.IsZero() || p.End.IsZero() || !p.End.After(p.Start) {
		return fmt.Errorf("invalid period")
	}
	return nil
}

type Candle struct {
	Symbol                         string
	Open, High, Low, Close, Volume float64
	OpenTime, CloseTime            time.Time
}

func (c Candle) Validate() error {
	if c.Symbol == "" || c.Open <= 0 ||
		c.High <= 0 || c.Low <= 0 ||
		c.Close <= 0 ||
		c.Volume < 0 ||
		c.OpenTime.IsZero() || c.CloseTime.IsZero() || !c.CloseTime.After(c.OpenTime) ||
		c.High < c.Open || c.High < c.Close ||
		c.Low > c.Open || c.Low > c.Close ||
		c.High < c.Low {
		return fmt.Errorf("invalid candle")
	}
	return nil
}

type PriceChange struct {
	Symbol                         string
	Interval                       Interval
	Open, Close, ChangePct, Volume float64
	StartTime, EndTime             time.Time
}

type JobStatus string

const (
	JobPending JobStatus = "pending"
	JobSent    JobStatus = "sent"
	JobFailed  JobStatus = "failed"
)

type Job struct {
	ID, TargetID, Symbol   string
	MarketProvider         MarketProvider
	Interval               Interval
	PeriodStart, PeriodEnd time.Time
	Status                 JobStatus
	ErrorMessage           string
	SentAt                 *time.Time
	CreatedAt, UpdatedAt   time.Time
}

func (j Job) Validate() error {
	if j.ID == "" || j.Symbol == "" || j.MarketProvider == "" || j.PeriodStart.IsZero() || j.PeriodEnd.IsZero() || !j.PeriodEnd.After(j.PeriodStart) {
		return fmt.Errorf("invalid job")
	}
	if err := j.Interval.Validate(); err != nil {
		return err
	}
	if err := j.MarketProvider.Validate(); err != nil {
		return err
	}
	if j.Status != JobPending && j.Status != JobSent && j.Status != JobFailed {
		return fmt.Errorf("unsupported job status %q", j.Status)
	}
	return nil
}

type MarketProvider string

const (
	MarketProviderBinance       MarketProvider = "binance"
	MarketProviderCoinMarketCap MarketProvider = "coinmarketcap"
)

func (p MarketProvider) Validate() error {
	if p != MarketProviderBinance && p != MarketProviderCoinMarketCap {
		return fmt.Errorf("unsupported market provider %q", p)
	}
	return nil
}

type Message struct {
	Title  string
	Period Period
	Lines  []string
}

type ChatProvider string

const (
	ChatProviderTelegram ChatProvider = "telegram"
	ChatProviderSlack    ChatProvider = "slack"
	ChatProviderLegacy   ChatProvider = "legacy"
)

func (p ChatProvider) Validate() error {
	if p != ChatProviderTelegram && p != ChatProviderSlack && p != ChatProviderLegacy {
		return fmt.Errorf("unsupported chat provider %q", p)
	}
	return nil
}

type AlertTarget struct {
	ID             string
	Provider       ChatProvider
	TenantID       string
	ExternalChatID string
	DisplayName    string
	CreatorUserID  string
	Enabled        bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

func (t AlertTarget) Validate() error {
	if t.ID == "" || t.Provider == "" || t.TenantID == "" || t.ExternalChatID == "" || t.CreatorUserID == "" {
		return fmt.Errorf("invalid alert target")
	}
	return t.Provider.Validate()
}

type AlertConfig struct {
	TargetID       string
	MarketProvider MarketProvider
	Enabled        bool
	Symbols        []string
	Intervals      []Interval
	Version        int64
	UpdatedBy      string
	UpdatedAt      time.Time
}

func (c AlertConfig) Validate() error {
	if c.TargetID == "" || c.MarketProvider == "" || c.Version < 0 || c.UpdatedBy == "" {
		return fmt.Errorf("invalid alert config")
	}
	if err := c.MarketProvider.Validate(); err != nil {
		return err
	}
	if len(c.Symbols) == 0 || len(c.Intervals) == 0 {
		return fmt.Errorf("alert config requires symbols and intervals")
	}
	seenSymbols := make(map[string]struct{}, len(c.Symbols))
	for _, symbol := range c.Symbols {
		if symbol == "" {
			return fmt.Errorf("alert config contains an empty symbol")
		}
		if _, ok := seenSymbols[symbol]; ok {
			return fmt.Errorf("duplicate alert symbol %q", symbol)
		}
		seenSymbols[symbol] = struct{}{}
	}
	seenIntervals := make(map[Interval]struct{}, len(c.Intervals))
	for _, interval := range c.Intervals {
		if err := interval.Validate(); err != nil {
			return err
		}
		if _, ok := seenIntervals[interval]; ok {
			return fmt.Errorf("duplicate alert interval %q", interval)
		}
		seenIntervals[interval] = struct{}{}
	}
	return nil
}

type InboundEvent struct {
	ID              string
	Provider        ChatProvider
	ExternalEventID string
	Message         string
	ReceivedAt      time.Time
	ProcessedAt     *time.Time
	Status          string
	ErrorMessage    string
}

func (e InboundEvent) Validate() error {
	if e.ID == "" || e.ExternalEventID == "" || e.ReceivedAt.IsZero() || e.Status == "" {
		return fmt.Errorf("invalid inbound event")
	}
	return e.Provider.Validate()
}
