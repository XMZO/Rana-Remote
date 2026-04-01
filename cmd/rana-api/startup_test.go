package main

import (
	"context"
	"testing"
	"time"

	"github.com/rana-remote/rana-remote/internal/module"
	"github.com/rana-remote/rana-remote/internal/scheduler"
	"github.com/rana-remote/rana-remote/internal/store"
)

func TestSchedulerRuntimeModuleStartIsNonBlocking(t *testing.T) {
	repo := store.NewMemoryRepository()
	runner := scheduler.NewRunner(repo, nil)
	runner.Stop(context.Background())
	runner = scheduler.NewRunner(repo, nil)

	m := module.BasicModule{
		ModuleName: "scheduler-runtime",
		OnEnabled:  true,
		StartFn: func(ctx context.Context) error {
			go func() {
				_ = runner.Start(ctx)
			}()
			return nil
		},
		StopFn: runner.Stop,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	start := time.Now()
	if err := m.Start(ctx); err != nil {
		t.Fatalf("start module: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("expected non-blocking start, took %s", elapsed)
	}

	if err := m.Stop(context.Background()); err != nil {
		t.Fatalf("stop module: %v", err)
	}
}
