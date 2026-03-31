package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rana-remote/rana-remote/internal/api"
	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/config"
	i18n2 "github.com/rana-remote/rana-remote/internal/i18n"
	"github.com/rana-remote/rana-remote/internal/module"
	"github.com/rana-remote/rana-remote/internal/notify"
	"github.com/rana-remote/rana-remote/internal/scheduler"
	"github.com/rana-remote/rana-remote/internal/security"
	"github.com/rana-remote/rana-remote/internal/store"
)

func main() {
	cfgPath := flag.String("c", "config.yaml", "path to config file")
	listen := flag.String("listen", "", "http listen address override")
	basePath := flag.String("base-path", "", "base path override")
	migrate := flag.Bool("migrate", false, "run migrations on startup")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if *listen != "" {
		cfg.Web.Listen = *listen
	}
	if *basePath != "" {
		cfg.Web.BasePath = *basePath
	}
	cfg.Web.BasePath = normalizeBasePath(cfg.Web.BasePath)

	if !cfg.Web.Enabled {
		log.Fatalf("web.enabled=false, rana-api requires web.enabled=true")
	}

	bundle := i18n2.NewBundle()
	_ = bundle.LoadDir(filepath.Join("web", "app", "locales"))
	translator := i18n2.NewTranslator(bundle, cfg.I18N)

	repo, cleanup, err := initRepository(cfg)
	if err != nil {
		log.Fatalf("init repository: %v", err)
	}
	defer cleanup()

	if *migrate {
		if migrator, ok := repo.(interface {
			Migrate(ctx context.Context) error
		}); ok {
			if err := migrator.Migrate(context.Background()); err != nil {
				log.Fatalf("run migration: %v", err)
			}
		}
	}

	if err := seedServers(context.Background(), repo, cfg.Servers); err != nil {
		log.Fatalf("seed servers: %v", err)
	}
	if err := ensureAdmin(context.Background(), repo, cfg); err != nil {
		log.Fatalf("ensure admin: %v", err)
	}

	accessTTL, _ := cfg.AccessTokenTTL()
	refreshTTL, _ := cfg.RefreshTokenTTL()
	jwtSecret := os.Getenv("RANA_JWT_SECRET")
	if jwtSecret == "" {
		jwtSecret = "rana-dev-secret-change-me"
		log.Printf("WARN: RANA_JWT_SECRET not set; using development secret")
	}
	tokens, err := auth.NewTokenManager(jwtSecret, accessTTL, refreshTTL)
	if err != nil {
		log.Fatalf("init token manager: %v", err)
	}

	registry := module.NewRegistry()
	_ = registry.Register(module.BasicModule{ModuleName: "backup", OnEnabled: cfg.Modules.Backup})
	_ = registry.Register(module.BasicModule{ModuleName: "policy", OnEnabled: cfg.Modules.Backup})
	_ = registry.Register(module.BasicModule{ModuleName: "schedule", OnEnabled: cfg.Modules.Schedule})
	_ = registry.Register(module.BasicModule{ModuleName: "audit", OnEnabled: cfg.Modules.Audit})
	_ = registry.Register(module.BasicModule{ModuleName: "notify", OnEnabled: cfg.Modules.Notify})
	_ = registry.Register(module.BasicModule{ModuleName: "users", OnEnabled: cfg.Modules.Users})
	_ = registry.Register(module.BasicModule{ModuleName: "i18n", OnEnabled: true})

	apiServer := api.NewServer(cfg, repo, tokens, translator, registry, nil)
	apiServer.SetConfigPath(*cfgPath)
	if cfg.Modules.Notify {
		apiServer.SetNotifier(notify.NewWebhookNotifier(&cfg.Notify))
	}
	schedRunner := scheduler.NewRunner(repo, func(ctx context.Context, schedule store.Schedule) error {
		exec, err := apiServer.TriggerExecution(ctx, schedule.PolicyID, schedule.ServerNames, "schedule")
		if err != nil {
			return err
		}
		if cfg.Modules.Audit {
			_, _ = repo.CreateAuditLog(ctx, store.AuditLog{
				ActorID:      "system",
				Action:       "schedule.trigger",
				ResourceType: "schedule",
				ResourceID:   schedule.ID,
				Diff:         fmt.Sprintf("execution_id=%s", exec.ID),
			})
		}
		return nil
	})
	_ = registry.Register(module.BasicModule{ModuleName: "scheduler-runtime", OnEnabled: cfg.Modules.Schedule, StartFn: schedRunner.Start, StopFn: schedRunner.Stop})

	handler := apiServer.Router()
	handler = withStaticFallback(handler, filepath.Join("web", "dist"))
	if cfg.Web.BasePath != "/" {
		handler = http.StripPrefix(cfg.Web.BasePath, handler)
		handler = withBasePath(cfg.Web.BasePath, handler)
	}

	httpServer := &http.Server{
		Addr:              cfg.Web.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := registry.StartAll(ctx); err != nil {
		log.Fatalf("start modules: %v", err)
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = registry.StopAll(shutdownCtx)
		_ = httpServer.Shutdown(shutdownCtx)
	}()

	log.Printf("rana-api listening on %s", cfg.Web.Listen)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http serve: %v", err)
	}
}

