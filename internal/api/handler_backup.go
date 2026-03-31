package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/config"
	"github.com/rana-remote/rana-remote/internal/security"
	"github.com/rana-remote/rana-remote/internal/store"
	"github.com/rana-remote/rana-remote/internal/task"
)

type createExecutionRequest struct {
	ServerNames []string `json:"server_names"`
	PolicyID    string   `json:"policy_id"`
	TriggerType string   `json:"trigger_type"`
}

// TriggerExecution triggers a backup execution by server names.
func (s *Server) TriggerExecution(ctx context.Context, policyID string, serverNames []string, triggerType string) (store.Execution, error) {
	if !s.cfg.Modules.Backup {
		return store.Execution{}, errFeatureDisabled
	}
	if triggerType == "" {
		triggerType = "manual"
	}
	var (
		selected []config.Server
		policy   *store.Policy
		err      error
	)
	if strings.TrimSpace(policyID) != "" {
		selected, policy, err = s.selectServersByPolicy(ctx, policyID, serverNames)
	} else {
		selected, err = s.selectServers(ctx, serverNames)
	}
	if err != nil {
		return store.Execution{}, err
	}

	names := make([]string, 0, len(selected))
	for _, srv := range selected {
		names = append(names, srv.Name)
	}
	sort.Strings(names)

	exec, err := s.repo.CreateExecution(ctx, store.Execution{
		PolicyID:    policyID,
		Status:      store.StatusPending,
		TriggerType: triggerType,
		ServerNames: names,
		StartedAt:   time.Now().UTC(),
	})
	if err != nil {
		return store.Execution{}, err
	}
	s.startExecution(exec, selected, policy)
	return exec, nil
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

	exec, err := s.TriggerExecution(r.Context(), req.PolicyID, req.ServerNames, req.TriggerType)
	if err != nil {
		if errors.Is(err, errFeatureDisabled) {
			s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}

	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{ActorID: user.ID, Action: "execution.create", ResourceType: "execution", ResourceID: exec.ID})
	}
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"code":    "ok",
		"message": s.translate(r, "execution.created", nil),
		"data":    map[string]string{"id": exec.ID},
	})
}

func (s *Server) startExecution(exec store.Execution, selected []config.Server, policy *store.Policy) {
	ctx, cancel := context.WithCancel(context.Background())
	s.mu.Lock()
	s.cancels[exec.ID] = cancel
	s.mu.Unlock()

	exec.Status = store.StatusRunning
	_ = s.repo.UpdateExecution(context.Background(), exec)

	go func() {
		start := time.Now()
		baseExecutor := s.executor
		if baseExecutor == nil {
			baseExecutor = task.SSHExecutor{SSHConfig: s.cfg.Global.SSH}
		}
		cfgForRun := *s.cfg
		retryLimit := 0
		if policy != nil {
			if policy.TimeoutSec > 0 {
				cfgForRun.Global.Timeout = fmt.Sprintf("%ds", policy.TimeoutSec)
			}
			if policy.RetryLimit > 0 {
				retryLimit = policy.RetryLimit
			}
		}
		logExec := logCapturingExecutor{
			base: baseExecutor,
			onLine: func(serverName, level, line string) {
				s.appendExecutionLog(context.Background(), exec.ID, level, "["+serverName+"] "+line)
			},
		}
		s.appendExecutionLog(context.Background(), exec.ID, "INFO", "execution started")
		var report task.Report
		for attempt := 0; attempt <= retryLimit; attempt++ {
			if attempt > 0 {
				s.appendExecutionLog(context.Background(), exec.ID, "WARN", fmt.Sprintf("retry attempt %d/%d", attempt, retryLimit))
				stored, _ := s.repo.GetExecution(context.Background(), exec.ID)
				stored.Status = store.StatusRetrying
				_ = s.repo.UpdateExecution(context.Background(), stored)
			}
			report = task.Run(ctx, &cfgForRun, selected, logExec)
			if ctx.Err() != nil || report.ExitCode() == 0 {
				break
			}
		}
		s.appendExecutionLog(context.Background(), exec.ID, "INFO", "execution finished")

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
		if s.cfg.Modules.Notify && s.notifier != nil {
			if err := s.notifier.NotifyExecution(context.Background(), stored); err != nil {
				s.appendExecutionLog(context.Background(), exec.ID, "WARN", "notify failed: "+err.Error())
			}
		}

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

	statusFilter := strings.TrimSpace(r.URL.Query().Get("status"))
	triggerFilter := strings.TrimSpace(r.URL.Query().Get("trigger_type"))
	serverFilter := strings.TrimSpace(r.URL.Query().Get("server"))

	filtered := make([]store.Execution, 0, len(executions))
	for _, ex := range executions {
		if statusFilter != "" && string(ex.Status) != statusFilter {
			continue
		}
		if triggerFilter != "" && ex.TriggerType != triggerFilter {
			continue
		}
		if serverFilter != "" {
			matched := false
			for _, name := range ex.ServerNames {
				if name == serverFilter {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		filtered = append(filtered, ex)
	}

	page := parsePageSpec(r)
	paged, meta := paginate(filtered, page)
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": paged, "meta": meta})
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

func (s *Server) handleRetryExecution(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	oldExec, err := s.repo.GetExecution(r.Context(), id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "execution.not_found", nil)
		return
	}
	if oldExec.Status == store.StatusPending || oldExec.Status == store.StatusRunning {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "execution is still running"})
		return
	}

	exec, err := s.TriggerExecution(r.Context(), oldExec.PolicyID, oldExec.ServerNames, "retry")
	if err != nil {
		if errors.Is(err, errFeatureDisabled) {
			s.writeError(w, r, http.StatusNotImplemented, "feature_disabled", nil)
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}
	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), store.AuditLog{
			ActorID:      user.ID,
			Action:       "execution.retry",
			ResourceType: "execution",
			ResourceID:   id,
			Diff:         "new_execution_id=" + exec.ID,
		})
	}
	s.writeJSON(w, http.StatusAccepted, map[string]any{
		"code": "ok",
		"data": map[string]string{"id": exec.ID},
	})
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
			item, convErr := s.toConfigServer(srv)
			if convErr != nil {
				return nil, convErr
			}
			selected = append(selected, item)
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
		item, convErr := s.toConfigServer(srv)
		if convErr != nil {
			return nil, convErr
		}
		selected = append(selected, item)
	}
	if len(selected) == 0 {
		return nil, errNoServers
	}
	return selected, nil
}

