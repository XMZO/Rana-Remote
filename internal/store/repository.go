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
	ListUsers(ctx context.Context) ([]User, error)
	GetUserByUsername(ctx context.Context, username string) (User, error)
	GetUserByID(ctx context.Context, id string) (User, error)
	UpdateUser(ctx context.Context, user User) error
	UpdateUserPassword(ctx context.Context, id, passwordHash string) error
	UpdateUserLogin(ctx context.Context, id string, at time.Time) error
	UpdateUserLocale(ctx context.Context, id, locale string) error
	DeleteUser(ctx context.Context, id string) error

	ListServers(ctx context.Context) ([]Server, error)
	GetServer(ctx context.Context, id string) (Server, error)
	UpsertServer(ctx context.Context, server Server) (Server, error)
	DeleteServer(ctx context.Context, id string) error
	SeedServers(ctx context.Context, servers []Server) error

	ListPolicies(ctx context.Context) ([]Policy, error)
	GetPolicy(ctx context.Context, id string) (Policy, error)
	CreatePolicy(ctx context.Context, policy Policy) (Policy, error)
	UpdatePolicy(ctx context.Context, policy Policy) error
	DeletePolicy(ctx context.Context, id string) error

	ListSchedules(ctx context.Context) ([]Schedule, error)
	GetSchedule(ctx context.Context, id string) (Schedule, error)
	CreateSchedule(ctx context.Context, schedule Schedule) (Schedule, error)
	UpdateSchedule(ctx context.Context, schedule Schedule) error
	DeleteSchedule(ctx context.Context, id string) error

	CreateExecution(ctx context.Context, ex Execution) (Execution, error)
	UpdateExecution(ctx context.Context, ex Execution) error
	GetExecution(ctx context.Context, id string) (Execution, error)
	ListExecutions(ctx context.Context) ([]Execution, error)

	AppendExecutionLog(ctx context.Context, log ExecutionLog) error
	ListExecutionLogs(ctx context.Context, executionID string) ([]ExecutionLog, error)

	CreateAuditLog(ctx context.Context, log AuditLog) (AuditLog, error)
	ListAuditLogs(ctx context.Context) ([]AuditLog, error)
	ApplyRetention(ctx context.Context, executionCutoff, logCutoff, auditCutoff time.Time) (RetentionReport, error)
}

type MemoryRepository struct {
	mu           sync.RWMutex
	users        map[string]User
	usersByName  map[string]string
	servers      map[string]Server
	serverByName map[string]string
	policies     map[string]Policy
	schedules    map[string]Schedule
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
		policies:     make(map[string]Policy),
		schedules:    make(map[string]Schedule),
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

func (m *MemoryRepository) ListUsers(_ context.Context) ([]User, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]User, 0, len(m.users))
	for _, u := range m.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Username < out[j].Username })
	return out, nil
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

func (m *MemoryRepository) UpdateUser(_ context.Context, user User) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.users[user.ID]
	if !ok {
		return ErrNotFound
	}
	if user.Username != current.Username {
		if existingID, exists := m.usersByName[user.Username]; exists && existingID != user.ID {
			return ErrAlreadyExists
		}
		delete(m.usersByName, current.Username)
		m.usersByName[user.Username] = user.ID
	}
	if user.PasswordHash == "" {
		user.PasswordHash = current.PasswordHash
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = current.CreatedAt
	}
	m.users[user.ID] = user
	return nil
}

