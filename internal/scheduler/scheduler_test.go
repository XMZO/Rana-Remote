package scheduler

import (
	"context"
	"testing"
	"time"

	"github.com/rana-remote/rana-remote/internal/store"
)

func TestNextRunAtDuration(t *testing.T) {
	base := time.Date(2026, 3, 4, 10, 0, 0, 0, time.UTC)
	next, err := NextRunAt("@every 5m", base, "")
	if err != nil {
		t.Fatalf("NextRunAt error: %v", err)
	}
	if !next.Equal(base.Add(5 * time.Minute)) {
		t.Fatalf("unexpected next: %s", next)
	}
}

func TestNextRunAtCronSubset(t *testing.T) {
	base := time.Date(2026, 3, 4, 10, 3, 0, 0, time.UTC)
	next, err := NextRunAt("*/5 * * * *", base, "")
	if err != nil {
		t.Fatalf("NextRunAt error: %v", err)
	}
	want := time.Date(2026, 3, 4, 10, 5, 0, 0, time.UTC)
	if !next.Equal(want) {
		t.Fatalf("next=%s want=%s", next, want)
	}
}

func TestRunnerTick(t *testing.T) {
	repo := store.NewMemoryRepository()
	_, err := repo.CreateSchedule(context.Background(), store.Schedule{
		Name:          "quick",
		ServerNames:   []string{"node1"},
		CronExpr:      "@every 1m",
		Enabled:       true,
		MisfirePolicy: "run_once",
	})
	if err != nil {
		t.Fatalf("CreateSchedule: %v", err)
	}

	triggered := 0
	runner := NewRunner(repo, func(_ context.Context, _ store.Schedule) error {
		triggered++
		return nil
	})

	now := time.Now().UTC()
	if err := runner.tick(context.Background(), now); err != nil {
		t.Fatalf("tick1 error: %v", err)
	}
	schedules, _ := repo.ListSchedules(context.Background())
	if schedules[0].NextRunAt.IsZero() {
		t.Fatalf("expected next_run_at to be populated")
	}

	schedules[0].NextRunAt = now.Add(-time.Second)
	if err := repo.UpdateSchedule(context.Background(), schedules[0]); err != nil {
		t.Fatalf("UpdateSchedule: %v", err)
	}
	if err := runner.tick(context.Background(), now); err != nil {
		t.Fatalf("tick2 error: %v", err)
	}
	if triggered == 0 {
		t.Fatalf("expected trigger call")
	}
}

func TestRunnerStartIsStoppableWithoutBlockingCallerForever(t *testing.T) {
	repo := store.NewMemoryRepository()
	runner := NewRunner(repo, nil)
	runner.pollEvery = 10 * time.Millisecond

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- runner.Start(ctx)
	}()

	select {
	case <-time.After(50 * time.Millisecond):
		if err := runner.Stop(context.Background()); err != nil {
			t.Fatalf("Stop error: %v", err)
		}
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned unexpected error early: %v", err)
		}
		return
	}

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Start returned error after stop: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("runner did not stop in time")
	}
}
