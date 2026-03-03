package api

import (
	"bytes"
	"encoding/json"
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

func TestRouter_LoginAndMe(t *testing.T) {
	cfg := &config.Config{
		Global:  config.GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Web:     config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		I18N:    config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Modules: config.ModuleConfig{Backup: true, Audit: true, Users: true},
	}
	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("secret")
	_, _ = repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})
	_, _ = repo.UpsertServer(t.Context(), store.Server{Name: "node", Host: "127.0.0.1", Port: 22, User: "root", KeyPath: "/tmp/id_rsa", Enabled: true, Paths: []string{"/etc"}, RcloneRemote: "s3:bucket"})

	bundle := i18n2.NewBundle()
	tr := i18n2.NewTranslator(bundle, cfg.I18N)
	tokens, _ := auth.NewTokenManager("test-secret", 15*time.Minute, 24*time.Hour)
	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: true})

	s := NewServer(cfg, repo, tokens, tr, registry, nil)
	h := s.Router()

	loginBody := []byte(`{"username":"admin","password":"secret"}`)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader(loginBody))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRes := httptest.NewRecorder()
	h.ServeHTTP(loginRes, loginReq)
	if loginRes.Code != http.StatusOK {
		t.Fatalf("login status=%d body=%s", loginRes.Code, loginRes.Body.String())
	}

	var payload map[string]any
	if err := json.Unmarshal(loginRes.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode login body: %v", err)
	}
	data, _ := payload["data"].(map[string]any)
	access, _ := data["access_token"].(string)
	if access == "" {
		t.Fatalf("access token missing")
	}

	meReq := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
	meReq.Header.Set("Authorization", "Bearer "+access)
	meRes := httptest.NewRecorder()
	h.ServeHTTP(meRes, meReq)
	if meRes.Code != http.StatusOK {
		t.Fatalf("me status=%d body=%s", meRes.Code, meRes.Body.String())
	}
}
