package scheduler

import (
	"context"
	"fmt"
	"strings"
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
	periods, err := NewPeriodEngine(location, "06:00", "23:00")
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
	executor, err := NewTargetExecutor(periods, newCountingMarket(), jobs, targets, configs, []notification.Notifier{notifier}, nil, location)
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
	periods, _ := NewPeriodEngine(location, "06:00", "23:00")
	targets := targetRepo{values: []domain.AlertTarget{{ID: "paused", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat", Enabled: true}}}
	configs := configRepo{values: map[string]domain.AlertConfig{"paused": {TargetID: "paused", Enabled: false, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}}}}
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, newCountingMarket(), newJobRepo(), targets, configs, []notification.Notifier{notifier}, nil, location)
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

func TestTargetExecutorFetchesSharedSymbolOnce(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	periods, _ := NewPeriodEngine(location, "06:00", "23:00")
	targets := targetRepo{values: []domain.AlertTarget{
		{ID: "target-a", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-a", Enabled: true},
		{ID: "target-b", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-b", Enabled: true},
		{ID: "target-c", Provider: domain.ChatProviderSlack, ExternalChatID: "channel-c", Enabled: true},
	}}
	configs := configRepo{values: map[string]domain.AlertConfig{
		"target-a": {TargetID: "target-a", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
		"target-b": {TargetID: "target-b", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
		"target-c": {TargetID: "target-c", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
	}}
	jobs := newJobRepo()
	market := newCountingMarket()
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, market, jobs, targets, configs, []notification.Notifier{notifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), time.Date(2026, 9, 1, 10, 0, 0, 0, location), domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if got := market.callCount("BTCUSDT"); got != 1 {
		t.Fatalf("expected 1 shared fetch for BTCUSDT, got %d", got)
	}
	if len(notifier.targets) != 3 {
		t.Fatalf("expected 3 deliveries, got %v", notifier.targets)
	}
	if len(jobs.sent) != 3 {
		t.Fatalf("expected 3 sent jobs, got %+v", jobs.sent)
	}
}

func TestTargetExecutorFetchesUnionOfSymbolsOnce(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	periods, _ := NewPeriodEngine(location, "06:00", "23:00")
	targets := targetRepo{values: []domain.AlertTarget{
		{ID: "target-a", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-a", Enabled: true},
		{ID: "target-b", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-b", Enabled: true},
	}}
	configs := configRepo{values: map[string]domain.AlertConfig{
		"target-a": {TargetID: "target-a", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT", "ETHUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
		"target-b": {TargetID: "target-b", Enabled: true, Version: 1, Symbols: []string{"ETHUSDT", "SOLUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
	}}
	jobs := newJobRepo()
	market := newCountingMarket()
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, market, jobs, targets, configs, []notification.Notifier{notifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), time.Date(2026, 9, 1, 10, 0, 0, 0, location), domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if got := market.totalCalls(); got != 3 {
		t.Fatalf("expected 3 unique-symbol fetches, got %d", got)
	}
	if len(notifier.targets) != 2 {
		t.Fatalf("expected 2 deliveries, got %v", notifier.targets)
	}
	msgA := messageText(notifier.messageFor("chat-a"))
	if !strings.Contains(msgA, "BTCUSDT") || !strings.Contains(msgA, "ETHUSDT") || strings.Contains(msgA, "SOLUSDT") {
		t.Fatalf("unexpected message for chat-a: %q", msgA)
	}
	msgB := messageText(notifier.messageFor("chat-b"))
	if !strings.Contains(msgB, "ETHUSDT") || !strings.Contains(msgB, "SOLUSDT") || strings.Contains(msgB, "BTCUSDT") {
		t.Fatalf("unexpected message for chat-b: %q", msgB)
	}
}

func TestTargetExecutorSharedFetchFailureMarksAllSharersUnavailable(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	periods, _ := NewPeriodEngine(location, "06:00", "23:00")
	targets := targetRepo{values: []domain.AlertTarget{
		{ID: "target-a", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-a", Enabled: true},
		{ID: "target-b", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-b", Enabled: true},
	}}
	configs := configRepo{values: map[string]domain.AlertConfig{
		"target-a": {TargetID: "target-a", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT", "ETHUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
		"target-b": {TargetID: "target-b", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT", "ETHUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
	}}
	jobs := newJobRepo()
	market := newCountingMarket()
	market.fail = map[string]error{"ETHUSDT": fmt.Errorf("boom")}
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, market, jobs, targets, configs, []notification.Notifier{notifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), time.Date(2026, 9, 1, 10, 0, 0, 0, location), domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if got := market.callCount("ETHUSDT"); got != 1 {
		t.Fatalf("expected 1 shared failing fetch for ETHUSDT, got %d", got)
	}
	if len(notifier.targets) != 2 {
		t.Fatalf("expected both sharers still delivered, got %v", notifier.targets)
	}
	for _, chatID := range []string{"chat-a", "chat-b"} {
		text := messageText(notifier.messageFor(chatID))
		if !strings.Contains(text, "BTCUSDT") {
			t.Fatalf("expected BTCUSDT line for %s, got %q", chatID, text)
		}
		if !strings.Contains(text, "ETHUSDT   unavailable") {
			t.Fatalf("expected ETHUSDT unavailable for %s, got %q", chatID, text)
		}
	}
}

func TestTargetExecutorDuplicateTickFetchesNothing(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	periods, _ := NewPeriodEngine(location, "06:00", "23:00")
	targets := targetRepo{values: []domain.AlertTarget{
		{ID: "target-a", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-a", Enabled: true},
		{ID: "target-b", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-b", Enabled: true},
	}}
	configs := configRepo{values: map[string]domain.AlertConfig{
		"target-a": {TargetID: "target-a", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
		"target-b": {TargetID: "target-b", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
	}}
	jobs := newJobRepo()
	market := newCountingMarket()
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, market, jobs, targets, configs, []notification.Notifier{notifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, location)
	if err := executor.Execute(context.Background(), now, domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), now, domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if got := market.totalCalls(); got != 1 {
		t.Fatalf("expected 1 fetch total across duplicate ticks, got %d", got)
	}
	if len(notifier.targets) != 2 {
		t.Fatalf("duplicate tick delivered again: %v", notifier.targets)
	}
}

func TestTargetExecutorSkipsSentTargetButFetchesFreshTarget(t *testing.T) {
	location, _ := time.LoadLocation("Asia/Ho_Chi_Minh")
	periods, _ := NewPeriodEngine(location, "06:00", "23:00")
	now := time.Date(2026, 9, 1, 10, 0, 0, 0, location)
	jobs := newJobRepo()
	market := newCountingMarket()

	firstTargets := targetRepo{values: []domain.AlertTarget{
		{ID: "target-a", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-a", Enabled: true},
	}}
	firstConfigs := configRepo{values: map[string]domain.AlertConfig{
		"target-a": {TargetID: "target-a", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
	}}
	firstNotifier := &targetCaptureNotifier{}
	first, err := NewTargetExecutor(periods, market, jobs, firstTargets, firstConfigs, []notification.Notifier{firstNotifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := first.Execute(context.Background(), now, domain.Interval1H); err != nil {
		t.Fatal(err)
	}

	targets := targetRepo{values: []domain.AlertTarget{
		{ID: "target-a", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-a", Enabled: true},
		{ID: "target-b", Provider: domain.ChatProviderTelegram, ExternalChatID: "chat-b", Enabled: true},
	}}
	configs := configRepo{values: map[string]domain.AlertConfig{
		"target-a": {TargetID: "target-a", Enabled: true, Version: 1, Symbols: []string{"BTCUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
		"target-b": {TargetID: "target-b", Enabled: true, Version: 1, Symbols: []string{"ETHUSDT"}, Intervals: []domain.Interval{domain.Interval1H}},
	}}
	notifier := &targetCaptureNotifier{}
	executor, err := NewTargetExecutor(periods, market, jobs, targets, configs, []notification.Notifier{notifier}, nil, location)
	if err != nil {
		t.Fatal(err)
	}
	if err := executor.Execute(context.Background(), now, domain.Interval1H); err != nil {
		t.Fatal(err)
	}
	if got := market.callCount("BTCUSDT"); got != 1 {
		t.Fatalf("expected no refetch for sent BTCUSDT, got %d calls", got)
	}
	if got := market.callCount("ETHUSDT"); got != 1 {
		t.Fatalf("expected 1 fetch for fresh ETHUSDT, got %d", got)
	}
	if len(notifier.targets) != 1 || notifier.targets[0] != "chat-b" {
		t.Fatalf("expected only fresh target delivered, got %v", notifier.targets)
	}
}

type targetRepo struct{ values []domain.AlertTarget }

func (r targetRepo) ListEnabledTargets(context.Context) ([]domain.AlertTarget, error) {
	return r.values, nil
}

func (r targetRepo) FindOrCreateTarget(ctx context.Context, target domain.AlertTarget) (domain.AlertTarget, error) {
	panic("not implemented") // TODO: Implement
}

func (r targetRepo) GetTarget(ctx context.Context, provider domain.ChatProvider, tenantID string, externalChatID string) (domain.AlertTarget, error) {
	panic("not implemented") // TODO: Implement
}

func (r targetRepo) CountTargets(ctx context.Context) (int64, error) {
	panic("not implemented") // TODO: Implement
}

type configRepo struct{ values map[string]domain.AlertConfig }

func (r configRepo) CreateAlertConfig(ctx context.Context, config domain.AlertConfig) error {
	panic("not implemented") // TODO: Implement
}

func (r configRepo) ReplaceAlertConfig(ctx context.Context, config domain.AlertConfig, expectedVersion int64) (domain.AlertConfig, error) {
	panic("not implemented") // TODO: Implement
}

func (r configRepo) SetAlertConfigEnabled(ctx context.Context, targetID string, enabled bool, updatedBy string, expectedVersion int64) (domain.AlertConfig, error) {
	panic("not implemented") // TODO: Implement
}

func (r configRepo) GetAlertConfig(_ context.Context, targetID string) (domain.AlertConfig, error) {
	value, ok := r.values[targetID]
	if !ok {
		return domain.AlertConfig{}, fmt.Errorf("missing config")
	}
	return value, nil
}

type fakeMarket struct {
	mu    sync.Mutex
	calls map[string]int
	fail  map[string]error
}

func newCountingMarket() *fakeMarket { return &fakeMarket{calls: make(map[string]int)} }

func (m *fakeMarket) GetKline(_ context.Context, symbol string, _ domain.Interval, start, end time.Time) (domain.Candle, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.calls == nil {
		m.calls = make(map[string]int)
	}
	m.calls[symbol]++
	if err, ok := m.fail[symbol]; ok {
		return domain.Candle{}, err
	}
	return domain.Candle{Symbol: symbol, Open: 100, High: 110, Low: 90, Close: 105, Volume: 10, OpenTime: start, CloseTime: end}, nil
}

func (m *fakeMarket) callCount(symbol string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.calls[symbol]
}

func (m *fakeMarket) totalCalls() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	total := 0
	for _, n := range m.calls {
		total += n
	}
	return total
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
	mu       sync.Mutex
	targets  []string
	messages []domain.Message
}

func (n *targetCaptureNotifier) Send(context.Context, domain.Message) error { return nil }
func (n *targetCaptureNotifier) SendToTarget(_ context.Context, target domain.AlertTarget, message domain.Message) error {
	n.mu.Lock()
	defer n.mu.Unlock()
	n.targets = append(n.targets, target.ExternalChatID)
	n.messages = append(n.messages, message)
	return nil
}

func (n *targetCaptureNotifier) messageFor(chatID string) domain.Message {
	n.mu.Lock()
	defer n.mu.Unlock()
	for i, target := range n.targets {
		if target == chatID {
			return n.messages[i]
		}
	}
	return domain.Message{}
}

func messageText(message domain.Message) string {
	return strings.Join(message.Lines, "\n")
}
