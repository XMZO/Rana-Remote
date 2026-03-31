package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/rana-remote/rana-remote/internal/auth"
)

type traceIDContextKey struct{}

const traceHeaderName = "X-Trace-Id"

type idempotencyRecord struct {
	Status    int
	Body      []byte
	CreatedAt time.Time
}

func withTraceID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		traceID := strings.TrimSpace(r.Header.Get(traceHeaderName))
		if traceID == "" {
			traceID = uuid.NewString()
		}
		w.Header().Set(traceHeaderName, traceID)
		ctx := context.WithValue(r.Context(), traceIDContextKey{}, traceID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func traceIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(traceIDContextKey{}).(string)
	return v
}

func withCORS(allowOrigins []string, next http.Handler) http.Handler {
	allowed := make(map[string]struct{}, len(allowOrigins))
	for _, origin := range allowOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			allowed[origin] = struct{}{}
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" {
			if len(allowed) == 0 {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			} else if _, ok := allowed[origin]; ok {
				w.Header().Set("Access-Control-Allow-Origin", origin)
			}
			w.Header().Set("Access-Control-Allow-Credentials", "true")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, Idempotency-Key, X-CSRF-Token, X-Trace-Id")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		}

		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withIPAllowList(allowList []string, next http.Handler) http.Handler {
	exact := make(map[string]struct{})
	cidrs := make([]*net.IPNet, 0)
	for _, raw := range allowList {
		item := strings.TrimSpace(raw)
		if item == "" {
			continue
		}
		if ip := net.ParseIP(item); ip != nil {
			exact[ip.String()] = struct{}{}
			continue
		}
		if _, subnet, err := net.ParseCIDR(item); err == nil {
			cidrs = append(cidrs, subnet)
		}
	}
	if len(exact) == 0 && len(cidrs) == 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := net.ParseIP(remoteIP(r))
		if ip == nil {
			writeSimpleJSONError(w, http.StatusForbidden, "auth.forbidden", "ip not allowed")
			return
		}
		if _, ok := exact[ip.String()]; ok {
			next.ServeHTTP(w, r)
			return
		}
		for _, subnet := range cidrs {
			if subnet.Contains(ip) {
				next.ServeHTTP(w, r)
				return
			}
		}
		writeSimpleJSONError(w, http.StatusForbidden, "auth.forbidden", "ip not allowed")
	})
}

func withCSRFGate(enabled bool, next http.Handler) http.Handler {
	if !enabled {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) || isCSRFIgnoredPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}
		cookie, err := r.Cookie("rana_csrf")
		if err != nil || strings.TrimSpace(cookie.Value) == "" {
			writeSimpleJSONError(w, http.StatusForbidden, "csrf.invalid", "missing csrf cookie")
			return
		}
		token := strings.TrimSpace(r.Header.Get("X-CSRF-Token"))
		if token == "" || token != cookie.Value {
			writeSimpleJSONError(w, http.StatusForbidden, "csrf.invalid", "invalid csrf token")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func withIdempotency(s *Server, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isSafeMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}
		key := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
		if key == "" {
			next.ServeHTTP(w, r)
			return
		}

		userID := "anonymous"
		if user, ok := auth.UserFromContext(r.Context()); ok {
			userID = user.ID
		}
		cacheKey := userID + "|" + r.Method + "|" + r.URL.Path + "|" + key

		if rec, ok := s.getIdempotency(cacheKey); ok {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("X-Idempotent-Replay", "1")
			w.WriteHeader(rec.Status)
			_, _ = w.Write(rec.Body)
			return
		}

		rw := newCaptureWriter(w)
		next.ServeHTTP(rw, r)
		if rw.status >= 200 && rw.status < 300 {
			s.setIdempotency(cacheKey, idempotencyRecord{
				Status:    rw.status,
				Body:      rw.body.Bytes(),
				CreatedAt: time.Now().UTC(),
			})
		}
	})
}

func withRequestTimeoutLog(_ time.Duration, next http.Handler) http.Handler {
	return next
}

func (s *Server) getIdempotency(key string) (idempotencyRecord, bool) {
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	now := time.Now().UTC()
	for k, v := range s.idempotency {
		if now.Sub(v.CreatedAt) > 24*time.Hour {
			delete(s.idempotency, k)
		}
	}
	rec, ok := s.idempotency[key]
	return rec, ok
}

func (s *Server) setIdempotency(key string, rec idempotencyRecord) {
	s.idempotencyMu.Lock()
	defer s.idempotencyMu.Unlock()
	s.idempotency[key] = rec
}

type captureWriter struct {
	http.ResponseWriter
	status int
	body   bytes.Buffer
}

func newCaptureWriter(w http.ResponseWriter) *captureWriter {
	return &captureWriter{ResponseWriter: w, status: http.StatusOK}
}

func (w *captureWriter) WriteHeader(statusCode int) {
	w.status = statusCode
	w.ResponseWriter.WriteHeader(statusCode)
}

func (w *captureWriter) Write(p []byte) (int, error) {
	w.body.Write(p)
	return w.ResponseWriter.Write(p)
}

func writeSimpleJSONError(w http.ResponseWriter, status int, code, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "message": msg})
}

func isSafeMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

func isCSRFIgnoredPath(path string) bool {
	return path == "/api/v1/auth/login" || path == "/api/v1/auth/refresh" || path == "/healthz"
}

type loginRateLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	records map[string][]time.Time
}

func newLoginRateLimiter(spec string) *loginRateLimiter {
	limit := 5
	window := time.Minute
	raw := strings.TrimSpace(spec)
	if raw != "" {
		parts := strings.Split(raw, "/")
		if len(parts) == 2 {
			if n := strings.TrimSpace(parts[0]); n != "" {
				if parsed := parsePositiveInt(n, 0); parsed > 0 {
					limit = parsed
				}
			}
			switch strings.TrimSpace(parts[1]) {
			case "s":
				window = time.Second
			case "m":
				window = time.Minute
			case "h":
				window = time.Hour
			}
		}
	}
	return &loginRateLimiter{
		limit:   limit,
		window:  window,
		records: make(map[string][]time.Time),
	}
}

func (l *loginRateLimiter) Allow(key string) bool {
	if l == nil || l.limit <= 0 {
		return true
	}
	now := time.Now().UTC()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	entries := l.records[key]
	filtered := entries[:0]
	for _, ts := range entries {
		if ts.After(cutoff) {
			filtered = append(filtered, ts)
		}
	}
	if len(filtered) >= l.limit {
		l.records[key] = filtered
		return false
	}
	filtered = append(filtered, now)
	l.records[key] = filtered
	return true
}

func remoteIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}
