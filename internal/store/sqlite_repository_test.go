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

func TestSQLiteRepository_SettingsRoundTrip(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "rana-settings-test.db")
	repo, err := NewSQLiteRepository(dsn)
	if err != nil {
		t.Fatalf("NewSQLiteRepository error: %v", err)
	}
	defer repo.Close()

	ctx := context.Background()

	settings, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings error: %v", err)
	}
	if !settings.UpdatedAt.IsZero() {
		t.Fatalf("expected zero UpdatedAt for initial settings, got %v", settings.UpdatedAt)
	}

	initialUpdatedAt := time.Now().UTC().Truncate(time.Millisecond)
	initialSettings := SystemSettings{
		GlobalTimeout:           "2m",
		GlobalConcurrency:       5,
		GlobalSSHStrictHostKey:  true,
		GlobalSSHKnownHostsPath: "/custom/path",
		WebCSRFEnabled:          false,
		WebCORSAllowOrigins:     []string{"http://example.com"},
		WebIPAllowList:          []string{"192.168.1.0/24"},
		ModulesSchedule:         true,
		ModulesAudit:            false,
		ModulesNotify:           true,
		ModulesUsers:            true,
		I18NDefaultLocale:       "en-US",
		NotifyWebhookURL:        "https://example.com/hook",
		NotifyEmailEnabled:      true,
		NotifyEmailSMTPHost:     "smtp.example.com",
		NotifyEmailSMTPPort:     465,
		NotifyEmailUsername:     "user",
		NotifyEmailPassword:     "pass",
		NotifyEmailFrom:         "from@example.com",
		NotifyEmailTo:           []string{"to@example.com"},
		NotifyEmailUseTLS:       true,
		NotifyOnSuccess:         true,
		NotifyOnFailure:         false,
		NotifySuppressionWindow: "5m",
		UpdatedAt:               initialUpdatedAt,
	}
	if err := repo.SaveSettings(ctx, initialSettings); err != nil {
		t.Fatalf("SaveSettings error: %v", err)
	}

	got, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings after save error: %v", err)
	}
	if got.GlobalTimeout != "2m" {
		t.Fatalf("expected GlobalTimeout 2m, got %s", got.GlobalTimeout)
	}
	if got.GlobalConcurrency != 5 {
		t.Fatalf("expected GlobalConcurrency 5, got %d", got.GlobalConcurrency)
	}
	if !got.GlobalSSHStrictHostKey {
		t.Fatalf("expected GlobalSSHStrictHostKey true")
	}
	if got.WebCSRFEnabled {
		t.Fatalf("expected WebCSRFEnabled false")
	}
	if len(got.WebCORSAllowOrigins) != 1 || got.WebCORSAllowOrigins[0] != "http://example.com" {
		t.Fatalf("expected WebCORSAllowOrigins [http://example.com], got %v", got.WebCORSAllowOrigins)
	}
	if !got.ModulesSchedule {
		t.Fatalf("expected ModulesSchedule true")
	}
	if got.ModulesAudit {
		t.Fatalf("expected ModulesAudit false")
	}
	if got.I18NDefaultLocale != "en-US" {
		t.Fatalf("expected I18NDefaultLocale en-US, got %s", got.I18NDefaultLocale)
	}
	if got.NotifyWebhookURL != "https://example.com/hook" {
		t.Fatalf("expected NotifyWebhookURL, got %s", got.NotifyWebhookURL)
	}
	if !got.NotifyEmailEnabled {
		t.Fatalf("expected NotifyEmailEnabled true")
	}
	if got.NotifyEmailSMTPPort != 465 {
		t.Fatalf("expected NotifyEmailSMTPPort 465, got %d", got.NotifyEmailSMTPPort)
	}
	if len(got.NotifyEmailTo) != 1 || got.NotifyEmailTo[0] != "to@example.com" {
		t.Fatalf("expected NotifyEmailTo [to@example.com], got %v", got.NotifyEmailTo)
	}
	if !got.NotifyOnSuccess {
		t.Fatalf("expected NotifyOnSuccess true")
	}
	if got.NotifyOnFailure {
		t.Fatalf("expected NotifyOnFailure false")
	}
	if got.NotifySuppressionWindow != "5m" {
		t.Fatalf("expected NotifySuppressionWindow 5m, got %s", got.NotifySuppressionWindow)
	}
	if got.UpdatedAt.IsZero() {
		t.Fatalf("expected UpdatedAt to be set")
	}

	updatedUpdatedAt := time.Now().UTC().Add(time.Second).Truncate(time.Millisecond)
	updatedSettings := SystemSettings{
		GlobalTimeout:   "10m",
		ModulesSchedule: false,
		NotifyOnFailure: true,
		UpdatedAt:       updatedUpdatedAt,
	}
	if err := repo.SaveSettings(ctx, updatedSettings); err != nil {
		t.Fatalf("SaveSettings(update) error: %v", err)
	}

	got2, err := repo.GetSettings(ctx)
	if err != nil {
		t.Fatalf("GetSettings after update error: %v", err)
	}
	if got2.GlobalTimeout != "10m" {
		t.Fatalf("expected GlobalTimeout 10m after update, got %s", got2.GlobalTimeout)
	}
	if got2.ModulesSchedule {
		t.Fatalf("expected ModulesSchedule false after update")
	}
	if !got2.NotifyOnFailure {
		t.Fatalf("expected NotifyOnFailure true after update")
	}
	if got2.GlobalConcurrency != 0 {
		t.Fatalf("expected GlobalConcurrency reset to 0, got %d", got2.GlobalConcurrency)
	}
	if got2.GlobalSSHKnownHostsPath != "" {
		t.Fatalf("expected GlobalSSHKnownHostsPath reset to empty, got %q", got2.GlobalSSHKnownHostsPath)
	}
	if got2.NotifyEmailEnabled {
		t.Fatalf("expected NotifyEmailEnabled reset to false")
	}
	if len(got2.NotifyEmailTo) != 0 {
		t.Fatalf("expected NotifyEmailTo reset to empty, got %v", got2.NotifyEmailTo)
	}
	if len(got2.WebCORSAllowOrigins) != 0 {
		t.Fatalf("expected WebCORSAllowOrigins reset to empty, got %v", got2.WebCORSAllowOrigins)
	}
	if !got2.UpdatedAt.Equal(updatedUpdatedAt) {
		t.Fatalf("expected UpdatedAt %v, got %v", updatedUpdatedAt, got2.UpdatedAt)
	}
}
