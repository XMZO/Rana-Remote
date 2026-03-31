package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/store"
)

type upsertPolicyRequest struct {
	Name         string   `json:"name"`
	ServerNames  []string `json:"server_names"`
	Paths        []string `json:"paths"`
	RcloneRemote string   `json:"rclone_remote"`
	RcloneFlags  []string `json:"rclone_flags"`
	TimeoutSec   int      `json:"timeout_sec"`
	RetryLimit   int      `json:"retry_limit"`
	Enabled      *bool    `json:"enabled"`
}

func (s *Server) handlePolicies(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		items, err := s.repo.ListPolicies(r.Context())
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		enabledRaw := strings.TrimSpace(r.URL.Query().Get("enabled"))
		hasEnabledFilter := false
		enabledFilter := false
		if enabledRaw != "" {
			parsed, parseErr := strconv.ParseBool(enabledRaw)
			if parseErr != nil {
				s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "enabled must be true/false"})
				return
			}
			hasEnabledFilter = true
			enabledFilter = parsed
		}
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		filtered := make([]store.Policy, 0, len(items))
		for _, p := range items {
			if hasEnabledFilter && p.Enabled != enabledFilter {
				continue
			}
			if q != "" {
				corpus := strings.ToLower(p.Name + " " + p.RcloneRemote + " " + strings.Join(p.ServerNames, " "))
				if !strings.Contains(corpus, q) {
					continue
				}
			}
			filtered = append(filtered, p)
		}
		page := parsePageSpec(r)
		paged, meta := paginate(filtered, page)
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": paged, "meta": meta})
	case http.MethodPost:
		var req upsertPolicyRequest
		if err := s.readJSON(r, &req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
			return
		}
		item, err := buildPolicyRequest(req, store.Policy{}, true)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		if err := s.validatePolicyServers(r.Context(), item.ServerNames); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		created, err := s.repo.CreatePolicy(r.Context(), item)
		if err != nil {
			if err == store.ErrAlreadyExists {
				s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "policy name already exists"})
				return
			}
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{
				ActorID:      user.ID,
				Action:       "policy.create",
				ResourceType: "policy",
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

func (s *Server) handlePolicyByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "id is required"})
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.repo.GetPolicy(r.Context(), id)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "policy not found"})
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": item})
	case http.MethodPut:
		current, err := s.repo.GetPolicy(r.Context(), id)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "policy not found"})
			return
		}
		var req upsertPolicyRequest
		if err := s.readJSON(r, &req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
			return
		}
		item, err := buildPolicyRequest(req, current, false)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		if err := s.validatePolicyServers(r.Context(), item.ServerNames); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		item.ID = id
		if err := s.repo.UpdatePolicy(r.Context(), item); err != nil {
			if err == store.ErrAlreadyExists {
				s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "policy name already exists"})
				return
			}
			if err == store.ErrNotFound {
				s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "policy not found"})
				return
			}
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{
				ActorID:      user.ID,
				Action:       "policy.update",
				ResourceType: "policy",
				ResourceID:   id,
				IP:           remoteIP(r),
				Diff:         "trace_id=" + traceIDFromContext(r.Context()),
			})
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": item})
	case http.MethodDelete:
		if err := s.ensurePolicyNotReferenced(r.Context(), id); err != nil {
			s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		if err := s.repo.DeletePolicy(r.Context(), id); err != nil {
			if err == store.ErrNotFound {
				s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "policy not found"})
				return
			}
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{
				ActorID:      user.ID,
				Action:       "policy.delete",
				ResourceType: "policy",
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

func buildPolicyRequest(req upsertPolicyRequest, base store.Policy, create bool) (store.Policy, error) {
	now := time.Now().UTC()
	item := base
	if create {
		item = store.Policy{}
	}

	if create || strings.TrimSpace(req.Name) != "" {
		item.Name = strings.TrimSpace(req.Name)
	}
	if create || req.ServerNames != nil {
		item.ServerNames = trimNonEmpty(req.ServerNames)
	}
	if create || req.Paths != nil {
		item.Paths = trimNonEmpty(req.Paths)
	}
	if create || strings.TrimSpace(req.RcloneRemote) != "" {
		item.RcloneRemote = strings.TrimSpace(req.RcloneRemote)
	}
	if create || req.RcloneFlags != nil {
		item.RcloneFlags = trimNonEmpty(req.RcloneFlags)
	}
	if create || req.TimeoutSec != 0 {
		item.TimeoutSec = req.TimeoutSec
	}
	if create || req.RetryLimit != 0 {
		item.RetryLimit = req.RetryLimit
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	} else if create {
		item.Enabled = true
	}
	if item.TimeoutSec < 0 {
		return store.Policy{}, errInvalidPolicy("timeout_sec must be >= 0")
	}
	if item.RetryLimit < 0 {
		return store.Policy{}, errInvalidPolicy("retry_limit must be >= 0")
	}
	if item.Name == "" {
		return store.Policy{}, errInvalidPolicy("name is required")
	}
	if len(item.ServerNames) == 0 {
		return store.Policy{}, errInvalidPolicy("server_names must not be empty")
	}
	if len(item.Paths) == 0 {
		return store.Policy{}, errInvalidPolicy("paths must not be empty")
	}
	if item.RcloneRemote == "" {
		return store.Policy{}, errInvalidPolicy("rclone_remote is required")
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return item, nil
}

type errInvalidPolicy string

func (e errInvalidPolicy) Error() string {
	return string(e)
}

func (s *Server) validatePolicyServers(ctx context.Context, names []string) error {
	servers, err := s.repo.ListServers(ctx)
	if err != nil {
		return err
	}
	available := make(map[string]bool, len(servers))
	for _, srv := range servers {
		if srv.Enabled {
			available[srv.Name] = true
		}
	}
	for _, name := range names {
		if !available[name] {
			return fmt.Errorf("server not found or disabled: %s", name)
		}
	}
	return nil
}

func (s *Server) ensurePolicyNotReferenced(ctx context.Context, policyID string) error {
	schedules, err := s.repo.ListSchedules(ctx)
	if err != nil {
		return err
	}
	for _, sch := range schedules {
		if sch.PolicyID == policyID {
			return fmt.Errorf("policy is referenced by schedule: %s", sch.ID)
		}
	}
	return nil
}
