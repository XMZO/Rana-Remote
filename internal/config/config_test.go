package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoad_WithEnvInterpolation(t *testing.T) {
	t.Setenv("SSH_KEY_PATH", "/tmp/id_rsa")
	t.Setenv("RANA_ADMIN_USER", "admin")
	t.Setenv("RANA_ADMIN_PASS", "secret")

	path := filepath.Join(t.TempDir(), "config.yaml")
	yaml := `
global:
  timeout: "5m"
web:
  enabled: true
  listen: ":8080"
database:
  driver: "sqlite"
  dsn: ":memory:"
auth:
  bootstrap_admin:
    username: "${RANA_ADMIN_USER}"
    password: "${RANA_ADMIN_PASS}"
modules:
  backup: true
i18n:
  default_locale: "zh-CN"
  supported_locales: ["zh-CN", "en-US"]
  fallback_locale: "en-US"
servers:
  - name: "node1"
    host: "127.0.0.1"
    user: "root"
    key_path: "${SSH_KEY_PATH}"
    paths: ["/etc"]
    rclone:
      remote: "s3:bucket/path"
`
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if cfg.Servers[0].KeyPath != "/tmp/id_rsa" {
		t.Fatalf("unexpected key path: %s", cfg.Servers[0].KeyPath)
	}
	if cfg.Servers[0].Port != 22 {
		t.Fatalf("expected default port 22, got %d", cfg.Servers[0].Port)
	}
}

func TestValidate_I18NFallbackMustExist(t *testing.T) {
	cfg := &Config{
		Global:  GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Modules: ModuleConfig{Backup: true},
		I18N: I18NConfig{
			DefaultLocale:    "zh-CN",
			SupportedLocales: []string{"zh-CN"},
			FallbackLocale:   "en-US",
		},
	}
	next := WithDefaults(*cfg)
	err := next.Validate()
	if err == nil || !strings.Contains(err.Error(), "fallback") {
		t.Fatalf("expected fallback validation error, got %v", err)
	}
}

func TestValidate_WebIPAllowList(t *testing.T) {
	cfg := &Config{
		Global:  GlobalConfig{Timeout: "1m", TempDir: "/tmp", SSH: SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"}},
		Modules: ModuleConfig{Backup: true},
		Web: WebConfig{
			Enabled:         true,
			Listen:          ":8080",
			SessionTTL:      "24h",
			AccessTokenTTL:  "15m",
			RefreshTokenTTL: "168h",
			IPAllowList:     []string{"invalid-ip"},
		},
		Database: DatabaseConfig{Driver: "sqlite", DSN: ":memory:"},
		Auth: AuthConfig{
			BootstrapAdmin: BootstrapAdmin{
				Username: "admin",
				Password: "secret",
			},
		},
		I18N: I18NConfig{
			DefaultLocale:    "zh-CN",
			SupportedLocales: []string{"zh-CN", "en-US"},
			FallbackLocale:   "en-US",
		},
	}
	next := WithDefaults(*cfg)
	if err := next.Validate(); err == nil || !strings.Contains(err.Error(), "ip_allow_list") {
		t.Fatalf("expected ip_allow_list validation error, got %v", err)
	}
}

func TestValidate_RetentionDuration(t *testing.T) {
	cfg := &Config{
		Global: GlobalConfig{
			Timeout:   "1m",
			TempDir:   "/tmp",
			SSH:       SSHConfig{StrictHostKey: true, KnownHostsPath: "/tmp/known_hosts"},
			Retention: RetentionConfig{Executions: "bad-duration", ExecutionLogs: "24h", AuditLogs: "72h"},
		},
		Modules:  ModuleConfig{Backup: true},
		Web:      WebConfig{Enabled: true, Listen: ":8080", SessionTTL: "24h", AccessTokenTTL: "15m", RefreshTokenTTL: "168h"},
		Database: DatabaseConfig{Driver: "sqlite", DSN: ":memory:"},
		Auth:     AuthConfig{BootstrapAdmin: BootstrapAdmin{Username: "admin", Password: "secret"}},
		I18N:     I18NConfig{DefaultLocale: "zh-CN", SupportedLocales: []string{"zh-CN", "en-US"}, FallbackLocale: "en-US"},
	}
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "global.retention.executions") {
		t.Fatalf("expected retention validation error, got %v", err)
	}
}

func TestDefaultConfig_UsesEnvBootstrapAdmin(t *testing.T) {
	t.Setenv("RANA_ADMIN_USER", "root-admin")
	t.Setenv("RANA_ADMIN_PASS", "super-secret")

	cfg := DefaultConfig(filepath.Join("/tmp", "rana", "config.yaml"))
	if cfg.Auth.BootstrapAdmin.Username != "root-admin" {
		t.Fatalf("unexpected bootstrap username: %s", cfg.Auth.BootstrapAdmin.Username)
	}
	if cfg.Auth.BootstrapAdmin.Password != "super-secret" {
		t.Fatalf("unexpected bootstrap password: %s", cfg.Auth.BootstrapAdmin.Password)
	}
	if cfg.Database.DSN != filepath.Join("/tmp", "rana", "rana.db") {
		t.Fatalf("unexpected sqlite dsn: %s", cfg.Database.DSN)
	}
	if len(cfg.Servers) != 0 {
		t.Fatalf("expected no seeded servers, got %d", len(cfg.Servers))
	}
}

func TestLoadOrInit_CreatesMinimalConfigWhenMissing(t *testing.T) {
	t.Setenv("RANA_ADMIN_USER", "admin")
	t.Setenv("RANA_ADMIN_PASS", "secret")

	path := filepath.Join(t.TempDir(), "config", "config.yaml")
	cfg, created, err := LoadOrInit(path)
	if err != nil {
		t.Fatalf("LoadOrInit() error = %v", err)
	}
	if !created {
		t.Fatal("expected config file to be created")
	}
	if cfg.Web.Listen != ":8080" {
		t.Fatalf("unexpected web.listen: %s", cfg.Web.Listen)
	}
	if cfg.Database.DSN != filepath.Join(filepath.Dir(path), "rana.db") {
		t.Fatalf("unexpected sqlite dsn: %s", cfg.Database.DSN)
	}
	persisted, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read created config: %v", err)
	}
	text := string(persisted)
	if !strings.Contains(text, "username: admin") {
		t.Fatalf("created config missing bootstrap username: %s", text)
	}
	if !strings.Contains(filepath.ToSlash(text), "dsn: "+filepath.ToSlash(filepath.Join(filepath.Dir(path), "rana.db"))) {
		t.Fatalf("created config missing sqlite dsn: %s", text)
	}
}
