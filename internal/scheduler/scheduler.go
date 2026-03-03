package scheduler

import "context"

// Scheduler is a placeholder for cron-based jobs.
type Scheduler struct{}

func New() *Scheduler {
	return &Scheduler{}
}

func (s *Scheduler) Start(_ context.Context) error { return nil }
func (s *Scheduler) Stop(_ context.Context) error  { return nil }
