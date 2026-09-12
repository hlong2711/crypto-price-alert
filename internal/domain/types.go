package domain

import (
	"fmt"
	"time"
)

type Interval string

const (
	Interval1H Interval = "1h"
	Interval4H Interval = "4h"
)

func (i Interval) Validate() error {
	if i != Interval1H && i != Interval4H {
		return fmt.Errorf("unsupported interval %q", i)
	}
	return nil
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
	Interval               Interval
	PeriodStart, PeriodEnd time.Time
	Status                 JobStatus
	ErrorMessage           string
	SentAt                 *time.Time
	CreatedAt, UpdatedAt   time.Time
}

func (j Job) Validate() error {
	if j.ID == "" || j.Symbol == "" || j.PeriodStart.IsZero() || j.PeriodEnd.IsZero() || !j.PeriodEnd.After(j.PeriodStart) {
		return fmt.Errorf("invalid job")
	}
	if err := j.Interval.Validate(); err != nil {
		return err
	}
	if j.Status != JobPending && j.Status != JobSent && j.Status != JobFailed {
		return fmt.Errorf("unsupported job status %q", j.Status)
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
	TargetID  string
	Enabled   bool
	Symbols   []string
	Intervals []Interval
	Version   int64
	UpdatedBy string
	UpdatedAt time.Time
}

func (c AlertConfig) Validate() error {
	if c.TargetID == "" || c.Version < 0 || c.UpdatedBy == "" {
		return fmt.Errorf("invalid alert config")
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
