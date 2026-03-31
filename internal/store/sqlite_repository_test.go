package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func TestSQLiteRepository_BasicRoundTrip(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "rana-test.db")
	repo, err := NewSQLiteRepository(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteRepository error: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()
	u, err := repo.CreateUser(ctx, User{Username: "admin", PasswordHash: "hash", Role: RoleAdmin, Locale: "zh-CN"})
	if err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}
	if err := repo.UpdateUserLogin(ctx, u.ID, time.Now().UTC()); err != nil {
		t.Fatalf("UpdateUserLogin error: %v", err)
	}

	_, err = repo.UpsertServer(ctx, Server{
		Name:         "node1",
		Host:         "127.0.0.1",
		Port:         22,
		User:         "root",
		KeyPath:      "/tmp/id_rsa",
		Passphrase:   "secret",
		Enabled:      true,
		Paths:        []string{"/etc"},
		RcloneRemote: "s3:bucket/path",
		RcloneFlags:  []string{"--s3-chunk-size", "64M"},
	})
	if err != nil {
		t.Fatalf("UpsertServer error: %v", err)
	}
	servers, err := repo.ListServers(ctx)
	if err != nil || len(servers) != 1 {
		t.Fatalf("ListServers error=%v servers=%d", err, len(servers))
	}
	gotServer, err := repo.GetServer(ctx, servers[0].ID)
	if err != nil {
		t.Fatalf("GetServer error: %v", err)
	}
	if gotServer.Passphrase != "secret" {
		t.Fatalf("unexpected server passphrase: %q", gotServer.Passphrase)
	}
	second, err := repo.UpsertServer(ctx, Server{
		Name:         "node2",
		Host:         "127.0.0.2",
		Port:         22,
		User:         "root",
		KeyPath:      "/tmp/id_rsa",
		Enabled:      true,
		Paths:        []string{"/var/lib"},
		RcloneRemote: "s3:bucket/path2",
	})
	if err != nil {
		t.Fatalf("UpsertServer second error: %v", err)
	}
	if err := repo.DeleteServer(ctx, second.ID); err != nil {
		t.Fatalf("DeleteServer error: %v", err)
	}
	if _, err := repo.GetServer(ctx, second.ID); err != ErrNotFound {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}

	exec, err := repo.CreateExecution(ctx, Execution{Status: StatusPending, TriggerType: "manual", ServerNames: []string{"node1"}})
	if err != nil {
		t.Fatalf("CreateExecution error: %v", err)
	}
	exec.Status = StatusSuccess
	exec.EndedAt = time.Now().UTC()
	exec.DurationMS = 1234
	exec.Results = []Result{{Server: "node1", Success: true, DurationMS: 1000}}
	if err := repo.UpdateExecution(ctx, exec); err != nil {
		t.Fatalf("UpdateExecution error: %v", err)
	}
	if err := repo.AppendExecutionLog(ctx, ExecutionLog{ExecutionID: exec.ID, Level: "INFO", Line: "hello"}); err != nil {
		t.Fatalf("AppendExecutionLog error: %v", err)
	}

	logs, err := repo.ListExecutionLogs(ctx, exec.ID)
	if err != nil {
		t.Fatalf("ListExecutionLogs error: %v", err)
	}
	if len(logs) != 1 || logs[0].Line != "hello" {
		t.Fatalf("unexpected logs: %+v", logs)
	}

	items, err := repo.ListExecutions(ctx)
	if err != nil {
		t.Fatalf("ListExecutions error: %v", err)
	}
	if len(items) != 1 || items[0].ID != exec.ID {
		t.Fatalf("unexpected executions: %+v", items)
	}
}

func TestSQLiteRepository_PersistAcrossReopen(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "rana-persist.db")
	ctx := context.Background()

	first, err := NewSQLiteRepository(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteRepository(first) error: %v", err)
	}
	created, err := first.CreateUser(ctx, User{
		Username:     "persist-admin",
		PasswordHash: "hash",
		Role:         RoleAdmin,
		Locale:       "zh-CN",
	})
	if err != nil {
		t.Fatalf("CreateUser error: %v", err)
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close(first) error: %v", err)
	}

	second, err := NewSQLiteRepository(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteRepository(second) error: %v", err)
	}
	defer second.Close()

	got, err := second.GetUserByUsername(ctx, "persist-admin")
	if err != nil {
		t.Fatalf("GetUserByUsername error: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("unexpected user id after reopen: got=%s want=%s", got.ID, created.ID)
	}
}

func TestSQLiteRepository_ApplyRetention(t *testing.T) {
	repo, err := NewSQLiteRepository(filepath.Join(t.TempDir(), "rana-retention.db"))
	if err != nil {
		t.Fatalf("NewSQLiteRepository error: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()
	now := time.Now().UTC()
	oldExec, err := repo.CreateExecution(ctx, Execution{
		ID:          "old-exec",
		Status:      StatusSuccess,
		TriggerType: "manual",
		ServerNames: []string{"srv-a"},
		StartedAt:   now.Add(-48 * time.Hour),
		EndedAt:     now.Add(-47 * time.Hour),
	})
	if err != nil {
		t.Fatalf("CreateExecution(old) error: %v", err)
	}
	newExec, err := repo.CreateExecution(ctx, Execution{
		ID:          "new-exec",
		Status:      StatusSuccess,
		TriggerType: "manual",
		ServerNames: []string{"srv-b"},
		StartedAt:   now.Add(-2 * time.Hour),
		EndedAt:     now.Add(-1 * time.Hour),
	})
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
	logs, err := repo.ListExecutionLogs(ctx, newExec.ID)
	if err != nil {
		t.Fatalf("ListExecutionLogs(new) error: %v", err)
	}
	if len(logs) != 1 || logs[0].Line != "new" {
		t.Fatalf("unexpected remaining logs: %+v", logs)
	}
	audits, err := repo.ListAuditLogs(ctx)
	if err != nil {
		t.Fatalf("ListAuditLogs error: %v", err)
	}
	if len(audits) != 1 || audits[0].ID != "new-audit" {
		t.Fatalf("unexpected remaining audit logs: %+v", audits)
	}
}
