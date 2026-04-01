package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/config"
	i18n2 "github.com/rana-remote/rana-remote/internal/i18n"
	"github.com/rana-remote/rana-remote/internal/module"
	"github.com/rana-remote/rana-remote/internal/store"
	"github.com/rana-remote/rana-remote/internal/task"
)

type taskNotifier interface {
	NotifyExecution(ctx context.Context, ex store.Execution) error
}

type Server struct {
	cfg          *config.Config
	repo         store.Repository
	tokens       *auth.TokenManager
	tr           *i18n2.Translator
	registry     *module.Registry
	executor     task.Executor
	notifier     taskNotifier
	cfgPath      string
	loginLimiter *loginRateLimiter

	mu            sync.Mutex
	cancels       map[string]context.CancelFunc
	streams       map[string]map[chan store.ExecutionLog]struct{}
	idempotency   map[string]idempotencyRecord
	idempotencyMu sync.Mutex
}

func NewServer(cfg *config.Config, repo store.Repository, tokens *auth.TokenManager, tr *i18n2.Translator, registry *module.Registry, executor task.Executor) *Server {
	return &Server{
		cfg:          cfg,
		repo:         repo,
		tokens:       tokens,
		tr:           tr,
		registry:     registry,
		executor:     executor,
		loginLimiter: newLoginRateLimiter(cfg.Auth.LoginRateLimit),
		cancels:      make(map[string]context.CancelFunc),
		streams:      make(map[string]map[chan store.ExecutionLog]struct{}),
		idempotency:  make(map[string]idempotencyRecord),
	}
}

func (s *Server) SetConfigPath(path string) {
	s.cfgPath = strings.TrimSpace(path)
}

func (s *Server) SetNotifier(n taskNotifier) {
	s.notifier = n
}

