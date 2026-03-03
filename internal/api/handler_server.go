package api

import (
	"net/http"

	"github.com/rana-remote/rana-remote/internal/auth"
)

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.repo.ListServers(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"code":    "ok",
		"message": s.translate(r, "server.list.success", nil),
		"data":    servers,
	})
}

func (s *Server) handleAuditLogs(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Audit {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}
	logs, err := s.repo.ListAuditLogs(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": logs})
}

func (s *Server) handleSchedules(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Schedule {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}
	user, _ := auth.UserFromContext(r.Context())
	_ = user
	// Scheduler module placeholder for MVP.
	s.writeJSON(w, http.StatusOK, map[string]any{
		"code": "ok",
		"data": []any{},
	})
}