func (s *Server) selectServersByPolicy(ctx context.Context, policyID string, overrideNames []string) ([]config.Server, *store.Policy, error) {
	policy, err := s.repo.GetPolicy(ctx, policyID)
	if err != nil {
		return nil, nil, err
	}
	if !policy.Enabled {
		return nil, nil, errors.New("policy is disabled")
	}
	names := append([]string(nil), policy.ServerNames...)
	if len(overrideNames) > 0 {
		set := make(map[string]struct{}, len(overrideNames))
		for _, name := range overrideNames {
			set[name] = struct{}{}
		}
		filtered := make([]string, 0, len(names))
		for _, name := range names {
			if _, ok := set[name]; ok {
				filtered = append(filtered, name)
			}
		}
		names = filtered
	}
	selected, err := s.selectServers(ctx, names)
	if err != nil {
		return nil, nil, err
	}
	for i := range selected {
		selected[i].Paths = append([]string(nil), policy.Paths...)
		selected[i].Rclone.Remote = policy.RcloneRemote
		selected[i].Rclone.Flags = append([]string(nil), policy.RcloneFlags...)
	}
	return selected, &policy, nil
}

var (
	errNoServers       = errors.New("no servers selected")
	errFeatureDisabled = errors.New("feature disabled")
)

func (s *Server) toConfigServer(item store.Server) (config.Server, error) {
	passphrase, err := security.DecryptIfConfigured(item.Passphrase)
	if err != nil {
		return config.Server{}, fmt.Errorf("decrypt server passphrase for %s: %w", item.Name, err)
	}
	return config.Server{
		Name:       item.Name,
		Host:       item.Host,
		Port:       item.Port,
		User:       item.User,
		KeyPath:    item.KeyPath,
		Passphrase: passphrase,
		Paths:      append([]string(nil), item.Paths...),
		Rclone: config.Rclone{
			Remote: item.RcloneRemote,
			Flags:  append([]string(nil), item.RcloneFlags...),
		},
	}, nil
}

type logCapturingExecutor struct {
	base   task.Executor
	onLine func(serverName, level, line string)
}

func (e logCapturingExecutor) Execute(ctx context.Context, srv config.Server, script string, stdout, stderr io.Writer) error {
	infoSink := newLineSink(func(line string) {
		if e.onLine != nil {
			e.onLine(srv.Name, "INFO", line)
		}
	})
	errSink := newLineSink(func(line string) {
		if e.onLine != nil {
			e.onLine(srv.Name, "ERROR", line)
		}
	})
	err := e.base.Execute(ctx, srv, script, io.MultiWriter(stdout, infoSink), io.MultiWriter(stderr, errSink))
	infoSink.Flush()
	errSink.Flush()
	return err
}

type lineSink struct {
	mu      sync.Mutex
	buf     string
	onFlush func(line string)
}

func newLineSink(onFlush func(line string)) *lineSink {
	return &lineSink{onFlush: onFlush}
}

func (w *lineSink) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.buf += string(p)
	for {
		idx := strings.IndexByte(w.buf, '\n')
		if idx < 0 {
			break
		}
		line := strings.TrimSpace(w.buf[:idx])
		if line != "" && w.onFlush != nil {
			w.onFlush(line)
		}
		w.buf = w.buf[idx+1:]
	}
	return len(p), nil
}

func (w *lineSink) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	line := strings.TrimSpace(w.buf)
	w.buf = ""
	if line != "" && w.onFlush != nil {
		w.onFlush(line)
	}
}
