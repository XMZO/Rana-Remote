package store

import (
	"context"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrNotFound      = errors.New("not found")
	ErrAlreadyExists = errors.New("already exists")
)

type Repository interface {
	CreateUser(ctx context.Context, user User) (User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	UpdateUserLogin(ctx context.Context, id string, at time.Time) error
	UpdateUserLocale(ctx context.Context, id, locale string) error

	ListServers(ctx context.Context) ([]Server, error)
	UpsertServer(ctx context.Context, server Server) (Server, error)
	SeedServers(ctx context.Context, servers []Server) error

	CreateExecution(ctx context.Context, ex Execution) (Execution, error)
	UpdateExecution(ctx context.Context, ex Execution) error
	GetExecution(ctx context.Context, id string) (Execution, error)
	ListExecutions(ctx context.Context) ([]Execution, error)

	AppendExecutionLog(ctx context.Context, log ExecutionLog) error
	ListExecutionLogs(ctx context.Context, executionID string) ([]ExecutionLog, error)

	CreateAuditLog(ctx context.Context, log AuditLog) (AuditLog, error)
	ListAuditLogs(ctx context.Context) ([]AuditLog, error)
}

type MemoryRepository struct {
	mu           sync.RWMutex
	users        map[string]User
	usersByName  map[string]string
	servers      map[string]Server
	serverByName map[string]string
	executions   map[string]Execution
	execLogs     map[string][]ExecutionLog
	auditLogs    []AuditLog
}

func NewMemoryRepository() *MemoryRepository {
	return &MemoryRepository{
		users:        make(map[string]User),
		usersByName:  make(map[string]string),
		servers:      make(map[string]Server),
		serverByName: make(map[string]string),
		executions:   make(map[string]Execution),
		execLogs:     make(map[string][]ExecutionLog),
		auditLogs:    make([]AuditLog, 0),
	}
}

func (m *MemoryRepository) CreateUser(_ context.Context, user User) (User, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.usersByName[user.Username]; ok {
		return User{}, ErrAlreadyExists
	}
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}
	m.users[user.ID] = user
	m.usersByName[user.Username] = user.ID
	return user, nil
}

func (m *MemoryRepository) GetUserByUsername(_ context.Context, username string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	id, ok := m.usersByName[username]
	if !ok {
		return User{}, ErrNotFound
	}
	user, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (m *MemoryRepository) GetUserByID(_ context.Context, id string) (User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	user, ok := m.users[id]
	if !ok {
		return User{}, ErrNotFound
	}
	return user, nil
}

func (m *MemoryRepository) UpdateUserLogin(_ context.Context, id string, at time.Time) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	user.LastLoginAt = at
	m.users[id] = user
	return nil
}

func (m *MemoryRepository) UpdateUserLocale(_ context.Context, id, locale string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	user.Locale = locale
	m.users[id] = user
	return nil
}

func (m *MemoryRepository) ListServers(_ context.Context) ([]Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Server, 0, len(m.servers))
	for _, s := range m.servers {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (m *MemoryRepository) UpsertServer(_ context.Context, server Server) (Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if server.ID == "" {
		if id, ok := m.serverByName[server.Name]; ok {
			server.ID = id
		} else {
			server.ID = uuid.NewString()
		}
	}
	if server.CreatedAt.IsZero() {
		server.CreatedAt = time.Now().UTC()
	}
	m.servers[server.ID] = server
	m.serverByName[server.Name] = server.ID
	return server, nil
}

func (m *MemoryRepository) SeedServers(ctx context.Context, servers []Server) error {
	for _, s := range servers {
		if _, err := m.UpsertServer(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MemoryRepository) CreateExecution(_ context.Context, ex Execution) (Execution, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ex.ID == "" {
		ex.ID = uuid.NewString()
	}
	if ex.StartedAt.IsZero() {
		ex.StartedAt = time.Now().UTC()
	}
	m.executions[ex.ID] = ex
	return ex, nil
}

func (m *MemoryRepository) UpdateExecution(_ context.Context, ex Execution) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.executions[ex.ID]; !ok {
		return ErrNotFound
	}
	m.executions[ex.ID] = ex
	return nil
}

func (m *MemoryRepository) GetExecution(_ context.Context, id string) (Execution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	ex, ok := m.executions[id]
	if !ok {
		return Execution{}, ErrNotFound
	}
	return ex, nil
}

func (m *MemoryRepository) ListExecutions(_ context.Context) ([]Execution, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Execution, 0, len(m.executions))
	for _, e := range m.executions {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].StartedAt.After(out[j].StartedAt) })
	return out, nil
}

func (m *MemoryRepository) AppendExecutionLog(_ context.Context, log ExecutionLog) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}
	m.execLogs[log.ExecutionID] = append(m.execLogs[log.ExecutionID], log)
	return nil
}

func (m *MemoryRepository) ListExecutionLogs(_ context.Context, executionID string) ([]ExecutionLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	logs := m.execLogs[executionID]
	out := make([]ExecutionLog, len(logs))
	copy(out, logs)
	return out, nil
}

func (m *MemoryRepository) CreateAuditLog(_ context.Context, log AuditLog) (AuditLog, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if log.ID == "" {
		log.ID = uuid.NewString()
	}
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}
	m.auditLogs = append(m.auditLogs, log)
	return log, nil
}

func (m *MemoryRepository) ListAuditLogs(_ context.Context) ([]AuditLog, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]AuditLog, len(m.auditLogs))
	copy(out, m.auditLogs)
	sort.Slice(out, func(i, j int) bool { return out[i].Timestamp.After(out[j].Timestamp) })
	return out, nil
}
