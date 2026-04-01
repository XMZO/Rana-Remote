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
	srv := NewServer(cfg, repo, tokens, tr, registry, nil)
	return srv, srv.Router()
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
	if !s.cfg.Notify.Email.Enabled || s.cfg.Notify.Email.SMTPHost != "smtp.example.com" {
		t.Fatalf("email notify settings not applied: %+v", s.cfg.Notify.Email)
	}
	if len(s.cfg.Notify.Email.To) != 2 {
		t.Fatalf("expected two recipients, got %+v", s.cfg.Notify.Email.To)
	}
	dbSettings, err := s.repo.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("failed to get settings from DB: %v", err)
	}
	if !dbSettings.NotifyEmailEnabled || dbSettings.NotifyEmailSMTPHost != "smtp.example.com" {
		t.Fatalf("settings not persisted to DB: %+v", dbSettings)
	}
	if len(dbSettings.NotifyEmailTo) != 2 {
		t.Fatalf("expected two recipients in DB, got %+v", dbSettings.NotifyEmailTo)
	}
	if dbSettings.UpdatedAt.IsZero() {
		t.Fatalf("expected UpdatedAt to be persisted")
	}
}

func TestSettings_UpdatePersistsToDB(t *testing.T) {
	s, h := newSettingsTestServer(t)
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
	dbSettings, err := s.repo.GetSettings(t.Context())
	if err != nil {
		t.Fatalf("failed to get settings from DB: %v", err)
	}
	if dbSettings.NotifyWebhookURL != "https://example.com/hook" {
		t.Fatalf("webhook URL not persisted to DB: %s", dbSettings.NotifyWebhookURL)
	}
	if !dbSettings.NotifyOnSuccess {
		t.Fatalf("on_success not persisted to DB")
	}
	if dbSettings.NotifySuppressionWindow != "10m" {
		t.Fatalf("suppression_window not persisted to DB: %s", dbSettings.NotifySuppressionWindow)
	}
	if dbSettings.UpdatedAt.IsZero() {
		t.Fatalf("expected UpdatedAt to be persisted")
	}
	if s.cfg.Notify.WebhookURL != "https://example.com/hook" {
		t.Fatalf("webhook URL not synced to config: %s", s.cfg.Notify.WebhookURL)
	}
}

func TestSettings_GetReturnsConfigSnapshotAfterDBSave(t *testing.T) {
	s, h := newSettingsTestServer(t)
	initialSettings := store.SystemSettings{
		GlobalTimeout:           "2m",
		GlobalConcurrency:       5,
		GlobalSSHStrictHostKey:  false,
		GlobalSSHKnownHostsPath: "/custom/path",
		ModulesSchedule:         true,
		ModulesAudit:            true,
		ModulesNotify:           false,
		ModulesUsers:            true,
		I18NDefaultLocale:       "en-US",
		NotifyOnSuccess:         true,
		NotifyOnFailure:         false,
		NotifySuppressionWindow: "15m",
		NotifyEmailEnabled:      true,
		NotifyEmailSMTPHost:     "custom.smtp.com",
		NotifyEmailSMTPPort:     587,
		NotifyEmailFrom:         "test@example.com",
		NotifyEmailTo:           []string{"dest@example.com"},
		NotifyWebhookURL:        "https://custom.hook.com",
		UpdatedAt:               time.Now().UTC().Truncate(time.Millisecond),
	}
	if err := s.repo.SaveSettings(t.Context(), initialSettings); err != nil {
		t.Fatalf("failed to save initial settings: %v", err)
	}

	// GET currently serves the in-memory config snapshot rather than reading from DB.
	s.cfg.Global.Timeout = initialSettings.GlobalTimeout
	s.cfg.Global.Concurrency = initialSettings.GlobalConcurrency
	s.cfg.Global.SSH.StrictHostKey = initialSettings.GlobalSSHStrictHostKey
	s.cfg.Global.SSH.KnownHostsPath = initialSettings.GlobalSSHKnownHostsPath
	s.cfg.Modules.Schedule = initialSettings.ModulesSchedule
	s.cfg.Modules.Audit = initialSettings.ModulesAudit
	s.cfg.Modules.Notify = initialSettings.ModulesNotify
	s.cfg.Modules.Users = initialSettings.ModulesUsers
	s.cfg.I18N.DefaultLocale = initialSettings.I18NDefaultLocale
	s.cfg.Notify.OnSuccess = initialSettings.NotifyOnSuccess
	s.cfg.Notify.OnFailure = initialSettings.NotifyOnFailure
	s.cfg.Notify.SuppressionWindow = initialSettings.NotifySuppressionWindow
	s.cfg.Notify.Email.Enabled = initialSettings.NotifyEmailEnabled
	s.cfg.Notify.Email.SMTPHost = initialSettings.NotifyEmailSMTPHost
	s.cfg.Notify.Email.SMTPPort = initialSettings.NotifyEmailSMTPPort
	s.cfg.Notify.Email.From = initialSettings.NotifyEmailFrom
	s.cfg.Notify.Email.To = append([]string(nil), initialSettings.NotifyEmailTo...)
	s.cfg.Notify.WebhookURL = initialSettings.NotifyWebhookURL

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
	global := data["global"].(map[string]any)
	if global["timeout"] != "2m" {
		t.Fatalf("expected timeout 2m, got %v", global["timeout"])
	}
	if int(global["concurrency"].(float64)) != 5 {
		t.Fatalf("expected concurrency 5, got %v", global["concurrency"])
	}
	ssh := global["ssh"].(map[string]any)
	if ssh["strict_host_key"] != false {
		t.Fatalf("expected strict_host_key false, got %v", ssh["strict_host_key"])
	}
	modules := data["modules"].(map[string]any)
	if modules["notify"] != false {
		t.Fatalf("expected modules.notify false, got %v", modules["notify"])
	}
	i18n := data["i18n"].(map[string]any)
	if i18n["default_locale"] != "en-US" {
		t.Fatalf("expected default_locale en-US, got %v", i18n["default_locale"])
	}
	notify := data["notify"].(map[string]any)
	if notify["webhook_url"] != "https://custom.hook.com" {
		t.Fatalf("expected webhook_url from config snapshot, got %v", notify["webhook_url"])
	}
}

func TestSettings_ConfigFallbackOnEmptyDB(t *testing.T) {
	s, h := newSettingsTestServer(t)
	s.cfg.Global.Timeout = "3m"
	s.cfg.Global.Concurrency = 10
	s.cfg.Modules.Schedule = false
	s.cfg.I18N.DefaultLocale = "zh-CN"

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
	global := data["global"].(map[string]any)
	if global["timeout"] != "3m" {
		t.Fatalf("expected timeout 3m from config, got %v", global["timeout"])
	}
	if int(global["concurrency"].(float64)) != 10 {
		t.Fatalf("expected concurrency 10 from config, got %v", global["concurrency"])
	}
	modules := data["modules"].(map[string]any)
	if modules["schedule"] != false {
		t.Fatalf("expected modules.schedule false from config, got %v", modules["schedule"])
	}
}
