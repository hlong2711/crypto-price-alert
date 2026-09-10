package scheduler

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"crypto-price-alert/internal/domain"
	"crypto-price-alert/internal/notification"
)

func TestTargetExecutorUsesLatestPerTargetConfiguration(t *testing.T) {
	location, err := time.LoadLocation("Asia/Ho_Chi_Minh")
	if err != nil {
		t.Fatal(err)
	}
	periods, err := NewPeriodEngine(location)
	if err != nil {
		t.Fatal(err)
	}
	targets := targetRepo{values: []domain.AlertTarget{
		{ID: "target-a", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-a", Enabled: true},
		{ID: "target-b", Provider: domain.ChatProviderSlack, ExternalChatID: "channel-b", Enabled: true},
	}}
	configs := configRepo{values: map[string]domain.AlertConfig{
		"target-a": {TargetID: "target-a", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
		"target-b": {TargetID: "target-b", Enabled: true, Version: 1, Symbols: []string{"ETHUSDT"}, Intervals: []domain.Interval{domain.Interval4H}},
	}}
	jobs := newJobRepo()
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, fakeMarket{}, jobs, targets, configs, []notification.Notifier{notifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, location)
	if err := executor.Execute(context.Background(), now, domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if len(notifier.targets) != 1 || notifier.targets[0] != "chat-a" {
		t.Fatalf("unexpected target deliveries: %v", notifier.targets)
	}
	if len(jobs.sent) != 1 || jobs.sent[0].TargetID != "target-a" {
		t.Fatalf("unexpected sent jobs: %+v", jobs.sent)
	}

	// A duplicate coordinator tick sees the sent target job and does not deliver twice.
	if err := executor.Execute(context.Background(), now, domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if len(notifier.targets) != 1 {
		t.Fatalf("duplicate tick delivered again: %v", notifier.targets)
	}
}

func TestTargetExecutorSkipsPausedTargets(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	periods, _ := NewPeriodEngine(location)
	targets := targetRepo{values: []domain.AlertTarget{{ID: "paused", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat", Enabled: true}}}
	configs := configRepo{values: map[string]domain.AlertConfig{"paused": {TargetID: "paused", Enabled: false, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}}}}
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, fakeMarket{}, newJobRepo(), targets, configs, []notification.Notifier{notifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), time.Date(2026, 9, 1, 10, 0, 0, 0, location), domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if len(notifier.targets) != 0 {
		t.Fatalf("paused target was delivered: %v", notifier.targets)
	}
}

type targetRepo struct{ values []domain.AlertTarget }

func (r targetRepo) ListEnabledTargets(context.Context) ([]domain.AlertTarget, error) {
	return r.values, nil
}

type configRepo struct{ values map[string]domain.AlertConfig }

func (r configRepo) GetAlertConfig(_ context.Context, targetID string) (domain.AlertConfig, error) {
	value, ok := r.values[targetID]
	if !ok {
		return domain.AlertConfig{}, fmt.Errorf("missing config")
	}
	return value, nil
}

type fakeMarket struct{}

func (fakeMarket) GetKline(_ context.Context, symbol string, _ domain.Interval, start, end time.Time) (domain.Candle, error) {
	return domain.Candle{Symbol: symbol, Open: 100, High: 110, Low: 90, Close: 105, Volume: 10, OpenTime: start, CloseTime: end}, nil
}

type jobRepo struct {
	mu   sync.Mutex
	jobs map[string]domain.Job
	sent []domain.Job
}

func newJobRepo() *jobRepo { return &jobRepo{jobs: make(map[string]domain.Job)} }
func (r *jobRepo) CreateIfNotExists(_ context.Context, job domain.Job) (domain.Job, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := job.TargetID + ":" + job.Symbol + ":" + string(job.Interval) + ":" + job.PeriodStart.String()
	if existing, ok := r.jobs[key]; ok {
		return existing, false, nil
	}
	r.jobs[key] = job
	return job, true, nil
}
func (r *jobRepo) MarkSent(_ context.Context, id string, sentAt time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, job := range r.jobs {
		if job.ID == id {
			job.Status = domain.JobSent
			job.SentAt = &sentAt
			r.jobs[key] = job
			r.sent = append(r.sent, job)
		}
	}
	return nil
}
func (r *jobRepo) MarkFailed(_ context.Context, id, reason string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, job := range r.jobs {
		if job.ID == id {
			job.Status = domain.JobFailed
			job.ErrorMessage = reason
			r.jobs[key] = job
		}
	}
	return nil
}

type targetCaptureNotifier struct {
	mu      sync.Mutex
	targets []string
}

func (n *targetCaptureNotifier) Send(context.Context, domain.Message) error { return nil }
func (n *targetCaptureNotifier) SendToTarget(_ context.Context, target domain.AlertTarget, _ domain.Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.targets = append(n.targets, target.ExternalChatID)
	return nil
}
