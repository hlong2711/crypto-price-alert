package scheduler

import (
	"context"
	"fmt"
	"time"

	"crypto-price-alert/internal/domain"

	"github.com/robfig/cron/v3"
)

type Scheduler struct {
	cron     *cron.Cron
	executor *Executor
}

func NewScheduler(location *time.Location, executor *Executor) (*Scheduler, error) {
	if location == nil || executor == nil {
		return nil, fmt.Errorf("invalid scheduler settings")
	}
	return &Scheduler{
		cron:     cron.New(cron.WithLocation(location)),
		executor: executor}, nil
}

func (s *Scheduler) Start() {
	_, _ = s.cron.AddFunc("0 * * * *",
		func() {
			_ = s.executor.Execute(context.Background(), time.Now(), domain.Interval1H)
		})
	_, _ = s.cron.AddFunc("0 11,15,19,23 * * *",
		func() {
			_ = s.executor.Execute(context.Background(), time.Now(), domain.Interval4H)
		})
	s.cron.Start()
}

func (s *Scheduler) Stop() context.Context {
	return s.cron.Stop()
}
