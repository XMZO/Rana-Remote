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

func TestScheduleCRUD(t *testing.T) {
	cfg := &config.Config{
		Global: config.GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Web:    config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		I18N:   config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Modules: config.ModuleConfig{
			Backup:   true,
			Audit:    true,
			Users:    true,
			Schedule: true,
		},
	}
	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("secret")
	user, _ := repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})
	_, _ = repo.UpsertServer(t.Context(), store.Server{Name: "node", Host: "127.0.0.1", Port: 22, User: "root", KeyPath: "/tmp/id_rsa", Enabled: true, Paths: []string{"/etc"}, RcloneRemote: "s3:bucket"})

	bundle := i18n2.NewBundle()
	tr := i18n2.NewTranslator(bundle, cfg.I18N)
	tokens, _ := auth.NewTokenManager("test-secret", 15*time.Minute, 24*time.Hour)
	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "schedule", OnEnabled: true})

	s := NewServer(cfg, repo, tokens, tr, registry, nil)
	h := s.Router()
	pair, _ := tokens.Generate(user.ID, string(user.Role), user.Locale, time.Now().UTC())
	authz := "Bearer " + pair.AccessToken

	createPayload := map[string]any{
		"name":           "every-five",
		"server_names":   []string{"node"},
		"cron_expr":      "@every 5m",
		"misfire_policy": "run_once",
		"enabled":        true,
	}
	body, _ := json.Marshal(createPayload)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/schedules", bytes.NewReader(body))
	createReq.Header.Set("Authorization", authz)
	createReq.Header.Set("Content-Type", "application/json")
	createRes := httptest.NewRecorder()
	h.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create schedule status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	var createResp map[string]any
	if err := json.Unmarshal(createRes.Body.Bytes(), &createResp); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	data, _ := createResp["data"].(map[string]any)
	scheduleID, _ := data["id"].(string)
	if scheduleID == "" {
		t.Fatalf("schedule id missing")
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/schedules", nil)
	listReq.Header.Set("Authorization", authz)
	listRes := httptest.NewRecorder()
	h.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list schedule status=%d body=%s", listRes.Code, listRes.Body.String())
	}

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/schedules/"+scheduleID, nil)
	getReq.Header.Set("Authorization", authz)
	getRes := httptest.NewRecorder()
	h.ServeHTTP(getRes, getReq)
	if getRes.Code != http.StatusOK {
		t.Fatalf("get schedule status=%d body=%s", getRes.Code, getRes.Body.String())
	}

	delReq := httptest.NewRequest(http.MethodDelete, "/api/v1/schedules/"+scheduleID, nil)
	delReq.Header.Set("Authorization", authz)
	delRes := httptest.NewRecorder()
	h.ServeHTTP(delRes, delReq)
	if delRes.Code != http.StatusOK {
		t.Fatalf("delete schedule status=%d body=%s", delRes.Code, delRes.Body.String())
	}
}
