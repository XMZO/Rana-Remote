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

func newAPITestServer(t *testing.T) (*Server, http.Handler, *auth.TokenManager, store.User, *store.MemoryRepository) {
	t.Helper()
	cfg := &config.Config{
		Global:  config.GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: config.SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Web:     config.WebConfig{Enabled: true, Listen: ":8080", AccessTokenTTL: "15m", RefreshTokenTTL: "24h"},
		I18N:    config.I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US", LocaleSourceOrder: []string{"query", "cookie", "header"}},
		Modules: config.ModuleConfig{Backup: true, Schedule: true, Audit: true, Users: true},
		Auth: config.AuthConfig{
			PasswordPolicy: config.PasswordPolicy{
				MinLength:      8,
				RequireNumber:  true,
				RequireSpecial: true,
			},
		},
	}
	repo := store.NewMemoryRepository()
	hash, _ := auth.HashPassword("Admin#123")
	admin, _ := repo.CreateUser(t.Context(), store.User{Username: "admin", PasswordHash: hash, Role: store.RoleAdmin, Locale: "zh-CN"})
	_, _ = repo.UpsertServer(t.Context(), store.Server{Name: "node", Host: "127.0.0.1", Port: 22, User: "root", KeyPath: "/tmp/id_rsa", Enabled: true, Paths: []string{"/etc"}, RcloneRemote: "s3:bucket"})

	bundle := i18n2.NewBundle()
	tr := i18n2.NewTranslator(bundle, cfg.I18N)
	tokens, _ := auth.NewTokenManager("test-secret", 15*time.Minute, 24*time.Hour)
	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "policy", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "schedule", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: true})
	_ = registry.Register(module.BasicModule{ModuleName: "users", OnEnabled: true})

	s := NewServer(cfg, repo, tokens, tr, registry, noopExecutor{})
	return s, s.Router(), tokens, admin, repo
}

func TestPolicyCRUDAndExecutionByPolicy(t *testing.T) {
	_, h, tokens, admin, _ := newAPITestServer(t)
	pair, _ := tokens.Generate(admin.ID, string(admin.Role), admin.Locale, time.Now().UTC())
	authz := "Bearer " + pair.AccessToken

	create := map[string]any{
		"name":          "daily-node",
		"server_names":  []string{"node"},
		"paths":         []string{"/etc", "/var/lib"},
		"rclone_remote": "s3:bucket/policy",
		"rclone_flags":  []string{"--checksum"},
		"timeout_sec":   30,
		"retry_limit":   1,
		"enabled":       true,
	}
	body, _ := json.Marshal(create)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/policies", bytes.NewReader(body))
	createReq.Header.Set("Authorization", authz)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-CSRF-Token", "skip")
	createRes := httptest.NewRecorder()
	h.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create policy status=%d body=%s", createRes.Code, createRes.Body.String())
	}

	var createResp map[string]any
	_ = json.Unmarshal(createRes.Body.Bytes(), &createResp)
	policyID, _ := createResp["data"].(map[string]any)["id"].(string)
	if policyID == "" {
		t.Fatalf("policy id missing")
	}

	execBody, _ := json.Marshal(map[string]any{"policy_id": policyID, "trigger_type": "manual"})
	execReq := httptest.NewRequest(http.MethodPost, "/api/v1/executions", bytes.NewReader(execBody))
	execReq.Header.Set("Authorization", authz)
	execReq.Header.Set("Content-Type", "application/json")
	execReq.Header.Set("X-CSRF-Token", "skip")
	execRes := httptest.NewRecorder()
	h.ServeHTTP(execRes, execReq)
	if execRes.Code != http.StatusAccepted {
		t.Fatalf("create execution by policy status=%d body=%s", execRes.Code, execRes.Body.String())
	}
}

func TestUsersCRUDAndPassword(t *testing.T) {
	_, h, tokens, admin, _ := newAPITestServer(t)
	pair, _ := tokens.Generate(admin.ID, string(admin.Role), admin.Locale, time.Now().UTC())
	authz := "Bearer " + pair.AccessToken

	createUser := map[string]any{
		"username": "operator1",
		"password": "Pass#1234",
		"role":     "operator",
		"locale":   "en-US",
	}
	body, _ := json.Marshal(createUser)
	createReq := httptest.NewRequest(http.MethodPost, "/api/v1/users", bytes.NewReader(body))
	createReq.Header.Set("Authorization", authz)
	createReq.Header.Set("Content-Type", "application/json")
	createReq.Header.Set("X-CSRF-Token", "skip")
	createRes := httptest.NewRecorder()
	h.ServeHTTP(createRes, createReq)
	if createRes.Code != http.StatusCreated {
		t.Fatalf("create user status=%d body=%s", createRes.Code, createRes.Body.String())
	}
	var userResp map[string]any
	_ = json.Unmarshal(createRes.Body.Bytes(), &userResp)
	userID, _ := userResp["data"].(map[string]any)["id"].(string)
	if userID == "" {
		t.Fatalf("user id missing")
	}

	resetBody, _ := json.Marshal(map[string]any{"new_password": "Pass#5678"})
	resetReq := httptest.NewRequest(http.MethodPut, "/api/v1/users/"+userID+"/password", bytes.NewReader(resetBody))
	resetReq.Header.Set("Authorization", authz)
	resetReq.Header.Set("Content-Type", "application/json")
	resetReq.Header.Set("X-CSRF-Token", "skip")
	resetRes := httptest.NewRecorder()
	h.ServeHTTP(resetRes, resetReq)
	if resetRes.Code != http.StatusOK {
		t.Fatalf("reset password status=%d body=%s", resetRes.Code, resetRes.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/users?page=1&page_size=20", nil)
	listReq.Header.Set("Authorization", authz)
	listRes := httptest.NewRecorder()
	h.ServeHTTP(listRes, listReq)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list users status=%d body=%s", listRes.Code, listRes.Body.String())
	}
}
