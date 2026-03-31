package store

import (
	"context"
	"testing"
	"time"
)

func TestMemoryRepositoryUserRoundTrip(t *testing.T) {
	repo := NewMemoryRepository()
	created, err := repo.CreateUser(context.Background(), User{Username: "admin", Role: RoleAdmin})
	if err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}
	got, err := repo.GetUserByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("GetUserByUsername error: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("unexpected user id")
	}
}

func TestMemoryRepository_ApplyRetention(t *testing.T) {
	repo := NewMemoryRepository()
	ctx := context.Background()
	now := time.Now().UTC()

	oldExec, err := repo.CreateExecution(ctx, Execution{ID: "old", Status: StatusSuccess, TriggerType: "manual", StartedAt: now.Add(-48 * time.Hour)})
	if err != nil {
		t.Fatalf("CreateExecution(old) error: %v", err)
	}
	newExec, err := repo.CreateExecution(ctx, Execution{ID: "new", Status: StatusSuccess, TriggerType: "manual", StartedAt: now.Add(-2 * time.Hour)})
	if err != nil {
		t.Fatalf("CreateExecution(new) error: %v", err)
	}
	if err := repo.AppendExecutionLog(ctx, ExecutionLog{ExecutionID: oldExec.ID, Timestamp: now.Add(-48 * time.Hour), Level: "info", Line: "old"}); err != nil {
		t.Fatalf("AppendExecutionLog(old) error: %v", err)
	}
	if err := repo.AppendExecutionLog(ctx, ExecutionLog{ExecutionID: newExec.ID, Timestamp: now.Add(-2 * time.Hour), Level: "info", Line: "new"}); err != nil {
		t.Fatalf("AppendExecutionLog(new) error: %v", err)
	}
	if _, err := repo.CreateAuditLog(ctx, AuditLog{ID: "old-audit", ActorID: "admin", Action: "old", ResourceType: "settings", ResourceID: "global", Timestamp: now.Add(-96 * time.Hour)}); err != nil {
		t.Fatalf("CreateAuditLog(old) error: %v", err)
	}
	if _, err := repo.CreateAuditLog(ctx, AuditLog{ID: "new-audit", ActorID: "admin", Action: "new", ResourceType: "settings", ResourceID: "global", Timestamp: now.Add(-1 * time.Hour)}); err != nil {
		t.Fatalf("CreateAuditLog(new) error: %v", err)
	}

	report, err := repo.ApplyRetention(ctx, now.Add(-24*time.Hour), now.Add(-24*time.Hour), now.Add(-24*time.Hour))
	if err != nil {
		t.Fatalf("ApplyRetention error: %v", err)
	}
	if report.Executions != 1 || report.AuditLogs != 1 {
		t.Fatalf("unexpected retention report: %+v", report)
	}
	if _, err := repo.GetExecution(ctx, oldExec.ID); err != ErrNotFound {
		t.Fatalf("expected old execution removed, got %v", err)
	}
	if _, err := repo.GetExecution(ctx, newExec.ID); err != nil {
		t.Fatalf("expected new execution kept, got %v", err)
	}
}
