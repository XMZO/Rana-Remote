package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/config"
	i18n2 "github.com/rana-remote/rana-remote/internal/i18n"
	"github.com/rana-remote/rana-remote/internal/module"
	"github.com/rana-remote/rana-remote/internal/store"
)

func TestExecutionLogsAndStream(t *testing.T) {
	cfg := &config.Config{
		Global:  config.GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Web:     config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		I18N:    config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Modules: config.ModuleConfig{Backup: true, Audit: true, Users: true},
	}
	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("secret")
	user, _ := repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})

	exec, _ := repo.CreateExecution(t.Context(), store.Execution{Status: store.StatusRunning, TriggerType: "manual", ServerNames: []string{"node"}})
	_ = repo.AppendExecutionLog(t.Context(), store.ExecutionLog{ExecutionID: exec.ID, Level: "INFO", Line: "line one", Timestamp: time.Now().UTC()})

	bundle := i18n2.NewBundle()
	tr := i18n2.NewTranslator(bundle, cfg.I18N)
	tokens, _ := auth.NewTokenManager("test-secret", 15*time.Minute, 24*time.Hour)
	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: true})

	s := NewServer(cfg, repo, tokens, tr, registry, nil)
	h := s.Router()

	pair, _ := tokens.Generate(user.ID, string(user.Role), user.Locale, time.Now().UTC())
	authz := "Bearer " + pair.AccessToken

	logsReq := httptest.NewRequest(http.MethodGet, "/api/v1/executions/"+exec.ID+"/logs", nil)
	logsReq.Header.Set("Authorization", authz)
	logsRes := httptest.NewRecorder()
	h.ServeHTTP(logsRes, logsReq)
	if logsRes.Code != http.StatusOK {
		t.Fatalf("logs status=%d body=%s", logsRes.Code, logsRes.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(logsRes.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode logs payload: %v", err)
	}
	logs, _ := payload["data"].([]any)
	if len(logs) == 0 {
		t.Fatalf("expected logs in response")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	streamReq := httptest.NewRequest(http.MethodGet, "/api/v1/executions/"+exec.ID+"/stream", bytes.NewReader(nil)).WithContext(ctx)
	streamReq.Header.Set("Authorization", authz)
	streamRes := httptest.NewRecorder()
	h.ServeHTTP(streamRes, streamReq)
	if streamRes.Code != http.StatusOK {
		t.Fatalf("stream status=%d body=%s", streamRes.Code, streamRes.Body.String())
	}
	if !strings.Contains(streamRes.Body.String(), "event: log") {
		t.Fatalf("expected stream event output, got: %s", streamRes.Body.String())
	}
}
