package api

import (
	"net/http"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/store"
)

type upsertUserRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Locale   string `json:"locale"`
	Password string `json:"password,omitempty"`
}

type setPasswordRequest struct {
	OldPassword string `json:"old_password,omitempty"`
	NewPassword string `json:"new_password"`
}

func (s *Server) handleUsers(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Users {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		users, err := s.repo.ListUsers(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		roleFilter := strings.TrimSpace(r.URL.Query().Get("role"))
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		filtered := make([]store.User, 0, len(users))
		for _, u := range users {
			if roleFilter != "" && string(u.Role) != roleFilter {
				continue
			}
			if q != "" && !strings.Contains(strings.ToLower(u.Username), q) {
				continue
			}
			u.PasswordHash = ""
			filtered = append(filtered, u)
		}
		page := parsePageSpec(r)
		paged, meta := paginate(filtered, page)
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": paged, "meta": meta})
	case http.MethodPost:
		var req upsertUserRequest
		if err := s.readJSON(r, &req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
			return
		}
		role := normalizeRole(req.Role)
		if role == "" {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "invalid role"})
			return
		}
		if err := s.validatePasswordPolicy(req.Password); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		if !s.tr.IsSupported(req.Locale) {
			req.Locale = s.tr.DefaultLocale()
		}
		hashed, err := auth.HashPassword(req.Password)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		user := store.User{
			Username:     strings.TrimSpace(req.Username),
			PasswordHash: hashed,
			Role:         role,
			Locale:       req.Locale,
			CreatedAt:    time.Now().UTC(),
		}
		created, err := s.repo.CreateUser(r.Context(), user)
		if err != nil {
			if err == store.ErrAlreadyExists {
				s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "username already exists"})
				return
			}
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		created.PasswordHash = ""
		if actor, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
				ActorID:      actor.ID,
				Action:       "user.create",
				ResourceType: "user",
				ResourceID:   created.ID,
				IP:           remoteIP(r),
				Diff:         "trace_id=" + traceIDFromContext(r.Context()),
			})
		}
		s.writeJSON(w, http.StatusCreated, map[string]any{"code": "ok", "data": created})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUserByID(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Users {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "id is required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		user, err := s.repo.GetUserByID(r.Context(), id)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "user not found"})
			return
		}
		user.PasswordHash = ""
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": user})
	case http.MethodPut:
		current, err := s.repo.GetUserByID(r.Context(), id)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "user not found"})
			return
		}
		var req upsertUserRequest
		if err := s.readJSON(r, &req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
			return
		}
		if strings.TrimSpace(req.Username) != "" {
			current.Username = strings.TrimSpace(req.Username)
		}
		if role := normalizeRole(req.Role); role != "" {
			current.Role = role
		}
		if strings.TrimSpace(req.Locale) != "" && s.tr.IsSupported(req.Locale) {
			current.Locale = req.Locale
		}
		if err := s.repo.UpdateUser(r.Context(), current); err != nil {
			if err == store.ErrAlreadyExists {
				s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "username already exists"})
				return
			}
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		current.PasswordHash = ""
		if actor, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
				ActorID:      actor.ID,
				Action:       "user.update",
				ResourceType: "user",
				ResourceID:   id,
				IP:           remoteIP(r),
				Diff:         "trace_id=" + traceIDFromContext(r.Context()),
			})
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": current})
	case http.MethodDelete:
		ctxUser, _ := auth.UserFromContext(r.Context())
		if ctxUser.ID == id {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "cannot delete current user"})
			return
		}
		users, err := s.repo.ListUsers(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		adminCount := 0
		targetRole := store.RoleViewer
		for _, u := range users {
			if u.Role == store.RoleAdmin {
				adminCount++
			}
			if u.ID == id {
				targetRole = u.Role
			}
		}
		if targetRole == store.RoleAdmin && adminCount <= 1 {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "cannot delete last admin"})
			return
		}
		if err := s.repo.DeleteUser(r.Context(), id); err != nil {
			if err == store.ErrNotFound {
				s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "user not found"})
				return
			}
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		if actor, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
				ActorID:      actor.ID,
				Action:       "user.delete",
				ResourceType: "user",
				ResourceID:   id,
				IP:           remoteIP(r),
				Diff:         "trace_id=" + traceIDFromContext(r.Context()),
			})
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleUserPasswordByID(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Users {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "id is required"})
		return
	}
	var req setPasswordRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	if err := s.validatePasswordPolicy(req.NewPassword); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}
	hashed, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	if err := s.repo.UpdateUserPassword(r.Context(), id, hashed); err != nil {
		if err == store.ErrNotFound {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "user not found"})
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	if actor, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
			ActorID:      actor.ID,
			Action:       "user.password.reset",
			ResourceType: "user",
			ResourceID:   id,
			IP:           remoteIP(r),
			Diff:         "trace_id=" + traceIDFromContext(r.Context()),
		}))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
}

func (s *Server) handleSetMyPassword(w http.ResponseWriter, r *http.Request) {
	ctxUser, ok := auth.UserFromContext(r.Context())
	if !ok {
		s.writeError(w, r, http.StatusUnauthorized, "auth.unauthorized", nil)
		return
	}
	var req setPasswordRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	if err := s.validatePasswordPolicy(req.NewPassword); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}

	user, err := s.repo.GetUserByID(r.Context(), ctxUser.ID)
	if err != nil {
		s.writeError(w, r, http.StatusUnauthorized, "auth.unauthorized", nil)
		return
	}
	if !auth.CheckPassword(user.PasswordHash, req.OldPassword) {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "old password mismatch"})
		return
	}
	hashed, err := auth.HashPassword(req.NewPassword)
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	if err := s.repo.UpdateUserPassword(r.Context(), user.ID, hashed); err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	if s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
			ActorID:      user.ID,
			Action:       "user.password.change",
			ResourceType: "user",
			ResourceID:   user.ID,
			IP:           remoteIP(r),
			Diff:         "trace_id=" + traceIDFromContext(r.Context()),
		}))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
}

func normalizeRole(role string) store.Role {
	switch strings.ToLower(strings.TrimSpace(role)) {
	case "admin":
		return store.RoleAdmin
	case "operator":
		return store.RoleOperator
	case "viewer":
		return store.RoleViewer
	default:
		return ""
	}
}

func (s *Server) validatePasswordPolicy(password string) error {
	password = strings.TrimSpace(password)
	if len(password) < s.cfg.Auth.PasswordPolicy.MinLength {
		return errInvalidUser("password too short")
	}
	hasDigit := false
	hasSpecial := false
	for _, ch := range password {
		if ch >= '0' && ch <= '9' {
			hasDigit = true
		}
		if strings.ContainsRune("!@#$%^&*()-_=+[]{};:'\",.<>/?\\|`~", ch) {
			hasSpecial = true
		}
	}
	if s.cfg.Auth.PasswordPolicy.RequireNumber && !hasDigit {
		return errInvalidUser("password must contain number")
	}
	if s.cfg.Auth.PasswordPolicy.RequireSpecial && !hasSpecial {
		return errInvalidUser("password must contain special char")
	}
	return nil
}

type errInvalidUser string

func (e errInvalidUser) Error() string {
	return string(e)
}
