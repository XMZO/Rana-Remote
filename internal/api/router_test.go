package api

import (
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

func newRouterTestServer(t *testing.T, cfg *config.Config) (*Server, http.Handler) {
	t.Helper()
	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("secret")
	_, _ = repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})

	bundle := i18n2.NewBundle()
	tr := i18n2.NewTranslator(bundle, cfg.I18N)
	tokens, _ := auth.NewTokenManager("test-secret", 15*time.Minute, 24*time.Hour)
	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: cfg.Modules.Backup})
	_ = registry.Register(module.BasicModule{ModuleName: "schedule", OnEnabled: cfg.Modules.Schedule})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: cfg.Modules.Audit})
	_ = registry.Register(module.BasicModule{ModuleName: "users", OnEnabled: cfg.Modules.Users})

	s := NewServer(cfg, repo, tokens, tr, registry, nil)
	return s, s.Router()
}

func baseRouterConfig() *config.Config {
	return &config.Config{
		Global:  config.GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Web:     config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		I18N:    config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Modules: config.ModuleConfig{Backup: true, Schedule: true, Audit: true, Users: true},
	}
}

func TestRouter_Health(t *testing.T) {
	_, h := newRouterTestServer(t, baseRouterConfig())

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected healthz 200, got %d", res.Code)
	}
}

func TestRouter_IPAllowList(t *testing.T) {
	cfg := baseRouterConfig()
	cfg.Web.IPAllowList = []string{"127.0.0.1"}
	_, h := newRouterTestServer(t, cfg)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "203.0.113.10:3456"
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusForbidden {
		t.Fatalf("expected forbidden for non-allow-listed ip, got %d", res.Code)
	}
}

func TestRouter_ScheduleFeatureDisabled(t *testing.T) {
	cfg := baseRouterConfig()
	cfg.Modules.Schedule = false
	_, h := newRouterTestServer(t, cfg)
	access := loginAccessToken(t, h)

	for _, method := range []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete} {
		path := "/api/v1/schedules"
		if method == http.MethodPut || method == http.MethodDelete {
			path = "/api/v1/schedules/test-id"
		}
		req := httptest.NewRequest(method, path, nil)
		req.Header.Set("Authorization", "Bearer "+access)
		res := httptest.NewRecorder()
		h.ServeHTTP(res, req)
		if res.Code != http.StatusNotImplemented {
			t.Fatalf("method %s expected 501, got %d body=%s", method, res.Code, res.Body.String())
		}
		var payload map[string]any
		if err := json.Unmarshal(res.Body.Bytes(), &payload); err != nil {
			t.Fatalf("decode response for %s: %v", method, err)
		}
		if payload["code"] != "feature_disabled" {
			t.Fatalf("method %s unexpected code: %#v", method, payload["code"])
		}
	}
}

func TestRouter_LoginRemainsAvailableWhenUsersModuleDisabled(t *testing.T) {
	cfg := baseRouterConfig()
	cfg.Modules.Users = false
	_, h := newRouterTestServer(t, cfg)

	access := loginAccessToken(t, h)
	if access == "" {
		t.Fatal("expected access token")
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/modules", nil)
	req.Header.Set("Authorization", "Bearer "+access)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusOK {
		t.Fatalf("expected modules endpoint available, got %d body=%s", res.Code, res.Body.String())
	}
}

func TestRouter_RecoveryReturns500InsteadOfReset(t *testing.T) {
	h := withTraceID(withRecovery(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	res := httptest.NewRecorder()
	h.ServeHTTP(res, req)
	if res.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", res.Code, res.Body.String())
	}
	if got := res.Header().Get(traceHeaderName); got == "" {
		t.Fatal("expected trace header on panic response")
	}
}
