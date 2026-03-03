package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	defaultTimeout         = "30m"
	defaultTempDir         = "/tmp/rana-backup"
	defaultKnownHostsPath  = "~/.ssh/known_hosts"
	defaultWebListen       = ":8080"
	defaultWebBasePath     = "/"
	defaultSessionTTL      = "24h"
	defaultAccessTokenTTL  = "15m"
	defaultRefreshTokenTTL = "168h"
	defaultDBDriver        = "sqlite"
	defaultDBDSN           = "./data/rana.db"
	defaultLoginRateLimit  = "5/m"
	defaultLocale          = "zh-CN"
	defaultFallbackLocale  = "en-US"
)

// Config is the application config root.
type Config struct {
	Global   GlobalConfig   `yaml:"global"`
	Web      WebConfig      `yaml:"web"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	I18N     I18NConfig     `yaml:"i18n"`
	Modules  ModuleConfig   `yaml:"modules"`
	Servers  []Server       `yaml:"servers"`
}

type GlobalConfig struct {
	Timeout     string    `yaml:"timeout"`
	Concurrency int       `yaml:"concurrency"`
	TempDir     string    `yaml:"temp_dir"`
	SSH         SSHConfig `yaml:"ssh"`
}

type SSHConfig struct {
	StrictHostKey  bool   `yaml:"strict_host_key"`
	KnownHostsPath string `yaml:"known_hosts_path"`
}

type WebConfig struct {
	Enabled          bool     `yaml:"enabled"`
	Listen           string   `yaml:"listen"`
	BasePath         string   `yaml:"base_path"`
	SessionTTL       string   `yaml:"session_ttl"`
	AccessTokenTTL   string   `yaml:"access_token_ttl"`
	RefreshTokenTTL  string   `yaml:"refresh_token_ttl"`
	CSRFEnabled      bool     `yaml:"csrf_enabled"`
	CORSAllowOrigins []string `yaml:"cors_allow_origins"`
}

type DatabaseConfig struct {
	Driver string `yaml:"driver"`
	DSN    string `yaml:"dsn"`
}

type AuthConfig struct {
	BootstrapAdmin BootstrapAdmin `yaml:"bootstrap_admin"`
	PasswordPolicy PasswordPolicy `yaml:"password_policy"`
	LoginRateLimit string         `yaml:"login_rate_limit"`
}

type BootstrapAdmin struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
}

type PasswordPolicy struct {
	MinLength      int  `yaml:"min_length"`
	RequireNumber  bool `yaml:"require_number"`
	RequireSpecial bool `yaml:"require_special"`
}

type I18NConfig struct {
	DefaultLocale     string   `yaml:"default_locale"`
	SupportedLocales  []string `yaml:"supported_locales"`
	FallbackLocale    string   `yaml:"fallback_locale"`
	LocaleSourceOrder []string `yaml:"locale_source_order"`
}

type ModuleConfig struct {
	Backup   bool `yaml:"backup"`
	Schedule bool `yaml:"schedule"`
	Audit    bool `yaml:"audit"`
	Notify   bool `yaml:"notify"`
	Users    bool `yaml:"users"`
}

type Server struct {
	Name       string   `yaml:"name"`
	Host       string   `yaml:"host"`
	Port       int      `yaml:"port"`
	User       string   `yaml:"user"`
	KeyPath    string   `yaml:"key_path"`
	Passphrase string   `yaml:"passphrase"`
	Paths      []string `yaml:"paths"`
	Rclone     Rclone   `yaml:"rclone"`
}

type Rclone struct {
	Remote string   `yaml:"remote"`
	Flags  []string `yaml:"flags"`
}

func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}

	expanded := os.ExpandEnv(string(raw))
	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	cfg.applyDefaults()
	cfg.expandPaths()
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *Config) applyDefaults() {
	if strings.TrimSpace(c.Global.Timeout) == "" {
		c.Global.Timeout = defaultTimeout
	}
	if strings.TrimSpace(c.Global.TempDir) == "" {
		c.Global.TempDir = defaultTempDir
	}
	if strings.TrimSpace(c.Global.SSH.KnownHostsPath) == "" {
		c.Global.SSH.KnownHostsPath = defaultKnownHostsPath
	}
	if !c.Global.SSH.StrictHostKey {
		// secure-by-default
		c.Global.SSH.StrictHostKey = true
	}

	if strings.TrimSpace(c.Web.Listen) == "" {
		c.Web.Listen = defaultWebListen
	}
	if strings.TrimSpace(c.Web.BasePath) == "" {
		c.Web.BasePath = defaultWebBasePath
	}
	if strings.TrimSpace(c.Web.SessionTTL) == "" {
		c.Web.SessionTTL = defaultSessionTTL
	}
	if strings.TrimSpace(c.Web.AccessTokenTTL) == "" {
		c.Web.AccessTokenTTL = defaultAccessTokenTTL
	}
	if strings.TrimSpace(c.Web.RefreshTokenTTL) == "" {
		c.Web.RefreshTokenTTL = defaultRefreshTokenTTL
	}

	if strings.TrimSpace(c.Database.Driver) == "" {
		c.Database.Driver = defaultDBDriver
	}
	if strings.TrimSpace(c.Database.DSN) == "" {
		c.Database.DSN = defaultDBDSN
	}
	if strings.TrimSpace(c.Auth.LoginRateLimit) == "" {
		c.Auth.LoginRateLimit = defaultLoginRateLimit
	}
	if c.Auth.PasswordPolicy.MinLength == 0 {
		c.Auth.PasswordPolicy.MinLength = 12
	}

	if strings.TrimSpace(c.I18N.DefaultLocale) == "" {
		c.I18N.DefaultLocale = defaultLocale
	}
	if strings.TrimSpace(c.I18N.FallbackLocale) == "" {
		c.I18N.FallbackLocale = defaultFallbackLocale
	}
	if len(c.I18N.SupportedLocales) == 0 {
		c.I18N.SupportedLocales = []string{defaultLocale, defaultFallbackLocale}
	}
	if len(c.I18N.LocaleSourceOrder) == 0 {
		c.I18N.LocaleSourceOrder = []string{"query", "cookie", "header"}
	}

	// backup is foundational and should default true.
	if !c.Modules.Backup {
		c.Modules.Backup = true
	}

	for i := range c.Servers {
		if c.Servers[i].Port == 0 {
			c.Servers[i].Port = 22
		}
	}
}

func (c *Config) expandPaths() {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	expand := func(s string) string {
		if home == "" {
			return s
		}
		if strings.HasPrefix(s, "~/") {
			return filepath.Join(home, strings.TrimPrefix(s, "~/"))
		}
		return s
	}

	c.Global.SSH.KnownHostsPath = expand(c.Global.SSH.KnownHostsPath)
	for i := range c.Servers {
		c.Servers[i].KeyPath = expand(c.Servers[i].KeyPath)
	}
}

func (c *Config) Validate() error {
	if c.Global.Concurrency < 0 {
		return errors.New("global.concurrency must be >= 0")
	}
	if _, err := c.GlobalTimeout(); err != nil {
		return fmt.Errorf("invalid global.timeout: %w", err)
	}
	if strings.TrimSpace(c.Global.TempDir) == "" {
		return errors.New("global.temp_dir is required")
	}
	if c.Global.SSH.StrictHostKey && strings.TrimSpace(c.Global.SSH.KnownHostsPath) == "" {
		return errors.New("global.ssh.known_hosts_path is required when strict_host_key=true")
	}

	if c.Web.Enabled {
		if strings.TrimSpace(c.Web.Listen) == "" {
			return errors.New("web.listen is required when web.enabled=true")
		}
		if strings.TrimSpace(c.Database.Driver) == "" || strings.TrimSpace(c.Database.DSN) == "" {
			return errors.New("database.driver and database.dsn are required when web.enabled=true")
		}
		if strings.TrimSpace(c.Auth.BootstrapAdmin.Username) == "" || strings.TrimSpace(c.Auth.BootstrapAdmin.Password) == "" {
			return errors.New("auth.bootstrap_admin.username/password are required when web.enabled=true")
		}
		if _, err := time.ParseDuration(c.Web.SessionTTL); err != nil {
			return fmt.Errorf("invalid web.session_ttl: %w", err)
		}
		if _, err := time.ParseDuration(c.Web.AccessTokenTTL); err != nil {
			return fmt.Errorf("invalid web.access_token_ttl: %w", err)
		}
		if _, err := time.ParseDuration(c.Web.RefreshTokenTTL); err != nil {
			return fmt.Errorf("invalid web.refresh_token_ttl: %w", err)
		}
	}

	if strings.TrimSpace(c.I18N.DefaultLocale) == "" {
		return errors.New("i18n.default_locale is required")
	}
	if strings.TrimSpace(c.I18N.FallbackLocale) == "" {
		return errors.New("i18n.fallback_locale is required")
	}
	if len(c.I18N.SupportedLocales) == 0 {
		return errors.New("i18n.supported_locales must not be empty")
	}
	localeSet := make(map[string]struct{}, len(c.I18N.SupportedLocales))
	for _, loc := range c.I18N.SupportedLocales {
		loc = strings.TrimSpace(loc)
		if loc == "" {
			return errors.New("i18n.supported_locales contains empty locale")
		}
		localeSet[loc] = struct{}{}
	}
	if _, ok := localeSet[c.I18N.DefaultLocale]; !ok {
		return errors.New("i18n.default_locale must exist in i18n.supported_locales")
	}
	if _, ok := localeSet[c.I18N.FallbackLocale]; !ok {
		return errors.New("i18n.fallback_locale must exist in i18n.supported_locales")
	}
	for _, src := range c.I18N.LocaleSourceOrder {
		switch src {
		case "query", "cookie", "header":
		default:
			return fmt.Errorf("invalid i18n.locale_source_order value: %s", src)
		}
	}

	if !c.Modules.Backup {
		return errors.New("modules.backup must be true")
	}

	for i, s := range c.Servers {
		prefix := fmt.Sprintf("servers[%d]", i)
		if strings.TrimSpace(s.Name) == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if strings.TrimSpace(s.Host) == "" {
			return fmt.Errorf("%s.host is required", prefix)
		}
		if s.Port < 1 || s.Port > 65535 {
			return fmt.Errorf("%s.port must be in [1,65535]", prefix)
		}
		if strings.TrimSpace(s.User) == "" {
			return fmt.Errorf("%s.user is required", prefix)
		}
		if strings.TrimSpace(s.KeyPath) == "" {
			return fmt.Errorf("%s.key_path is required", prefix)
		}
		if len(s.Paths) == 0 {
			return fmt.Errorf("%s.paths must not be empty", prefix)
		}
		for j, p := range s.Paths {
			if strings.TrimSpace(p) == "" {
				return fmt.Errorf("%s.paths[%d] must not be empty", prefix, j)
			}
		}
		if strings.TrimSpace(s.Rclone.Remote) == "" {
			return fmt.Errorf("%s.rclone.remote is required", prefix)
		}
		for j, f := range s.Rclone.Flags {
			if strings.TrimSpace(f) == "" {
				return fmt.Errorf("%s.rclone.flags[%d] must not be empty", prefix, j)
			}
		}
	}

	return nil
}

func (c *Config) GlobalTimeout() (time.Duration, error) {
	return time.ParseDuration(c.Global.Timeout)
}

func (c *Config) AccessTokenTTL() (time.Duration, error) {
	return time.ParseDuration(c.Web.AccessTokenTTL)
}

func (c *Config) RefreshTokenTTL() (time.Duration, error) {
	return time.ParseDuration(c.Web.RefreshTokenTTL)
}

func (c *Config) SessionTTL() (time.Duration, error) {
	return time.ParseDuration(c.Web.SessionTTL)
}

func (c *Config) FilterServers(names []string) []Server {
	if len(names) == 0 {
		out := make([]Server, len(c.Servers))
		copy(out, c.Servers)
		return out
	}
	set := make(map[string]struct{}, len(names))
	for _, name := range names {
		trimmed := strings.TrimSpace(name)
		if trimmed != "" {
			set[trimmed] = struct{}{}
		}
	}
	out := make([]Server, 0, len(set))
	for _, s := range c.Servers {
		if _, ok := set[s.Name]; ok {
			out = append(out, s)
		}
	}
	return out
}
