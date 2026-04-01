package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"runtime/debug"
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
	defer func() {
		if r := recover(); r != nil {
			log.Printf("FATAL: panic in main: %v\n%s", r, debug.Stack())
			os.Exit(1)
		}
	}()

	cfgPath := flag.String("c", "/data/config.yaml", "path to config file")
	listen := flag.String("listen", "", "http listen address override")
	basePath := flag.String("base-path", "", "base path override")
	migrate := flag.Bool("migrate", false, "run migrations on startup")
	flag.Parse()

	cfg, created, err := config.LoadOrInit(*cfgPath)
	if err != nil {
		log.Fatalf("load config: %v", err)
	}
	if created {
		log.Printf("startup: initialized default config at %s", *cfgPath)
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
	log.Printf("startup: config loaded listen=%s base_path=%s db_driver=%s", cfg.Web.Listen, cfg.Web.BasePath, cfg.Database.Driver)

	bundle := i18n2.NewBundle()
	_ = bundle.LoadDir(filepath.Join("web", "app", "locales"))
	translator := i18n2.NewTranslator(bundle, cfg.I18N)

	repo, cleanup, err := initRepository(cfg)
	if err != nil {
		log.Fatalf("init repository: %v", err)
	}
	defer cleanup()
	log.Printf("startup: repository initialized")

	if *migrate {
		if migrator, ok := repo.(interface{ Migrate(ctx context.Context) error }); ok {
			log.Printf("startup: running migrations")
			if err := migrator.Migrate(context.Background()); err != nil {
				log.Fatalf("run migration: %v", err)
			}
		}
	}

	if err := seedServers(context.Background(), repo, cfg.Servers); err != nil {
		log.Fatalf("seed servers: %v", err)
	}
	log.Printf("startup: seeded %d configured servers", len(cfg.Servers))

	if err := ensureAdmin(context.Background(), repo, cfg); err != nil {
		log.Fatalf("ensure admin: %v", err)
	}
	log.Printf("startup: bootstrap admin ensured user=%s", cfg.Auth.BootstrapAdmin.Username)

	if err := loadSettingsFromDB(context.Background(), repo, cfg); err != nil {
		log.Printf("WARN: startup: failed to load settings from DB: %v (using config defaults)", err)
	} else {
		log.Printf("startup: runtime settings synchronized from database if present")
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
	log.Printf("startup: token manager initialized access_ttl=%s refresh_ttl=%s", accessTTL, refreshTTL)

	registry := module.NewRegistry()
	mustRegisterModule(registry, module.BasicModule{ModuleName: "backup", OnEnabled: cfg.Modules.Backup})
	mustRegisterModule(registry, module.BasicModule{ModuleName: "policy", OnEnabled: cfg.Modules.Backup})
	mustRegisterModule(registry, module.BasicModule{ModuleName: "schedule", OnEnabled: cfg.Modules.Schedule})
	mustRegisterModule(registry, module.BasicModule{ModuleName: "audit", OnEnabled: cfg.Modules.Audit})
	mustRegisterModule(registry, module.BasicModule{ModuleName: "notify", OnEnabled: cfg.Modules.Notify})
	mustRegisterModule(registry, module.BasicModule{ModuleName: "users", OnEnabled: cfg.Modules.Users})
	mustRegisterModule(registry, module.BasicModule{ModuleName: "i18n", OnEnabled: true})

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
	mustRegisterModule(registry, module.BasicModule{
		ModuleName: "scheduler-runtime",
		OnEnabled:  cfg.Modules.Schedule,
		StartFn: func(ctx context.Context) error {
			log.Printf("startup: scheduler runtime launching asynchronously")
			go func() {
				defer func() {
					if r := recover(); r != nil {
						log.Printf("FATAL: scheduler runtime panic: %v\n%s", r, debug.Stack())
					}
				}()
				if err := schedRunner.Start(ctx); err != nil {
					log.Printf("scheduler runtime exited with error: %v", err)
					return
				}
				log.Printf("scheduler runtime exited cleanly")
			}()
			return nil
		},
		StopFn: schedRunner.Stop,
	})

	handler := apiServer.Router()
	handler = withPanicRecovery(handler)
	handler = withRequestLog(handler)
	handler = withStaticFallback(handler, filepath.Join("web", "dist"))
	if cfg.Web.BasePath != "/" {
		handler = http.StripPrefix(cfg.Web.BasePath, handler)
		handler = withBasePath(cfg.Web.BasePath, handler)
	}

	httpServer := &http.Server{
		Addr:              cfg.Web.Listen,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ErrorLog:          log.New(os.Stderr, "http-server: ", log.LstdFlags),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	log.Printf("startup: starting modules=%v", registry.Names())
	if err := registry.StartAll(ctx); err != nil {
		log.Fatalf("start modules: %v", err)
	}
	log.Printf("startup: modules started successfully")

	go func() {
		<-ctx.Done()
		log.Printf("shutdown: signal received, stopping modules and http server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := registry.StopAll(shutdownCtx); err != nil {
			log.Printf("shutdown: stop modules: %v", err)
		}
		if err := httpServer.Shutdown(shutdownCtx); err != nil {
			log.Printf("shutdown: http server: %v", err)
		}
	}()

	log.Printf("startup: health endpoint ready at %s%s/healthz", cfg.Web.Listen, cfg.Web.BasePath)
	log.Printf("rana-api listening on %s", cfg.Web.Listen)
	if err := httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("http serve: %v", err)
	}
	log.Printf("shutdown: rana-api stopped")
}

func mustRegisterModule(registry *module.Registry, m module.Module) {
	if err := registry.Register(m); err != nil {
		log.Fatalf("register module %q: %v", m.Name(), err)
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

type statusRecorder struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusRecorder) WriteHeader(statusCode int) {
	if w.wroteHeader {
		return
	}
	w.status = statusCode
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *statusRecorder) Write(p []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusRecorder) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func (w *statusRecorder) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	hj, ok := w.ResponseWriter.(http.Hijacker)
	if !ok {
		return nil, nil, fmt.Errorf("response writer does not support hijacking")
	}
	return hj.Hijack()
}

func withPanicRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("PANIC: %s %s remote=%s err=%v\n%s", r.Method, r.URL.String(), r.RemoteAddr, rec, debug.Stack())
				if !rw.wroteHeader {
					http.Error(rw, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
				}
			}
		}()
		next.ServeHTTP(rw, r)
	})
}

