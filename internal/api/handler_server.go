package api

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/rana-remote/rana-remote/internal/auth"
	"github.com/rana-remote/rana-remote/internal/security"
	sshclient "github.com/rana-remote/rana-remote/internal/ssh"
	"github.com/rana-remote/rana-remote/internal/store"
)

type upsertServerRequest struct {
	Name         string   `json:"name"`
	Host         string   `json:"host"`
	Port         int      `json:"port"`
	User         string   `json:"user"`
	KeyPath      string   `json:"key_path"`
	Passphrase   string   `json:"passphrase,omitempty"`
	Tags         []string `json:"tags"`
	Enabled      *bool    `json:"enabled"`
	Paths        []string `json:"paths"`
	RcloneRemote string   `json:"rclone_remote"`
	RcloneFlags  []string `json:"rclone_flags"`
}

func (s *Server) handleListServers(w http.ResponseWriter, r *http.Request) {
	servers, err := s.repo.ListServers(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}

	enabledQuery := strings.TrimSpace(r.URL.Query().Get("enabled"))
	hasEnabledFilter := false
	enabledFilter := false
	if enabledQuery != "" {
		enabledFilter, err = strconv.ParseBool(enabledQuery)
		if err != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": "enabled must be true/false"})
			return
		}
		hasEnabledFilter = true
	}

	q := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("q")))
	filtered := make([]store.Server, 0, len(servers))
	for _, srv := range servers {
		if hasEnabledFilter && srv.Enabled != enabledFilter {
			continue
		}
		if q != "" {
			corpus := strings.ToLower(srv.Name + " " + srv.Host + " " + srv.User + " " + strings.Join(srv.Tags, " "))
			if !strings.Contains(corpus, q) {
				continue
			}
		}
		filtered = append(filtered, srv)
	}

	page := parsePageSpec(r)
	paged, meta := paginate(filtered, page)
	view := make([]serverView, 0, len(paged))
	for _, item := range paged {
		view = append(view, toServerView(item))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{
		"code":    "ok",
		"message": s.translate(r, "server.list.success", nil),
		"data":    view,
		"meta":    meta,
	})
}

func (s *Server) handleCreateServer(w http.ResponseWriter, r *http.Request) {
	var req upsertServerRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	item, err := buildServerRequest(req, store.Server{}, true)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(req.Passphrase) != "" {
		encrypted, encErr := security.EncryptIfConfigured(item.Passphrase)
		if encErr != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": encErr.Error()})
			return
		}
		item.Passphrase = encrypted
	}

	servers, err := s.repo.ListServers(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	for _, srv := range servers {
		if srv.Name == item.Name {
			s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "server name already exists"})
			return
		}
	}

	created, err := s.repo.UpsertServer(r.Context(), item)
	if err != nil {
		if err == store.ErrAlreadyExists {
			s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "server already exists"})
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
			ActorID:      user.ID,
			Action:       "server.create",
			ResourceType: "server",
			ResourceID:   created.ID,
		}))
	}
	s.writeJSON(w, http.StatusCreated, map[string]any{"code": "ok", "data": toServerView(created)})
}

func (s *Server) handleGetServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := s.repo.GetServer(r.Context(), id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "server not found"})
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": toServerView(srv)})
}

func (s *Server) handleUpdateServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	current, err := s.repo.GetServer(r.Context(), id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "server not found"})
		return
	}

	var req upsertServerRequest
	if err := s.readJSON(r, &req); err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", nil)
		return
	}
	item, err := buildServerRequest(req, current, false)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}
	if strings.TrimSpace(req.Passphrase) != "" {
		encrypted, encErr := security.EncryptIfConfigured(item.Passphrase)
		if encErr != nil {
			s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": encErr.Error()})
			return
		}
		item.Passphrase = encrypted
	}
	item.ID = id
	item.CreatedAt = current.CreatedAt

	updated, err := s.repo.UpsertServer(r.Context(), item)
	if err != nil {
		if err == store.ErrAlreadyExists {
			s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "server name already exists"})
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
			ActorID:      user.ID,
			Action:       "server.update",
			ResourceType: "server",
			ResourceID:   updated.ID,
		}))
	}
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": toServerView(updated)})
}

func (s *Server) handleDeleteServer(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := s.repo.GetServer(r.Context(), id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "server not found"})
		return
	}

	schedules, err := s.repo.ListSchedules(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	for _, sch := range schedules {
		for _, name := range sch.ServerNames {
			if name == srv.Name {
				s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "server is referenced by schedule"})
				return
			}
		}
	}
	policies, err := s.repo.ListPolicies(r.Context())
	if err != nil {
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	for _, p := range policies {
		for _, name := range p.ServerNames {
			if name == srv.Name {
				s.writeError(w, r, http.StatusConflict, "validation.failed", map[string]any{"detail": "server is referenced by policy"})
				return
			}
		}
	}

	if err := s.repo.DeleteServer(r.Context(), id); err != nil {
		if err == store.ErrNotFound {
			s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "server not found"})
			return
		}
		s.writeError(w, r, http.StatusInternalServerError, "internal.error", nil)
		return
	}
	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
			ActorID:      user.ID,
			Action:       "server.delete",
			ResourceType: "server",
			ResourceID:   id,
		}))
	}

	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "message": s.translate(r, "ok", nil)})
}