func (s *Server) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", s.handleHealth)
	mux.HandleFunc("POST /api/v1/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/v1/auth/refresh", s.handleRefresh)

	authMiddleware := auth.Middleware(s.tokens)
	localeMiddleware := i18n2.Middleware(s.tr, s.cfg.I18N.LocaleSourceOrder)
	protected := chain(localeMiddleware, authMiddleware)

	mux.Handle("POST /api/v1/auth/logout", protected(http.HandlerFunc(s.handleLogout)))
	mux.Handle("GET /api/v1/me", protected(http.HandlerFunc(s.handleMe)))
	mux.Handle("PUT /api/v1/me/locale", protected(http.HandlerFunc(s.handleSetLocale)))
	mux.Handle("GET /api/v1/i18n/locales", protected(http.HandlerFunc(s.handleLocales)))
	mux.Handle("GET /api/v1/modules", protected(http.HandlerFunc(s.handleModules)))

	mux.Handle("GET /api/v1/servers", protected(authz(auth.PermServerRead, http.HandlerFunc(s.handleListServers))))
	mux.Handle("POST /api/v1/servers", protected(authz(auth.PermServerWrite, http.HandlerFunc(s.handleCreateServer))))
	mux.Handle("GET /api/v1/servers/{id}", protected(authz(auth.PermServerRead, http.HandlerFunc(s.handleGetServer))))
	mux.Handle("PUT /api/v1/servers/{id}", protected(authz(auth.PermServerWrite, http.HandlerFunc(s.handleUpdateServer))))
	mux.Handle("DELETE /api/v1/servers/{id}", protected(authz(auth.PermServerWrite, http.HandlerFunc(s.handleDeleteServer))))
	mux.Handle("POST /api/v1/servers/{id}/test-connection", protected(authz(auth.PermServerWrite, http.HandlerFunc(s.handleTestServerConnection))))

	mux.Handle("GET /api/v1/policies", protected(authz(auth.PermPolicyRead, http.HandlerFunc(s.handlePolicies))))
	mux.Handle("POST /api/v1/policies", protected(authz(auth.PermPolicyWrite, http.HandlerFunc(s.handlePolicies))))
	mux.Handle("GET /api/v1/policies/{id}", protected(authz(auth.PermPolicyRead, http.HandlerFunc(s.handlePolicyByID))))
	mux.Handle("PUT /api/v1/policies/{id}", protected(authz(auth.PermPolicyWrite, http.HandlerFunc(s.handlePolicyByID))))
	mux.Handle("DELETE /api/v1/policies/{id}", protected(authz(auth.PermPolicyWrite, http.HandlerFunc(s.handlePolicyByID))))

	mux.Handle("GET /api/v1/users", protected(authz(auth.PermUserWrite, http.HandlerFunc(s.handleUsers))))
	mux.Handle("POST /api/v1/users", protected(authz(auth.PermUserWrite, http.HandlerFunc(s.handleUsers))))
	mux.Handle("GET /api/v1/users/{id}", protected(authz(auth.PermUserWrite, http.HandlerFunc(s.handleUserByID))))
	mux.Handle("PUT /api/v1/users/{id}", protected(authz(auth.PermUserWrite, http.HandlerFunc(s.handleUserByID))))
	mux.Handle("DELETE /api/v1/users/{id}", protected(authz(auth.PermUserWrite, http.HandlerFunc(s.handleUserByID))))
	mux.Handle("PUT /api/v1/users/{id}/password", protected(authz(auth.PermUserWrite, http.HandlerFunc(s.handleUserPasswordByID))))
	mux.Handle("PUT /api/v1/me/password", protected(http.HandlerFunc(s.handleSetMyPassword)))

	mux.Handle("GET /api/v1/settings", protected(http.HandlerFunc(s.handleSettings)))
	mux.Handle("PUT /api/v1/settings", protected(authz(auth.PermSettingsWrite, http.HandlerFunc(s.handleSettings))))

	mux.Handle("POST /api/v1/executions", protected(authz(auth.PermExecutionWrite, http.HandlerFunc(s.handleCreateExecution))))
	mux.Handle("GET /api/v1/executions", protected(authz(auth.PermExecutionRead, http.HandlerFunc(s.handleListExecutions))))
	mux.Handle("GET /api/v1/executions/{id}", protected(authz(auth.PermExecutionRead, http.HandlerFunc(s.handleGetExecution))))
	mux.Handle("GET /api/v1/executions/{id}/logs", protected(authz(auth.PermExecutionRead, http.HandlerFunc(s.handleExecutionLogs))))
	mux.Handle("GET /api/v1/executions/{id}/stream", protected(authz(auth.PermExecutionRead, http.HandlerFunc(s.handleExecutionStream))))
	mux.Handle("POST /api/v1/executions/{id}/cancel", protected(authz(auth.PermExecutionWrite, http.HandlerFunc(s.handleCancelExecution))))
	mux.Handle("POST /api/v1/executions/{id}/retry", protected(authz(auth.PermExecutionWrite, http.HandlerFunc(s.handleRetryExecution))))

	mux.Handle("GET /api/v1/audit-logs", protected(authz(auth.PermAuditRead, http.HandlerFunc(s.handleAuditLogs))))

	if s.cfg.Modules.Schedule {
		mux.Handle("GET /api/v1/schedules", protected(authz(auth.PermExecutionRead, http.HandlerFunc(s.handleSchedules))))
		mux.Handle("POST /api/v1/schedules", protected(authz(auth.PermExecutionWrite, http.HandlerFunc(s.handleSchedules))))
		mux.Handle("GET /api/v1/schedules/{id}", protected(authz(auth.PermExecutionRead, http.HandlerFunc(s.handleScheduleByID))))
		mux.Handle("PUT /api/v1/schedules/{id}", protected(authz(auth.PermExecutionWrite, http.HandlerFunc(s.handleScheduleByID))))
		mux.Handle("DELETE /api/v1/schedules/{id}", protected(authz(auth.PermExecutionWrite, http.HandlerFunc(s.handleScheduleByID))))
	} else {
		mux.Handle("GET /api/v1/schedules", protected(http.HandlerFunc(s.handleFeatureDisabled)))
		mux.Handle("POST /api/v1/schedules", protected(http.HandlerFunc(s.handleFeatureDisabled)))
		mux.Handle("GET /api/v1/schedules/{id}", protected(http.HandlerFunc(s.handleFeatureDisabled)))
		mux.Handle("PUT /api/v1/schedules/{id}", protected(http.HandlerFunc(s.handleFeatureDisabled)))
		mux.Handle("DELETE /api/v1/schedules/{id}", protected(http.HandlerFunc(s.handleFeatureDisabled)))
	}

	handler := localeMiddleware(mux)
	handler = withTraceID(handler)
	handler = withRecovery(handler)
	handler = withIPAllowList(s.cfg.Web.IPAllowList, handler)
	handler = withCORS(s.cfg.Web.CORSAllowOrigins, handler)
	handler = withCSRFGate(s.cfg.Web.CSRFEnabled, handler)
	handler = withIdempotency(s, handler)
	handler = withRequestTimeoutLog(30*time.Second, handler)
	return handler
}

func authz(perm auth.Permission, next http.Handler) http.Handler {
	return auth.RequirePermission(perm)(next)
}

func chain(middlewares ...func(http.Handler) http.Handler) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		for i := len(middlewares) - 1; i >= 0; i-- {
			next = middlewares[i](next)
		}
		return next
	}
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "trace_id": traceIDFromRequest(r)})
}

func (s *Server) handleFeatureDisabled(w http.ResponseWriter, r *http.Request) {
	s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
}

func (s *Server) readJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("request body must contain a single JSON object")
	}
	return nil
}

func (s *Server) locale(r *http.Request) string {
	if user, ok := auth.UserFromContext(r.Context()); ok && user.Locale != "" {
		return s.tr.ResolveLocale(user.Locale)
	}
	loc := i18n2.LocaleFromContext(r.Context())
	if loc != "" {
		return loc
	}
	return s.tr.DefaultLocale()
}

func (s *Server) translate(r *http.Request, key string, params map[string]any) string {
	return s.tr.T(s.locale(r), key, params)
}

func (s *Server) writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	if traceID := w.Header().Get(traceHeaderName); traceID != "" {
		switch m := payload.(type) {
		case map[string]any:
			payload = withTraceIDPayload(m, traceID)
		case map[string]string:
			if _, exists := m["trace_id"]; !exists {
				m2 := make(map[string]any, len(m)+1)
				for k, v := range m {
					m2[k] = v
				}
				m2["trace_id"] = traceID
				payload = m2
			}
		}
	}
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code string, params map[string]any) {
	msg := s.translate(r, code, params)
	s.writeJSON(w, status, map[string]any{
		"code":    code,
		"message": msg,
	})
}

func splitCSV(v string) []string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}