func withRequestLog(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		rw := &statusRecorder{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(rw, r)
		log.Printf("HTTP %s %s status=%d duration=%s remote=%s ua=%q", r.Method, r.URL.String(), rw.status, time.Since(started).Round(time.Millisecond), r.RemoteAddr, r.UserAgent())
	})
}

func withStaticFallback(next http.Handler, distDir string) http.Handler {
	fs := http.FileServer(http.Dir(distDir))
	indexPath := filepath.Join(distDir, "index.html")
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/api/") {
			next.ServeHTTP(w, r)
			return
		}
		assetPath := filepath.Join(distDir, filepath.Clean(strings.TrimPrefix(r.URL.Path, "/")))
		if _, err := os.Stat(assetPath); err == nil {
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

// loadSettingsFromDB loads runtime settings from the database and syncs them to the
// in-memory config. This ensures that settings persisted via PUT /api/v1/settings
// survive across restarts.
func loadSettingsFromDB(ctx context.Context, repo store.Repository, cfg *config.Config) error {
	settings, err := repo.GetSettings(ctx)
	if err != nil {
		return fmt.Errorf("get settings: %w", err)
	}
	if settings.UpdatedAt.IsZero() {
		return nil
	}

	cfg.Global.Timeout = settings.GlobalTimeout
	cfg.Global.Concurrency = settings.GlobalConcurrency
	cfg.Global.SSH.StrictHostKey = settings.GlobalSSHStrictHostKey
	cfg.Global.SSH.KnownHostsPath = settings.GlobalSSHKnownHostsPath
	cfg.Web.CSRFEnabled = settings.WebCSRFEnabled
	cfg.Web.CORSAllowOrigins = settings.WebCORSAllowOrigins
	cfg.Web.IPAllowList = settings.WebIPAllowList
	cfg.Modules.Schedule = settings.ModulesSchedule
	cfg.Modules.Audit = settings.ModulesAudit
	cfg.Modules.Notify = settings.ModulesNotify
	cfg.Modules.Users = settings.ModulesUsers
	cfg.I18N.DefaultLocale = settings.I18NDefaultLocale
	cfg.Notify.WebhookURL = settings.NotifyWebhookURL
	cfg.Notify.Email.Enabled = settings.NotifyEmailEnabled
	cfg.Notify.Email.SMTPHost = settings.NotifyEmailSMTPHost
	cfg.Notify.Email.SMTPPort = settings.NotifyEmailSMTPPort
	cfg.Notify.Email.Username = settings.NotifyEmailUsername
	cfg.Notify.Email.Password = settings.NotifyEmailPassword
	cfg.Notify.Email.From = settings.NotifyEmailFrom
	cfg.Notify.Email.To = settings.NotifyEmailTo
	cfg.Notify.Email.UseTLS = settings.NotifyEmailUseTLS
	cfg.Notify.OnSuccess = settings.NotifyOnSuccess
	cfg.Notify.OnFailure = settings.NotifyOnFailure
	cfg.Notify.SuppressionWindow = settings.NotifySuppressionWindow
	return nil
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