func ensureAdmin(ctx context.Context, repo store.Repository, cfg *config.Config) error {
	_, err := repo.GetUserByUsername(ctx, cfg.Auth.BootstrapAdmin.Username)
	if err == nil {
		return nil
	}
	hashed, err := auth.HashPassword(cfg.Auth.BootstrapAdmin.Password)
	if err != nil {
		return fmt.Errorf("hash bootstrap admin password: %w", err)
	}
	_, err = repo.CreateUser(ctx, store.User{
		Username:     cfg.Auth.BootstrapAdmin.Username,
		PasswordHash: hashed,
		Role:         store.RoleAdmin,
		Locale:       cfg.I18N.DefaultLocale,
	})
	if err != nil {
		return fmt.Errorf("create bootstrap admin: %w", err)
	}
	return nil
}

func seedServers(ctx context.Context, repo store.Repository, servers []config.Server) error {
	converted := make([]store.Server, 0, len(servers))
	for _, s := range servers {
		converted = append(converted, store.Server{
			Name:         s.Name,
			Host:         s.Host,
			Port:         s.Port,
			User:         s.User,
			KeyPath:      s.KeyPath,
			Passphrase:   encryptedPassphrase(s.Passphrase),
			Enabled:      true,
			Paths:        append([]string(nil), s.Paths...),
			RcloneRemote: s.Rclone.Remote,
			RcloneFlags:  append([]string(nil), s.Rclone.Flags...),
		})
	}
	return repo.SeedServers(ctx, converted)
}

func encryptedPassphrase(raw string) string {
	enc, err := security.EncryptIfConfigured(raw)
	if err != nil {
		return raw
	}
	return enc
}

func withBasePath(basePath string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != basePath && !strings.HasPrefix(r.URL.Path, basePath+"/") {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withStaticFallback(next http.Handler, distDir string) http.Handler {
	fs := http.FileServer(http.Dir(distDir))
	indexPath := filepath.Join(distDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(r.URL.Path) > 4 && (r.URL.Path[:4] == "/api" || r.URL.Path == "/healthz") {
			next.ServeHTTP(w, r)
			return
		}
		if _, err := os.Stat(filepath.Join(distDir, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))); err == nil {
			fs.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/" || r.URL.Path == "" || r.URL.Path == "/index.html" {
			if _, err := os.Stat(indexPath); err == nil {
				fs.ServeHTTP(w, r)
				return
			}
		}
		if _, err := os.Stat(indexPath); err == nil {
			r2 := *r
			r2.URL = newCopyURL(r.URL)
			r2.URL.Path = "/"
			fs.ServeHTTP(w, &r2)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func normalizeBasePath(path string) string {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" || trimmed == "/" {
		return "/"
	}
	trimmed = "/" + strings.Trim(trimmed, "/")
	return trimmed
}

func newCopyURL(u *url.URL) *url.URL {
	copy := *u
	return &copy
}

func initRepository(cfg *config.Config) (store.Repository, func(), error) {
	switch cfg.Database.Driver {
	case "", "memory":
		return store.NewMemoryRepository(), func() {}, nil
	case "sqlite":
		repo, err := store.NewSQLiteRepository(cfg.Database.DSN)
		if err != nil {
			return nil, nil, err
		}
		return repo, func() {
			_ = repo.Close()
		}, nil
	default:
		return nil, nil, fmt.Errorf("unsupported database driver: %s", cfg.Database.Driver)
	}
}
