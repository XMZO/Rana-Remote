package api

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/scheduler"
	"github.com/rana-remote/rana-remote/internal/store"
)

type upsertScheduleRequest struct {
	Name          string   `json:"name"`
	PolicyID      string   `json:"policy_id"`
	ServerNames   []string `json:"server_names"`
	CronExpr      string   `json:"cron_expr"`
	Timezone      string   `json:"timezone"`
	Enabled       *bool    `json:"enabled"`
	MisfirePolicy string   `json:"misfire_policy"`
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Schedule {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		schedules, err := s.repo.ListSchedules(r.Context())
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
			enabledFilter = parsed
			hasEnabledFilter = true
		}
		q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
		filtered := make([]store.Schedule, 0, len(schedules))
		for _, sch := range schedules {
			if hasEnabledFilter && sch.Enabled != enabledFilter {
				continue
			}
			if q != "" {
				if !strings.Contains(strings.ToLower(sch.Name+" "+sch.CronExpr+" "+strings.Join(sch.ServerNames, " ")), q) {
					continue
				}
			}
			filtered = append(filtered, sch)
		}
		page := parsePageSpec(r)
		paged, meta := paginate(filtered, page)
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": paged, "meta": meta})
	case http.MethodPost:
		var req upsertScheduleRequest
		if err := s.readJSON(r, &req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
			return
		}
		schedule, err := buildScheduleRequest(req, store.Schedule{}, true)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		if err := s.resolveAndValidateScheduleTargets(r.Context(), &schedule); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		created, err := s.repo.CreateSchedule(r.Context(), schedule)
		if err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{ActorID: user.ID, Action: "schedule.create", ResourceType: "schedule", ResourceID: created.ID}))
		}
		s.writeJSON(w, http.StatusCreated, map[string]any{"code": "ok", "data": created})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func (s *Server) handleScheduleByID(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Schedule {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}
	id := r.PathValue("id")
	if id == "" {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}

	switch r.Method {
	case http.MethodGet:
		item, err := s.repo.GetSchedule(r.Context(), id)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "schedule not found"})
			return
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": item})
	case http.MethodPut:
		current, err := s.repo.GetSchedule(r.Context(), id)
		if err != nil {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "schedule not found"})
			return
		}
		var req upsertScheduleRequest
		if err := s.readJSON(r, &req); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
			return
		}
		updated, err := buildScheduleRequest(req, current, false)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		if err := s.resolveAndValidateScheduleTargets(r.Context(), &updated); err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
			return
		}
		updated.ID = id
		if err := s.repo.UpdateSchedule(r.Context(), updated); err != nil {
			s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
			return
		}
		if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{ActorID: user.ID, Action: "schedule.update", ResourceType: "schedule", ResourceID: id}))
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": updated})
	case http.MethodDelete:
		if err := s.repo.DeleteSchedule(r.Context(), id); err != nil {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "schedule not found"})
			return
		}
		if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
			_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{ActorID: user.ID, Action: "schedule.delete", ResourceType: "schedule", ResourceID: id}))
		}
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

func buildScheduleRequest(req upsertScheduleRequest, base store.Schedule, create bool) (store.Schedule, error) {
	now := time.Now().UTC()
	item := base
	if create {
		item = store.Schedule{}
	}

	if create || strings.TrimSpace(req.Name) != "" {
		item.Name = strings.TrimSpace(req.Name)
	}
	if create || strings.TrimSpace(req.PolicyID) != "" {
		item.PolicyID = strings.TrimSpace(req.PolicyID)
	}
	if create || req.ServerNames != nil {
		item.ServerNames = trimNonEmpty(req.ServerNames)
	}
	if create || strings.TrimSpace(req.CronExpr) != "" {
		item.CronExpr = strings.TrimSpace(req.CronExpr)
	}
	if create || strings.TrimSpace(req.Timezone) != "" {
		item.Timezone = strings.TrimSpace(req.Timezone)
	}
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	} else if create {
		item.Enabled = true
	}
	if create || strings.TrimSpace(req.MisfirePolicy) != "" {
		item.MisfirePolicy = strings.TrimSpace(req.MisfirePolicy)
	}
	if item.MisfirePolicy == "" {
		item.MisfirePolicy = "run_once"
	}
	if item.MisfirePolicy != "run_once" && item.MisfirePolicy != "skip" {
		return store.Schedule{}, errInvalidSchedule("misfire_policy must be run_once or skip")
	}
	if strings.TrimSpace(item.Name) == "" {
		return store.Schedule{}, errInvalidSchedule("name is required")
	}
	if item.PolicyID == "" && len(item.ServerNames) == 0 {
		return store.Schedule{}, errInvalidSchedule("server_names must not be empty when policy_id is empty")
	}
	if strings.TrimSpace(item.CronExpr) == "" {
		return store.Schedule{}, errInvalidSchedule("cron_expr is required")
	}
	next, err := scheduler.NextRunAt(item.CronExpr, now, item.Timezone)
	if err != nil {
		return store.Schedule{}, errInvalidSchedule(err.Error())
	}
	item.NextRunAt = next
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}
	item.UpdatedAt = now
	return item, nil
}

func trimNonEmpty(items []string) []string {
	out := make([]string, 0, len(items))
	for _, it := range items {
		it = strings.TrimSpace(it)
		if it != "" {
			out = append(out, it)
		}
	}
	return out
}

type errInvalidSchedule string

func (e errInvalidSchedule) Error() string {
	return string(e)
}

func (s *Server) validateScheduleServers(ctx context.Context, names []string) error {
	servers, err := s.repo.ListServers(ctx)
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return errInvalidSchedule("server_names must not be empty")
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

func (s *Server) resolveAndValidateScheduleTargets(ctx context.Context, schedule *store.Schedule) error {
	if schedule == nil {
		return errInvalidSchedule("schedule is required")
	}
	if strings.TrimSpace(schedule.PolicyID) != "" {
		policy, err := s.repo.GetPolicy(ctx, schedule.PolicyID)
		if err != nil {
			return errInvalidSchedule("policy not found")
		}
		if !policy.Enabled {
			return errInvalidSchedule("policy is disabled")
		}
		schedule.ServerNames = append([]string(nil), policy.ServerNames...)
	}
	return s.validateScheduleServers(ctx, schedule.ServerNames)
}
