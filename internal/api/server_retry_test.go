package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/config"
	i18n2 "github.com/rana-remote/rana-remote/internal/i18n"
	"github.com/rana-remote/rana-remote/internal/module"
	"github.com/rana-remote/rana-remote/internal/store"
)

type noopExecutor struct{}

func (noopExecutor) Execute(_ context.Context, _ config.Server, _ string, _ io.Writer, _ io.Writer) error {
	return nil
}

func TestServerCRUD(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Timeout: "1m",
			TempDir: "/tmp",
			SSH:     config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"},
		},
		Web:  config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		I18N: config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Modules: config.ModuleConfig{
			Backup: true,
			Audit:  true,
			Users:  true,
		},
	}

	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("secret")
	user, _ := repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})
	_, _ = repo.UpsertServer(t.Context(), store.Server{Name: "seed", Host: "127.0.0.1", Port: 22, User: "root", KeyPath: "/tmp/id_rsa", Enabled: true, Paths: []string{"/etc"}, RcloneRemote: "s3:seed"})

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

	createBody := map[string]any{
		"name":          "node2",
		"host":          "203.0.113.2",
		"port":          22,
		"user":          "root",
		"key_path":      "/tmp/id_rsa",
		"enabled":       true,
		"paths":         []string{"/var/lib/app"},
		"rclone_remote": "s3:bucket/node2",
		"rclone_flags":  []string{"--s3-chunk-size", "64M"},
	}
	rawCreate, _ := json.Marshal(createBody)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/servers", bytes.NewReader(rawCreate))
	createReq.Header.Set("Authorization", authz)
	createReq.Header.Set("Content-Type", "application/json")
	createRes := httptest.NewRecorder()
	h.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create server status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	var createResp map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	data, _ := createResp["data"].(map[string]any)
	serverID, _ := data["id"].(string)
	if serverID == "" {
		t.Fatalf("server id missing")
	}

	updateBody := map[string]any{
		"name":          "node2",
		"host":          "203.0.113.3",
		"port":          22,
		"user":          "root",
		"key_path":      "/tmp/id_rsa",
		"enabled":       true,
		"paths":         []string{"/var/lib/app", "/etc/nginx"},
		"rclone_remote": "s3:bucket/node2",
		"rclone_flags":  []string{"--checksum"},
	}
	rawUpdate, _ := json.Marshal(updateBody)
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/servers/"+serverID, bytes.NewReader(rawUpdate))
	updateReq.Header.Set("Authorization", authz)
	updateReq.Header.Set("Content-Type", "application/json")
	updateRes := httptest.NewRecorder()
	h.ServeHTTP(updateRes, updateReq)
	if updateRes.Code != http.StatusOK {
		t.Fatalf("update server status=%d body=%s", updateRes.Code, updateRes.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/servers/"+serverID, nil)
	getReq.Header.Set("Authorization", authz)
	getRes := httptest.NewRecorder()
	h.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get server status=%d body=%s", getRes.Code, getRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/servers?page=1&page_size=1", nil)
	listReq.Header.Set("Authorization", authz)
	listRes := httptest.NewRecorder()
	h.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list server status=%d body=%s", listRes.Code, listRes.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/servers/"+serverID, nil)
	delReq.Header.Set("Authorization", authz)
	delRes := httptest.NewRecorder()
	h.ServeHTTP(delRes, delReq)
	if delRes.Code != http.StatusOK {
		t.Fatalf("delete server status=%d body=%s", delRes.Code, delRes.Body.String())
	}
}

func TestRetryExecution(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{
			Timeout: "1m",
			TempDir: "/tmp",
			SSH:     config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"},
		},
		Web:  config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		I18N: config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Modules: config.ModuleConfig{
			Backup: true,
			Audit:  true,
			Users:  true,
		},
	}

	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("secret")
	user, _ := repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})
	_, _ = repo.UpsertServer(t.Context(), store.Server{Name: "node", Host: "127.0.0.1", Port: 22, User: "root", KeyPath: "/tmp/id_rsa", Enabled: true, Paths: []string{"/etc"}, RcloneRemote: "s3:bucket"})
	oldExec, _ := repo.CreateExecution(t.Context(), store.Execution{
		Status:      store.StatusFailed,
		TriggerType: "manual",
		ServerNames: []string{"node"},
		StartedAt:   time.Now().UTC().Add(-time.Minute),
		EndedAt:     time.Now().UTC(),
		Error:       "failed before",
	})

	bundle := i18n2.NewBundle()
	tr := i18n2.NewTranslator(bundle, cfg.I18N)
	tokens, _ := auth.NewTokenManager("test-secret", 15*time.Minute, 24*time.Hour)
	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: true})

	s := NewServer(cfg, repo, tokens, tr, registry, noopExecutor{})
	h := s.Router()

	pair, _ := tokens.Generate(user.ID, string(user.Role), user.Locale, time.Now().UTC())
	authz := "Bearer " + pair.AccessToken

	retryReq := httptest.NewRequest(http.MethodPost, "/api/v1/executions/"+oldExec.ID+"/retry", nil)
	retryReq.Header.Set("Authorization", authz)
	retryRes := httptest.NewRecorder()
	h.ServeHTTP(retryRes, retryReq)
	if retryRes.Code != http.StatusAccepted {
		t.Fatalf("retry status=%d body=%s", retryRes.Code, retryRes.Body.String())
	}

	var retryResp map[string]any
	if err := json.Unmarshal(retryRes.Body.Bytes(), &retryResp); err != nil {
		t.Fatalf("decode retry response: %v", err)
	}
	data, _ := retryResp["data"].(map[string]any)
	newID, _ := data["id"].(string)
	if newID == "" || newID == oldExec.ID {
		t.Fatalf("unexpected retry id old=%s new=%s", oldExec.ID, newID)
	}

	// wait for async execution update
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		ex, err := repo.GetExecution(t.Context(), newID)
		if err == nil && ex.Status != store.StatusPending && ex.Status != store.StatusRunning {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	ex, err := repo.GetExecution(t.Context(), newID)
	if err != nil {
		t.Fatalf("get retried execution error: %v", err)
	}
	if ex.Status == store.StatusPending || ex.Status == store.StatusRunning {
		t.Fatalf("expected retried execution to finish, got status=%s", ex.Status)
	}
}
