package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/config"
	i18n2 "github.com/rana-remote/rana-remote/internal/i18n"
	"github.com/rana-remote/rana-remote/internal/module"
	"github.com/rana-remote/rana-remote/internal/store"
)

func newSettingsTestServer(t *testing.T) (*Server, http.Handler) {
	t.Helper()
	cfg := &config.Config{
		Global:   config.GlobalConfig{Timeout: "1m", Concurrency: 1, TempDir: "/tmp", SSH: config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Web:      config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		Database: config.DatabaseConfig{Driver: "memory", DSN: "memory"},
		Auth:     config.AuthConfig{BootstrapAdmin: config.BootstrapAdmin{Username: "admin", Password: "secret"}, PasswordPolicy: config.PasswordPolicy{MinLength: 12}},
		I18N:     config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Notify:   config.NotifyConfig{SuppressionWindow: "0s"},
		Modules:  config.ModuleConfig{Backup: true, Audit: true, Notify: true, Users: true},
	}
	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("secret")
	_, _ = repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})
	bundle := i18n2.NewBundle()
	tr := i18n2.NewTranslator(bundle, cfg.I18N)
	tokens, _ := auth.NewTokenManager("test-secret", 15*time.Minute, 24*time.Hour)
	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "notify", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "users", OnEnabled: true})
	s := NewServer(cfg, repo, tokens, tr, registry, nil)
	return s, s.Router()
}

func loginAccessToken(t *testing.T, h http.Handler) string {
	t.Helper()
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
		t.Fatalf("decode login response: %v", err)
	}
	data := payload["data"].(map[string]any)
	access, _ := data["access_token"].(string)
	if access == "" {
		t.Fatal("missing access token")
	}
	return access
}

func TestSettings_GetIncludesEmailNotify(t *testing.T) {
	s, h := newSettingsTestServer(t)
	s.cfg.Notify.Email = config.EmailNotifyConfig{
		Enabled:  true,
		SMTPHost: "smtp.example.com",
		SMTPPort: 465,
		Username: "bot",
		Password: "secret",
		From:     "rana@example.com",
		To:       []string{"ops@example.com"},
		UseTLS:   true,
	}
	access := loginAccessToken(t, h)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/settings", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("settings get status=%d body=%s", res.Code, res.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode settings response: %v", err)
	}
	data := payload["data"].(map[string]any)
	notify := data["notify"].(map[string]any)
	email := notify["email"].(map[string]any)
	if email["smtp_host"] != "smtp.example.com" {
		t.Fatalf("unexpected smtp_host: %#v", email["smtp_host"])
	}
	if got := int(email["smtp_port"].(float64)); got != 465 {
		t.Fatalf("unexpected smtp_port: %d", got)
	}
}

func TestSettings_UpdateEmailNotify(t *testing.T) {
	s, h := newSettingsTestServer(t)
	access := loginAccessToken(t, h)
	body := []byte(`{
	  "notify": {
	    "email": {
	      "enabled": true,
	      "smtp_host": "smtp.example.com",
	      "smtp_port": 465,
	      "username": "bot",
	      "password": "secret",
	      "from": "rana@example.com",
	      "to": ["ops@example.com", "dev@example.com"],
	      "use_tls": true
	    },
	    "on_failure": true,
	    "suppression_window": "5m"
	  }
	}`)
	var parsed updateSettingsRequest
	if err := json.Unmarshal(body, &parsed); err != nil {
		t.Fatalf("decode request body: %v", err)
	}
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"secret"}`)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRes := httptest.NewRecorder()
	h.ServeHTTP(loginRes, loginReq)
	for _, cookie := range loginRes.Result().Cookies() {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("settings put status=%d body=%s parsed=%+v csrf=%v", res.Code, res.Body.String(), parsed, loginRes.Result().Cookies())
	}
	if !s.cfg.Notify.Email.Enabled || s.cfg.Notify.Email.SMTPHost != "smtp.example.com" {
		t.Fatalf("email notify settings not applied: %+v", s.cfg.Notify.Email)
	}
	if len(s.cfg.Notify.Email.To) != 2 {
		t.Fatalf("expected two recipients, got %+v", s.cfg.Notify.Email.To)
	}
}

func TestSettings_UpdatePersistsConfigFile(t *testing.T) {
	s, h := newSettingsTestServer(t)
	cfgPath := filepath.Join(t.TempDir(), "config.yaml")
	s.SetConfigPath(cfgPath)
	if err := config.Persist(cfgPath, s.cfg); err != nil {
		t.Fatalf("persist seed config: %v", err)
	}
	access := loginAccessToken(t, h)
	body := []byte(`{
	  "notify": {
	    "webhook_url": "https://example.com/hook",
	    "on_success": true,
	    "suppression_window": "10m"
	  }
	}`)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/settings", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+access)
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/auth/login", bytes.NewReader([]byte(`{"username":"admin","password":"secret"}`)))
	loginReq.Header.Set("Content-Type", "application/json")
	loginRes := httptest.NewRecorder()
	h.ServeHTTP(loginRes, loginReq)
	for _, cookie := range loginRes.Result().Cookies() {
		req.AddCookie(cookie)
	}
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("settings put status=%d body=%s", res.Code, res.Body.String())
	}
	persisted, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	text := string(persisted)
	if !strings.Contains(text, "webhook_url: https://example.com/hook") {
		t.Fatalf("persisted config missing webhook url: %s", text)
	}
	if !strings.Contains(text, "suppression_window: 10m") {
		t.Fatalf("persisted config missing suppression window: %s", text)
	}
}