func (m *MemoryRepository) UpdateUserPassword(_ context.Context, id, passwordHash string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	user.PasswordHash = passwordHash
	m.users[id] = user
	return nil
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

func (m *MemoryRepository) DeleteUser(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	user, ok := m.users[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.users, id)
	delete(m.usersByName, user.Username)
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

func (m *MemoryRepository) GetServer(_ context.Context, id string) (Server, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	srv, ok := m.servers[id]
	if !ok {
		return Server{}, ErrNotFound
	}
	return srv, nil
}

func (m *MemoryRepository) UpsertServer(_ context.Context, server Server) (Server, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if existingID, ok := m.serverByName[server.Name]; ok && existingID != server.ID && server.ID != "" {
		return Server{}, ErrAlreadyExists
	}

	if server.ID != "" {
		if old, ok := m.servers[server.ID]; ok {
			if old.Name != server.Name {
				if conflictID, conflict := m.serverByName[server.Name]; conflict && conflictID != server.ID {
					return Server{}, ErrAlreadyExists
				}
				delete(m.serverByName, old.Name)
			}
		}
	}

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

func (m *MemoryRepository) DeleteServer(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	srv, ok := m.servers[id]
	if !ok {
		return ErrNotFound
	}
	delete(m.servers, id)
	delete(m.serverByName, srv.Name)
	return nil
}

func (m *MemoryRepository) SeedServers(ctx context.Context, servers []Server) error {
	for _, s := range servers {
		if _, err := m.UpsertServer(ctx, s); err != nil {
			return err
		}
	}
	return nil
}

func (m *MemoryRepository) ListPolicies(_ context.Context) ([]Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Policy, 0, len(m.policies))
	for _, p := range m.policies {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *MemoryRepository) GetPolicy(_ context.Context, id string) (Policy, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	p, ok := m.policies[id]
	if !ok {
		return Policy{}, ErrNotFound
	}
	return p, nil
}

func (m *MemoryRepository) CreatePolicy(_ context.Context, policy Policy) (Policy, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if policy.ID == "" {
		policy.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = now
	}
	if policy.UpdatedAt.IsZero() {
		policy.UpdatedAt = now
	}
	if _, exists := m.policies[policy.ID]; exists {
		return Policy{}, ErrAlreadyExists
	}
	for _, existing := range m.policies {
		if existing.Name == policy.Name {
			return Policy{}, ErrAlreadyExists
		}
	}
	m.policies[policy.ID] = policy
	return policy, nil
}

func (m *MemoryRepository) UpdatePolicy(_ context.Context, policy Policy) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	current, ok := m.policies[policy.ID]
	if !ok {
		return ErrNotFound
	}
	for id, existing := range m.policies {
		if id != policy.ID && existing.Name == policy.Name {
			return ErrAlreadyExists
		}
	}
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = current.CreatedAt
	}
	policy.UpdatedAt = time.Now().UTC()
	m.policies[policy.ID] = policy
	return nil
}

func (m *MemoryRepository) DeletePolicy(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.policies[id]; !ok {
		return ErrNotFound
	}
	delete(m.policies, id)
	return nil
}

func (m *MemoryRepository) ListSchedules(_ context.Context) ([]Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]Schedule, 0, len(m.schedules))
	for _, s := range m.schedules {
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].Name < out[j].Name
		}
		return out[i].CreatedAt.Before(out[j].CreatedAt)
	})
	return out, nil
}

func (m *MemoryRepository) GetSchedule(_ context.Context, id string) (Schedule, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	s, ok := m.schedules[id]
	if !ok {
		return Schedule{}, ErrNotFound
	}
	return s, nil
}

func (m *MemoryRepository) CreateSchedule(_ context.Context, schedule Schedule) (Schedule, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if schedule.ID == "" {
		schedule.ID = uuid.NewString()
	}
	now := time.Now().UTC()
	if schedule.CreatedAt.IsZero() {
		schedule.CreatedAt = now
	}
	if schedule.UpdatedAt.IsZero() {
		schedule.UpdatedAt = now
	}
	if _, exists := m.schedules[schedule.ID]; exists {
		return Schedule{}, ErrAlreadyExists
	}
	m.schedules[schedule.ID] = schedule
	return schedule, nil
}

func (m *MemoryRepository) UpdateSchedule(_ context.Context, schedule Schedule) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[schedule.ID]; !ok {
		return ErrNotFound
	}
	schedule.UpdatedAt = time.Now().UTC()
	m.schedules[schedule.ID] = schedule
	return nil
}

func (m *MemoryRepository) DeleteSchedule(_ context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.schedules[id]; !ok {
		return ErrNotFound
	}
	delete(m.schedules, id)
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

func (m *MemoryRepository) ApplyRetention(_ context.Context, executionCutoff, logCutoff, auditCutoff time.Time) (RetentionReport, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	report := RetentionReport{}
	for id, ex := range m.executions {
		if !ex.StartedAt.IsZero() && ex.StartedAt.Before(executionCutoff) {
			delete(m.executions, id)
			delete(m.execLogs, id)
			report.Executions++
		}
	}
	for executionID, logs := range m.execLogs {
		kept := logs[:0]
		for _, log := range logs {
			if !log.Timestamp.IsZero() && log.Timestamp.Before(logCutoff) {
				report.ExecutionLogs++
				continue
			}
			kept = append(kept, log)
		}
		if len(kept) == 0 {
			delete(m.execLogs, executionID)
			continue
		}
		m.execLogs[executionID] = append([]ExecutionLog(nil), kept...)
	}
	keptAudit := m.auditLogs[:0]
	for _, log := range m.auditLogs {
		if !log.Timestamp.IsZero() && log.Timestamp.Before(auditCutoff) {
			report.AuditLogs++
			continue
		}
		keptAudit = append(keptAudit, log)
	}
	m.auditLogs = append([]AuditLog(nil), keptAudit...)
	return report, nil
}
