package api

import (
	"net/http"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	i18n2 "github.com/rana-remote/rana-remote/internal/i18n"
	"github.com/rana-remote/rana-remote/internal/store"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

type setLocaleRequest struct {
	Locale string `json:"locale"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	user, err := s.repo.GetUserByUsername(r.Context(), req.Username)
	if err != nil || !auth.CheckPassword(user.PasswordHash, req.Password) {
		s.writeError(w, r, http.StatusUnauthorized, "auth.invalid_credentials", nil)
		return
	}
	if user.Locale == "" {
		user.Locale = s.tr.DefaultLocale()
	}

	now := time.Now().UTC()
	tokens, err := s.tokens.Generate(user.ID, string(user.Role), user.Locale, now)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	_ = s.repo.UpdateUserLogin(r.Context(), user.ID, now)

	if s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{ActorID: user.ID, Action: "login", ResourceType: "auth"})
	}

	http.SetCookie(w, &http.Cookie{
		Name:     i18n2.LocaleCookieName,
		Value:    user.Locale,
		Path:     "/",
		HttpOnly: false,
		SameSite: http.SameSiteLaxMode,
	})

	s.writeJSON(w, http.StatusOK, map[string]any{
		"code":    "ok",
		"message": s.translate(r, "ok", nil),
		"data":    tokens,
	})
}

func (s *Server) handleRefresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	claims, err := s.tokens.Parse(req.RefreshToken, "refresh")
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, "auth.unauthorized", nil)
		return
	}
	user, err := s.repo.GetUserByID(r.Context(), claims.UserID)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, "auth.unauthorized", nil)
		return
	}
	if user.Locale == "" {
		user.Locale = s.tr.DefaultLocale()
	}
	tokens, err := s.tokens.Generate(user.ID, string(user.Role), user.Locale, time.Now().UTC())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil), "data": tokens})
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{ActorID: user.ID, Action: "logout", ResourceType: "auth"})
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		s.writeError(w, r, http.StatusUnauthorized, "auth.unauthorized", nil)
		return
	}
	user, err := s.repo.GetUserByID(r.Context(), ctxUser.ID)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, "auth.unauthorized", nil)
		return
	}
	user.PasswordHash = ""
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil), "data": user})
}

func (s *Server) handleSetLocale(w http.ResponseWriter, r *http.Request) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		s.writeError(w, r, http.StatusUnauthorized, "auth.unauthorized", nil)
		return
	}
	var req setLocaleRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	if !s.tr.IsSupported(req.Locale) {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	if err := s.repo.UpdateUserLocale(r.Context(), ctxUser.ID, req.Locale); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: i18n2.LocaleCookieName, Value: req.Locale, Path: "/", SameSite: http.SameSiteLaxMode})
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "locale.updated", nil)})
}

func (s *Server) handleLocales(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": s.tr.SupportedLocales()})
}

func (s *Server) handleModules(w http.ResponseWriter, _ *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": s.registry.Status()})
}