func (s *Server) handleTestServerConnection(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	srv, err := s.repo.GetServer(r.Context(), id)
	if err != nil {
		s.writeError(w, r, http.StatusNotFound, "validation.failed", map[string]any{"detail": "server not found"})
		return
	}

	start := time.Now()
	cfgSrv, err := s.toConfigServer(srv)
	if err != nil {
		s.writeError(w, r, http.StatusBadRequest, "validation.failed", map[string]any{"detail": err.Error()})
		return
	}
	client, err := sshclient.NewClient(cfgSrv, s.cfg.Global.SSH)
	if err != nil {
		s.writeError(w, r, http.StatusBadGateway, "validation.failed", map[string]any{"detail": fmt.Sprintf("connection failed: %v", err)})
		return
	}
	_ = client.Close()

	if user, ok := auth.UserFromContext(r.Context()); ok && s.cfg.Modules.Audit {
		_, _ = s.repo.CreateAuditLog(r.Context(), withAuditTrace(r, store.AuditLog{
			ActorID:      user.ID,
			Action:       "server.test_connection",
			ResourceType: "server",
			ResourceID:   id,
		}))
	}

	s.writeJSON(w, http.StatusOK, map[string]any{
		"code": "ok",
		"data": map[string]any{
			"server_id":   id,
			"ok":          true,
			"latency_ms":  time.Since(start).Milliseconds(),
			"tested_at":   time.Now().UTC(),
			"server_name": srv.Name,
		},
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

	actorID := strings.TrimSpace(r.URL.Query().Get("actor_id"))
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	resourceType := strings.TrimSpace(r.URL.Query().Get("resource_type"))

	filtered := make([]store.AuditLog, 0, len(logs))
	for _, item := range logs {
		if actorID != "" && item.ActorID != actorID {
			continue
		}
		if action != "" && item.Action != action {
			continue
		}
		if resourceType != "" && item.ResourceType != resourceType {
			continue
		}
		filtered = append(filtered, item)
	}
	if strings.EqualFold(strings.TrimSpace(r.URL.Query().Get("download")), "1") {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Content-Disposition", "attachment; filename=\"audit.log\"")
		for _, item := range filtered {
			_, _ = fmt.Fprintf(
				w,
				"[%s] actor=%s action=%s resource=%s:%s ip=%s diff=%s\n",
				item.Timestamp.Format(time.RFC3339),
				item.ActorID,
				item.Action,
				item.ResourceType,
				item.ResourceID,
				item.IP,
				item.Diff,
			)
		}
		return
	}
	page := parsePageSpec(r)
	paged, meta := paginate(filtered, page)
	s.writeJSON(w, http.StatusOK, map[string]any{"code": "ok", "data": paged, "meta": meta})
}

func buildServerRequest(req upsertServerRequest, base store.Server, create bool) (store.Server, error) {
	now := time.Now().UTC()
	item := base
	if create {
		item = store.Server{}
	}

	if create || strings.TrimSpace(req.Name) != "" {
		item.Name = strings.TrimSpace(req.Name)
	}
	if create || strings.TrimSpace(req.Host) != "" {
		item.Host = strings.TrimSpace(req.Host)
	}
	if create || req.Port != 0 {
		item.Port = req.Port
	}
	if item.Port == 0 {
		item.Port = 22
	}
	if create || strings.TrimSpace(req.User) != "" {
		item.User = strings.TrimSpace(req.User)
	}
	if create || strings.TrimSpace(req.KeyPath) != "" {
		item.KeyPath = strings.TrimSpace(req.KeyPath)
	}
	if create || req.Passphrase != "" {
		item.Passphrase = req.Passphrase
	}
	if create || req.Tags != nil {
		item.Tags = trimNonEmpty(req.Tags)
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
	if req.Enabled != nil {
		item.Enabled = *req.Enabled
	} else if create {
		item.Enabled = true
	}
	if item.CreatedAt.IsZero() {
		item.CreatedAt = now
	}

	if item.Name == "" {
		return store.Server{}, errInvalidServer("name is required")
	}
	if item.Host == "" {
		return store.Server{}, errInvalidServer("host is required")
	}
	if item.Port < 1 || item.Port > 65535 {
		return store.Server{}, errInvalidServer("port must be in [1,65535]")
	}
	if item.User == "" {
		return store.Server{}, errInvalidServer("user is required")
	}
	if item.KeyPath == "" {
		return store.Server{}, errInvalidServer("key_path is required")
	}
	if len(item.Paths) == 0 {
		return store.Server{}, errInvalidServer("paths must not be empty")
	}
	if item.RcloneRemote == "" {
		return store.Server{}, errInvalidServer("rclone_remote is required")
	}
	return item, nil
}

type errInvalidServer string

func (e errInvalidServer) Error() string {
	return string(e)
}

type serverView struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	Host          string    `json:"host"`
	Port          int       `json:"port"`
	User          string    `json:"user"`
	KeyPath       string    `json:"key_path"`
	HasPassphrase bool      `json:"has_passphrase"`
	Tags          []string  `json:"tags,omitempty"`
	Enabled       bool      `json:"enabled"`
	Paths         []string  `json:"paths"`
	RcloneRemote  string    `json:"rclone_remote"`
	RcloneFlags   []string  `json:"rclone_flags,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
}

func toServerView(srv store.Server) serverView {
	return serverView{
		ID:            srv.ID,
		Name:          srv.Name,
		Host:          srv.Host,
		Port:          srv.Port,
		User:          srv.User,
		KeyPath:       srv.KeyPath,
		HasPassphrase: strings.TrimSpace(srv.Passphrase) != "",
		Tags:          append([]string(nil), srv.Tags...),
		Enabled:       srv.Enabled,
		Paths:         append([]string(nil), srv.Paths...),
		RcloneRemote:  srv.RcloneRemote,
		RcloneFlags:   append([]string(nil), srv.RcloneFlags...),
		CreatedAt:     srv.CreatedAt,
	}
}
