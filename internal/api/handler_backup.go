package api

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/config"
	"github.com/rana-remote/rana-remote/internal/store"
	"github.com/rana-remote/rana-remote/internal/task"
)

type createExecutionRequest struct {
	ServerNames []string `json:"server_names"`
	TriggerType string   `json:"trigger_type"`
}

func (s *Server) handleCreateExecution(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.Modules.Backup {
		s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
		return
	}
	var req createExecutionRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	if req.TriggerType == "" {
		req.TriggerType = "manual"
	}

	selected, err := s.selectServers(r.Context(), req.ServerNames)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}

	names := make([]string, 0, len(selected))
	for _, srv := range selected {
		names = append(names, srv.Name)
	}
	sort.Strings(names)

	exec, err := s.repo.CreateExecution(r.Context(), store.Execution{
		Status:      store.StatusPending,
		TriggerType: req.TriggerType,
		ServerNames: names,
		StartedAt:   time.Now().UTC(),
	})
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{ActorID: user.ID, Action: "execution.create", ResourceType: "execution", ResourceID: exec.ID})
	}

	s.startExecution(exec, selected)
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"code":    "ok",
		"message": s.translate(r, "execution.created", nil),
		"data":    map[string]string{"id": exec.ID},
	})
}

func (s *Server) startExecution(exec store.Execution, selected []config.Server) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[exec.ID] = cancel
	s.mu.Unlock()

	exec.Status = store.StatusRunning
	_ = s.repo.UpdateExecution(context.Background(), exec)

	go func() {
		start := time.Now()
		report := task.Run(ctx, s.cfg, selected, s.executor)

		stored, err := s.repo.GetExecution(context.Background(), exec.ID)
		if err != nil {
			return
		}
		stored.EndedAt = time.Now().UTC()
		stored.DurationMS = time.Since(start).Milliseconds()
		stored.Results = make([]store.Result, 0, len(report.Results))
		for _, rr := range report.Results {
			stored.Results = append(stored.Results, store.Result{
				Server:     rr.Server,
				Success:    rr.Success,
				DurationMS: rr.Duration.Milliseconds(),
				Error:      rr.Error,
			})
		}

		if ctx.Err() != nil {
			stored.Status = store.StatusCanceled
			stored.Error = ctx.Err().Error()
		} else if report.ExitCode() == 0 {
			stored.Status = store.StatusSuccess
			stored.Error = ""
		} else {
			stored.Status = store.StatusFailed
			stored.Error = "one or more servers failed"
		}
		_ = s.repo.UpdateExecution(context.Background(), stored)

		s.mu.Lock()
		delete(s.cancels, exec.ID)
		s.mu.Unlock()
		cancel()
	}()
}

func (s *Server) handleListExecutions(w http.ResponseWriter, r *http.Request) {
	executions, err := s.repo.ListExecutions(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": executions})
}

func (s *Server) handleGetExecution(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	exec, err := s.repo.GetExecution(r.Context(), id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "execution.not_found", nil)
		return
	}
	logs, _ := s.repo.ListExecutionLogs(r.Context(), id)
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": map[string]any{"execution": exec, "logs": logs}})
}

func (s *Server) handleCancelExecution(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	_, err := s.repo.GetExecution(r.Context(), id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "execution.not_found", nil)
		return
	}
	s.mu.Lock()
	cancel, ok := s.cancels[id]
	s.mu.Unlock()
	if !ok {
		s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
		return
	}
	cancel()

	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{ActorID: user.ID, Action: "execution.cancel", ResourceType: "execution", ResourceID: id})
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
}

func (s *Server) selectServers(ctx context.Context, names []string) ([]config.Server, error) {
	all, err := s.repo.ListServers(ctx)
	if err != nil {
		return nil, err
	}
	if len(all) == 0 {
		return nil, errNoServers
	}
	byName := make(map[string]store.Server, len(all))
	for _, srv := range all {
		if srv.Enabled {
			byName[srv.Name] = srv
		}
	}
	selected := make([]config.Server, 0)
	if len(names) == 0 {
		for _, srv := range byName {
			selected = append(selected, toConfigServer(srv))
		}
		sort.Slice(selected, func(i, j int) bool { return selected[i].Name < selected[j].Name })
		if len(selected) == 0 {
			return nil, errNoServers
		}
		return selected, nil
	}

	for _, name := range names {
		srv, ok := byName[name]
		if !ok {
			continue
		}
		selected = append(selected, toConfigServer(srv))
	}
	if len(selected) == 0 {
		return nil, errNoServers
	}
	return selected, nil
}

var errNoServers = errors.New("no servers selected")

func toConfigServer(s store.Server) config.Server {
	return config.Server{
		Name:    s.Name,
		Host:    s.Host,
		Port:    s.Port,
		User:    s.User,
		KeyPath: s.KeyPath,
		Paths:   append([]string(nil), s.Paths...),
		Rclone: config.Rclone{
			Remote: s.RcloneRemote,
			Flags:  append([]string(nil), s.RcloneFlags...),
		},
	}
}
