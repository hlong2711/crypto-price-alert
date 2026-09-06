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
	ID, Symbol             string
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
