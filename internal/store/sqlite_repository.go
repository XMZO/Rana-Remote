package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

// SQLiteRepository implements Repository with a real SQLite database.
type SQLiteRepository struct {
	db *sql.DB
}

func NewSQLiteRepository(dsn string) (*SQLiteRepository, error) {
	dsn = strings.TrimSpace(dsn)
	if dsn == "" {
		return nil, errors.New("sqlite dsn is required")
	}
	if err := ensureSQLiteDir(dsn); err != nil {
		return nil, err
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(0)

	repo := &SQLiteRepository{db: db}
	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if err := repo.configure(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := repo.Migrate(context.Background()); err != nil {
		_ = db.Close()
		return nil, err
	}
	return repo, nil
}

func (s *SQLiteRepository) Close() error {
	return s.db.Close()
}

func (s *SQLiteRepository) configure(ctx context.Context) error {
	stmts := []string{
		"PRAGMA foreign_keys = ON",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA journal_mode = WAL",
	}
	for _, stmt := range stmts {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("configure sqlite with %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *SQLiteRepository) Migrate(ctx context.Context) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL,
			locale TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			last_login_at INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS servers (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			host TEXT NOT NULL,
			port INTEGER NOT NULL,
			ssh_user TEXT NOT NULL,
			key_path TEXT NOT NULL,
			passphrase TEXT NOT NULL DEFAULT '',
			tags_json TEXT NOT NULL DEFAULT '[]',
			enabled INTEGER NOT NULL,
			paths_json TEXT NOT NULL DEFAULT '[]',
			rclone_remote TEXT NOT NULL,
			rclone_flags_json TEXT NOT NULL DEFAULT '[]',
			created_at INTEGER NOT NULL
		)`,
		`ALTER TABLE servers ADD COLUMN passphrase TEXT NOT NULL DEFAULT ''`,
		`CREATE TABLE IF NOT EXISTS policies (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL UNIQUE,
			server_names_json TEXT NOT NULL DEFAULT '[]',
			paths_json TEXT NOT NULL DEFAULT '[]',
			rclone_remote TEXT NOT NULL,
			rclone_flags_json TEXT NOT NULL DEFAULT '[]',
			timeout_sec INTEGER NOT NULL DEFAULT 0,
			retry_limit INTEGER NOT NULL DEFAULT 0,
			enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS schedules (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			policy_id TEXT NOT NULL DEFAULT '',
			server_names_json TEXT NOT NULL DEFAULT '[]',
			cron_expr TEXT NOT NULL,
			timezone TEXT NOT NULL,
			enabled INTEGER NOT NULL,
			misfire_policy TEXT NOT NULL,
			last_run_at INTEGER NOT NULL DEFAULT 0,
			next_run_at INTEGER NOT NULL DEFAULT 0,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`ALTER TABLE schedules ADD COLUMN policy_id TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_schedules_enabled_next_run ON schedules(enabled, next_run_at)`,
		`CREATE TABLE IF NOT EXISTS executions (
			id TEXT PRIMARY KEY,
			policy_id TEXT NOT NULL DEFAULT '',
			status TEXT NOT NULL,
			trigger_type TEXT NOT NULL,
			server_names_json TEXT NOT NULL DEFAULT '[]',
			started_at INTEGER NOT NULL,
			ended_at INTEGER NOT NULL DEFAULT 0,
			duration_ms INTEGER NOT NULL DEFAULT 0,
			error_text TEXT NOT NULL DEFAULT '',
			results_json TEXT NOT NULL DEFAULT '[]'
		)`,
		`ALTER TABLE executions ADD COLUMN policy_id TEXT NOT NULL DEFAULT ''`,
		`CREATE INDEX IF NOT EXISTS idx_executions_started_at ON executions(started_at DESC)`,
		`CREATE TABLE IF NOT EXISTS execution_logs (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			execution_id TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			level TEXT NOT NULL,
			line TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_execution_logs_execution_time ON execution_logs(execution_id, timestamp, id)`,
		`CREATE TABLE IF NOT EXISTS audit_logs (
			id TEXT PRIMARY KEY,
			actor_id TEXT NOT NULL,
			action TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			diff TEXT NOT NULL DEFAULT '',
			ip TEXT NOT NULL DEFAULT '',
			timestamp INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_audit_logs_timestamp ON audit_logs(timestamp DESC)`,
	}

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin migration tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	for _, stmt := range stmts {
		if _, err := tx.ExecContext(ctx, stmt); err != nil {
			if isIgnorableMigrationErr(stmt, err) {
				continue
			}
			return fmt.Errorf("execute migration statement: %w", err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit migration tx: %w", err)
	}
	return nil
}

func (s *SQLiteRepository) CreateUser(ctx context.Context, user User) (User, error) {
	if user.ID == "" {
		user.ID = uuid.NewString()
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO users (id, username, password_hash, role, locale, created_at, last_login_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		user.ID,
		user.Username,
		user.PasswordHash,
		string(user.Role),
		user.Locale,
		toMillis(user.CreatedAt),
		toMillis(user.LastLoginAt),
	); err != nil {
		if isUniqueConstraintErr(err) {
			return User{}, ErrAlreadyExists
		}
		return User{}, fmt.Errorf("insert user: %w", err)
	}
	return user, nil
}

func (s *SQLiteRepository) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, username, password_hash, role, locale, created_at, last_login_at
		 FROM users
		 ORDER BY username ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list users: %w", err)
	}
	defer rows.Close()

	out := make([]User, 0)
	for rows.Next() {
		user, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, user)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate users: %w", err)
	}
	return out, nil
}

func (s *SQLiteRepository) GetUserByUsername(ctx context.Context, username string) (User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, username, password_hash, role, locale, created_at, last_login_at
		 FROM users WHERE username = ?`,
		username,
	)
	user, err := scanUser(row)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *SQLiteRepository) GetUserByID(ctx context.Context, id string) (User, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, username, password_hash, role, locale, created_at, last_login_at
		 FROM users WHERE id = ?`,
		id,
	)
	user, err := scanUser(row)
	if err != nil {
		return User{}, err
	}
	return user, nil
}

func (s *SQLiteRepository) UpdateUser(ctx context.Context, user User) error {
	current, err := s.GetUserByID(ctx, user.ID)
	if err != nil {
		return err
	}
	if user.PasswordHash == "" {
		user.PasswordHash = current.PasswordHash
	}
	if user.CreatedAt.IsZero() {
		user.CreatedAt = current.CreatedAt
	}

	res, err := s.db.ExecContext(
		ctx,
		`UPDATE users
		 SET username = ?, password_hash = ?, role = ?, locale = ?, created_at = ?, last_login_at = ?
		 WHERE id = ?`,
		user.Username,
		user.PasswordHash,
		string(user.Role),
		user.Locale,
		toMillis(user.CreatedAt),
		toMillis(user.LastLoginAt),
		user.ID,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("update user: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) UpdateUserPassword(ctx context.Context, id, passwordHash string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, id)
	if err != nil {
		return fmt.Errorf("update user password: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) UpdateUserLogin(ctx context.Context, id string, at time.Time) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET last_login_at = ? WHERE id = ?`, toMillis(at), id)
	if err != nil {
		return fmt.Errorf("update user login: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) UpdateUserLocale(ctx context.Context, id, locale string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE users SET locale = ? WHERE id = ?`, locale, id)
	if err != nil {
		return fmt.Errorf("update user locale: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) DeleteUser(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete user: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) ListServers(ctx context.Context) ([]Server, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, name, host, port, ssh_user, key_path, passphrase, tags_json, enabled, paths_json, rclone_remote, rclone_flags_json, created_at
		 FROM servers
		 ORDER BY name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list servers: %w", err)
	}
	defer rows.Close()

	out := make([]Server, 0)
	for rows.Next() {
		srv, err := scanServer(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, srv)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate servers: %w", err)
	}
	return out, nil
}

func (s *SQLiteRepository) GetServer(ctx context.Context, id string) (Server, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, host, port, ssh_user, key_path, passphrase, tags_json, enabled, paths_json, rclone_remote, rclone_flags_json, created_at
		 FROM servers
		 WHERE id = ?`,
		id,
	)
	srv, err := scanServer(row)
	if err != nil {
		return Server{}, err
	}
	return srv, nil
}

func (s *SQLiteRepository) UpsertServer(ctx context.Context, server Server) (Server, error) {
	server.Name = strings.TrimSpace(server.Name)
	if server.Name != "" {
		if idByName, err := s.findServerIDByName(ctx, server.Name); err != nil {
			return Server{}, err
		} else if idByName != "" {
			if server.ID == "" {
				server.ID = idByName
			} else if server.ID != idByName {
				return Server{}, ErrAlreadyExists
			}
		}
	}
	if server.ID == "" {
		server.ID = uuid.NewString()
	}
	if server.CreatedAt.IsZero() {
		server.CreatedAt = time.Now().UTC()
	}

	tagsJSON, err := marshalJSON(server.Tags)
	if err != nil {
		return Server{}, fmt.Errorf("marshal server tags: %w", err)
	}
	pathsJSON, err := marshalJSON(server.Paths)
	if err != nil {
		return Server{}, fmt.Errorf("marshal server paths: %w", err)
	}
	flagsJSON, err := marshalJSON(server.RcloneFlags)
	if err != nil {
		return Server{}, fmt.Errorf("marshal server rclone flags: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO servers (id, name, host, port, ssh_user, key_path, passphrase, tags_json, enabled, paths_json, rclone_remote, rclone_flags_json, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		 ON CONFLICT(id) DO UPDATE SET
		   name = excluded.name,
		   host = excluded.host,
		   port = excluded.port,
		   ssh_user = excluded.ssh_user,
		   key_path = excluded.key_path,
		   passphrase = excluded.passphrase,
		   tags_json = excluded.tags_json,
		   enabled = excluded.enabled,
		   paths_json = excluded.paths_json,
		   rclone_remote = excluded.rclone_remote,
		   rclone_flags_json = excluded.rclone_flags_json,
		   created_at = excluded.created_at`,
		server.ID,
		server.Name,
		server.Host,
		server.Port,
		server.User,
		server.KeyPath,
		server.Passphrase,
		tagsJSON,
		boolToInt(server.Enabled),
		pathsJSON,
		server.RcloneRemote,
		flagsJSON,
		toMillis(server.CreatedAt),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return Server{}, ErrAlreadyExists
		}
		return Server{}, fmt.Errorf("upsert server: %w", err)
	}
	return server, nil
}

func (s *SQLiteRepository) DeleteServer(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM servers WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete server: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) SeedServers(ctx context.Context, servers []Server) error {
	for _, srv := range servers {
		if _, err := s.UpsertServer(ctx, srv); err != nil {
			return err
		}
	}
	return nil
}

func (s *SQLiteRepository) ListPolicies(ctx context.Context) ([]Policy, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, name, server_names_json, paths_json, rclone_remote, rclone_flags_json, timeout_sec, retry_limit, enabled, created_at, updated_at
		 FROM policies
		 ORDER BY created_at ASC, name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list policies: %w", err)
	}
	defer rows.Close()

	out := make([]Policy, 0)
	for rows.Next() {
		p, err := scanPolicy(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate policies: %w", err)
	}
	return out, nil
}

func (s *SQLiteRepository) GetPolicy(ctx context.Context, id string) (Policy, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, server_names_json, paths_json, rclone_remote, rclone_flags_json, timeout_sec, retry_limit, enabled, created_at, updated_at
		 FROM policies
		 WHERE id = ?`,
		id,
	)
	p, err := scanPolicy(row)
	if err != nil {
		return Policy{}, err
	}
	return p, nil
}

func (s *SQLiteRepository) CreatePolicy(ctx context.Context, policy Policy) (Policy, error) {
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

	serverNamesJSON, err := marshalJSON(policy.ServerNames)
	if err != nil {
		return Policy{}, fmt.Errorf("marshal policy server names: %w", err)
	}
	pathsJSON, err := marshalJSON(policy.Paths)
	if err != nil {
		return Policy{}, fmt.Errorf("marshal policy paths: %w", err)
	}
	flagsJSON, err := marshalJSON(policy.RcloneFlags)
	if err != nil {
		return Policy{}, fmt.Errorf("marshal policy rclone flags: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO policies (id, name, server_names_json, paths_json, rclone_remote, rclone_flags_json, timeout_sec, retry_limit, enabled, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		policy.ID,
		policy.Name,
		serverNamesJSON,
		pathsJSON,
		policy.RcloneRemote,
		flagsJSON,
		policy.TimeoutSec,
		policy.RetryLimit,
		boolToInt(policy.Enabled),
		toMillis(policy.CreatedAt),
		toMillis(policy.UpdatedAt),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return Policy{}, ErrAlreadyExists
		}
		return Policy{}, fmt.Errorf("insert policy: %w", err)
	}
	return policy, nil
}

func (s *SQLiteRepository) UpdatePolicy(ctx context.Context, policy Policy) error {
	current, err := s.GetPolicy(ctx, policy.ID)
	if err != nil {
		return err
	}
	if policy.CreatedAt.IsZero() {
		policy.CreatedAt = current.CreatedAt
	}
	policy.UpdatedAt = time.Now().UTC()

	serverNamesJSON, err := marshalJSON(policy.ServerNames)
	if err != nil {
		return fmt.Errorf("marshal policy server names: %w", err)
	}
	pathsJSON, err := marshalJSON(policy.Paths)
	if err != nil {
		return fmt.Errorf("marshal policy paths: %w", err)
	}
	flagsJSON, err := marshalJSON(policy.RcloneFlags)
	if err != nil {
		return fmt.Errorf("marshal policy rclone flags: %w", err)
	}

	res, err := s.db.ExecContext(
		ctx,
		`UPDATE policies
		 SET name = ?, server_names_json = ?, paths_json = ?, rclone_remote = ?, rclone_flags_json = ?, timeout_sec = ?, retry_limit = ?, enabled = ?, created_at = ?, updated_at = ?
		 WHERE id = ?`,
		policy.Name,
		serverNamesJSON,
		pathsJSON,
		policy.RcloneRemote,
		flagsJSON,
		policy.TimeoutSec,
		policy.RetryLimit,
		boolToInt(policy.Enabled),
		toMillis(policy.CreatedAt),
		toMillis(policy.UpdatedAt),
		policy.ID,
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return ErrAlreadyExists
		}
		return fmt.Errorf("update policy: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) DeletePolicy(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM policies WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete policy: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) ListSchedules(ctx context.Context) ([]Schedule, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, name, policy_id, server_names_json, cron_expr, timezone, enabled, misfire_policy, last_run_at, next_run_at, created_at, updated_at
		 FROM schedules
		 ORDER BY created_at ASC, name ASC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list schedules: %w", err)
	}
	defer rows.Close()

	out := make([]Schedule, 0)
	for rows.Next() {
		schedule, err := scanSchedule(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, schedule)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate schedules: %w", err)
	}
	return out, nil
}

func (s *SQLiteRepository) GetSchedule(ctx context.Context, id string) (Schedule, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, name, policy_id, server_names_json, cron_expr, timezone, enabled, misfire_policy, last_run_at, next_run_at, created_at, updated_at
		 FROM schedules
		 WHERE id = ?`,
		id,
	)
	schedule, err := scanSchedule(row)
	if err != nil {
		return Schedule{}, err
	}
	return schedule, nil
}

func (s *SQLiteRepository) CreateSchedule(ctx context.Context, schedule Schedule) (Schedule, error) {
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

	serverNamesJSON, err := marshalJSON(schedule.ServerNames)
	if err != nil {
		return Schedule{}, fmt.Errorf("marshal schedule server names: %w", err)
	}

	_, err = s.db.ExecContext(
		ctx,
		`INSERT INTO schedules (id, name, policy_id, server_names_json, cron_expr, timezone, enabled, misfire_policy, last_run_at, next_run_at, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		schedule.ID,
		schedule.Name,
		schedule.PolicyID,
		serverNamesJSON,
		schedule.CronExpr,
		schedule.Timezone,
		boolToInt(schedule.Enabled),
		schedule.MisfirePolicy,
		toMillis(schedule.LastRunAt),
		toMillis(schedule.NextRunAt),
		toMillis(schedule.CreatedAt),
		toMillis(schedule.UpdatedAt),
	)
	if err != nil {
		if isUniqueConstraintErr(err) {
			return Schedule{}, ErrAlreadyExists
		}
		return Schedule{}, fmt.Errorf("insert schedule: %w", err)
	}
	return schedule, nil
}

func (s *SQLiteRepository) UpdateSchedule(ctx context.Context, schedule Schedule) error {
	schedule.UpdatedAt = time.Now().UTC()
	serverNamesJSON, err := marshalJSON(schedule.ServerNames)
	if err != nil {
		return fmt.Errorf("marshal schedule server names: %w", err)
	}

	res, err := s.db.ExecContext(
		ctx,
		`UPDATE schedules
		 SET name = ?, policy_id = ?, server_names_json = ?, cron_expr = ?, timezone = ?, enabled = ?, misfire_policy = ?, last_run_at = ?, next_run_at = ?, created_at = ?, updated_at = ?
		 WHERE id = ?`,
		schedule.Name,
		schedule.PolicyID,
		serverNamesJSON,
		schedule.CronExpr,
		schedule.Timezone,
		boolToInt(schedule.Enabled),
		schedule.MisfirePolicy,
		toMillis(schedule.LastRunAt),
		toMillis(schedule.NextRunAt),
		toMillis(schedule.CreatedAt),
		toMillis(schedule.UpdatedAt),
		schedule.ID,
	)
	if err != nil {
		return fmt.Errorf("update schedule: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) DeleteSchedule(ctx context.Context, id string) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM schedules WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("delete schedule: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) CreateExecution(ctx context.Context, ex Execution) (Execution, error) {
	if ex.ID == "" {
		ex.ID = uuid.NewString()
	}
	if ex.StartedAt.IsZero() {
		ex.StartedAt = time.Now().UTC()
	}
	serverNamesJSON, err := marshalJSON(ex.ServerNames)
	if err != nil {
		return Execution{}, fmt.Errorf("marshal execution server names: %w", err)
	}
	resultsJSON, err := marshalJSON(ex.Results)
	if err != nil {
		return Execution{}, fmt.Errorf("marshal execution results: %w", err)
	}

	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO executions (id, policy_id, status, trigger_type, server_names_json, started_at, ended_at, duration_ms, error_text, results_json)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		ex.ID,
		ex.PolicyID,
		string(ex.Status),
		ex.TriggerType,
		serverNamesJSON,
		toMillis(ex.StartedAt),
		toMillis(ex.EndedAt),
		ex.DurationMS,
		ex.Error,
		resultsJSON,
	); err != nil {
		if isUniqueConstraintErr(err) {
			return Execution{}, ErrAlreadyExists
		}
		return Execution{}, fmt.Errorf("insert execution: %w", err)
	}
	return ex, nil
}

func (s *SQLiteRepository) UpdateExecution(ctx context.Context, ex Execution) error {
	serverNamesJSON, err := marshalJSON(ex.ServerNames)
	if err != nil {
		return fmt.Errorf("marshal execution server names: %w", err)
	}
	resultsJSON, err := marshalJSON(ex.Results)
	if err != nil {
		return fmt.Errorf("marshal execution results: %w", err)
	}

	res, err := s.db.ExecContext(
		ctx,
		`UPDATE executions
		 SET policy_id = ?, status = ?, trigger_type = ?, server_names_json = ?, started_at = ?, ended_at = ?, duration_ms = ?, error_text = ?, results_json = ?
		 WHERE id = ?`,
		ex.PolicyID,
		string(ex.Status),
		ex.TriggerType,
		serverNamesJSON,
		toMillis(ex.StartedAt),
		toMillis(ex.EndedAt),
		ex.DurationMS,
		ex.Error,
		resultsJSON,
		ex.ID,
	)
	if err != nil {
		return fmt.Errorf("update execution: %w", err)
	}
	affected, err := rowsAffected(res)
	if err != nil {
		return err
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *SQLiteRepository) GetExecution(ctx context.Context, id string) (Execution, error) {
	row := s.db.QueryRowContext(
		ctx,
		`SELECT id, policy_id, status, trigger_type, server_names_json, started_at, ended_at, duration_ms, error_text, results_json
		 FROM executions
		 WHERE id = ?`,
		id,
	)
	exec, err := scanExecution(row)
	if err != nil {
		return Execution{}, err
	}
	return exec, nil
}

func (s *SQLiteRepository) ListExecutions(ctx context.Context) ([]Execution, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, policy_id, status, trigger_type, server_names_json, started_at, ended_at, duration_ms, error_text, results_json
		 FROM executions
		 ORDER BY started_at DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list executions: %w", err)
	}
	defer rows.Close()

	out := make([]Execution, 0)
	for rows.Next() {
		exec, err := scanExecution(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, exec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate executions: %w", err)
	}
	return out, nil
}

func (s *SQLiteRepository) AppendExecutionLog(ctx context.Context, log ExecutionLog) error {
	if log.Timestamp.IsZero() {
		log.Timestamp = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO execution_logs (execution_id, timestamp, level, line) VALUES (?, ?, ?, ?)`,
		log.ExecutionID,
		toMillis(log.Timestamp),
		log.Level,
		log.Line,
	); err != nil {
		return fmt.Errorf("insert execution log: %w", err)
	}
	return nil
}

func (s *SQLiteRepository) ListExecutionLogs(ctx context.Context, executionID string) ([]ExecutionLog, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT execution_id, timestamp, level, line
		 FROM execution_logs
		 WHERE execution_id = ?
		 ORDER BY timestamp ASC, id ASC`,
		executionID,
	)
	if err != nil {
		return nil, fmt.Errorf("list execution logs: %w", err)
	}
	defer rows.Close()

	out := make([]ExecutionLog, 0)
	for rows.Next() {
		var logRec ExecutionLog
		var ts int64
		if err := rows.Scan(&logRec.ExecutionID, &ts, &logRec.Level, &logRec.Line); err != nil {
			return nil, fmt.Errorf("scan execution log: %w", err)
		}
		logRec.Timestamp = fromMillis(ts)
		out = append(out, logRec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate execution logs: %w", err)
	}
	return out, nil
}

func (s *SQLiteRepository) CreateAuditLog(ctx context.Context, logRec AuditLog) (AuditLog, error) {
	if logRec.ID == "" {
		logRec.ID = uuid.NewString()
	}
	if logRec.Timestamp.IsZero() {
		logRec.Timestamp = time.Now().UTC()
	}
	if _, err := s.db.ExecContext(
		ctx,
		`INSERT INTO audit_logs (id, actor_id, action, resource_type, resource_id, diff, ip, timestamp)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		logRec.ID,
		logRec.ActorID,
		logRec.Action,
		logRec.ResourceType,
		logRec.ResourceID,
		logRec.Diff,
		logRec.IP,
		toMillis(logRec.Timestamp),
	); err != nil {
		if isUniqueConstraintErr(err) {
			return AuditLog{}, ErrAlreadyExists
		}
		return AuditLog{}, fmt.Errorf("insert audit log: %w", err)
	}
	return logRec, nil
}

func (s *SQLiteRepository) ListAuditLogs(ctx context.Context) ([]AuditLog, error) {
	rows, err := s.db.QueryContext(
		ctx,
		`SELECT id, actor_id, action, resource_type, resource_id, diff, ip, timestamp
		 FROM audit_logs
		 ORDER BY timestamp DESC`,
	)
	if err != nil {
		return nil, fmt.Errorf("list audit logs: %w", err)
	}
	defer rows.Close()

	out := make([]AuditLog, 0)
	for rows.Next() {
		var logRec AuditLog
		var ts int64
		if err := rows.Scan(
			&logRec.ID,
			&logRec.ActorID,
			&logRec.Action,
			&logRec.ResourceType,
			&logRec.ResourceID,
			&logRec.Diff,
			&logRec.IP,
			&ts,
		); err != nil {
			return nil, fmt.Errorf("scan audit log: %w", err)
		}
		logRec.Timestamp = fromMillis(ts)
		out = append(out, logRec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate audit logs: %w", err)
	}
	return out, nil
}

func (s *SQLiteRepository) findServerIDByName(ctx context.Context, name string) (string, error) {
	row := s.db.QueryRowContext(ctx, `SELECT id FROM servers WHERE name = ?`, name)
	var id string
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", nil
		}
		return "", fmt.Errorf("find server by name: %w", err)
	}
	return id, nil
}

func scanUser(scanner interface{ Scan(dest ...any) error }) (User, error) {
	var user User
	var createdAtMS, lastLoginAtMS int64
	var role string
	err := scanner.Scan(
		&user.ID,
		&user.Username,
		&user.PasswordHash,
		&role,
		&user.Locale,
		&createdAtMS,
		&lastLoginAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return User{}, ErrNotFound
		}
		return User{}, fmt.Errorf("scan user: %w", err)
	}
	user.Role = Role(role)
	user.CreatedAt = fromMillis(createdAtMS)
	user.LastLoginAt = fromMillis(lastLoginAtMS)
	return user, nil
}

func scanServer(scanner interface{ Scan(dest ...any) error }) (Server, error) {
	var server Server
	var tagsJSON, pathsJSON, flagsJSON string
	var port int64
	var enabled int64
	var createdAtMS int64
	err := scanner.Scan(
		&server.ID,
		&server.Name,
		&server.Host,
		&port,
		&server.User,
		&server.KeyPath,
		&server.Passphrase,
		&tagsJSON,
		&enabled,
		&pathsJSON,
		&server.RcloneRemote,
		&flagsJSON,
		&createdAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Server{}, ErrNotFound
		}
		return Server{}, fmt.Errorf("scan server: %w", err)
	}
	server.Port = int(port)
	server.Enabled = enabled != 0
	server.CreatedAt = fromMillis(createdAtMS)
	if err := unmarshalJSON(tagsJSON, &server.Tags); err != nil {
		return Server{}, fmt.Errorf("decode server tags: %w", err)
	}
	if err := unmarshalJSON(pathsJSON, &server.Paths); err != nil {
		return Server{}, fmt.Errorf("decode server paths: %w", err)
	}
	if err := unmarshalJSON(flagsJSON, &server.RcloneFlags); err != nil {
		return Server{}, fmt.Errorf("decode server rclone flags: %w", err)
	}
	return server, nil
}

func scanSchedule(scanner interface{ Scan(dest ...any) error }) (Schedule, error) {
	var schedule Schedule
	var serverNamesJSON string
	var enabled int64
	var lastRunAtMS, nextRunAtMS, createdAtMS, updatedAtMS int64
	err := scanner.Scan(
		&schedule.ID,
		&schedule.Name,
		&schedule.PolicyID,
		&serverNamesJSON,
		&schedule.CronExpr,
		&schedule.Timezone,
		&enabled,
		&schedule.MisfirePolicy,
		&lastRunAtMS,
		&nextRunAtMS,
		&createdAtMS,
		&updatedAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Schedule{}, ErrNotFound
		}
		return Schedule{}, fmt.Errorf("scan schedule: %w", err)
	}
	schedule.Enabled = enabled != 0
	schedule.LastRunAt = fromMillis(lastRunAtMS)
	schedule.NextRunAt = fromMillis(nextRunAtMS)
	schedule.CreatedAt = fromMillis(createdAtMS)
	schedule.UpdatedAt = fromMillis(updatedAtMS)
	if err := unmarshalJSON(serverNamesJSON, &schedule.ServerNames); err != nil {
		return Schedule{}, fmt.Errorf("decode schedule server names: %w", err)
	}
	return schedule, nil
}

func scanExecution(scanner interface{ Scan(dest ...any) error }) (Execution, error) {
	var exec Execution
	var status string
	var serverNamesJSON, resultsJSON string
	var startedAtMS, endedAtMS int64
	err := scanner.Scan(
		&exec.ID,
		&exec.PolicyID,
		&status,
		&exec.TriggerType,
		&serverNamesJSON,
		&startedAtMS,
		&endedAtMS,
		&exec.DurationMS,
		&exec.Error,
		&resultsJSON,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Execution{}, ErrNotFound
		}
		return Execution{}, fmt.Errorf("scan execution: %w", err)
	}
	exec.Status = ExecutionStatus(status)
	exec.StartedAt = fromMillis(startedAtMS)
	exec.EndedAt = fromMillis(endedAtMS)
	if err := unmarshalJSON(serverNamesJSON, &exec.ServerNames); err != nil {
		return Execution{}, fmt.Errorf("decode execution server names: %w", err)
	}
	if err := unmarshalJSON(resultsJSON, &exec.Results); err != nil {
		return Execution{}, fmt.Errorf("decode execution results: %w", err)
	}
	return exec, nil
}

func scanPolicy(scanner interface{ Scan(dest ...any) error }) (Policy, error) {
	var policy Policy
	var serverNamesJSON, pathsJSON, flagsJSON string
	var enabled int64
	var createdAtMS, updatedAtMS int64
	err := scanner.Scan(
		&policy.ID,
		&policy.Name,
		&serverNamesJSON,
		&pathsJSON,
		&policy.RcloneRemote,
		&flagsJSON,
		&policy.TimeoutSec,
		&policy.RetryLimit,
		&enabled,
		&createdAtMS,
		&updatedAtMS,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return Policy{}, ErrNotFound
		}
		return Policy{}, fmt.Errorf("scan policy: %w", err)
	}
	policy.Enabled = enabled != 0
	policy.CreatedAt = fromMillis(createdAtMS)
	policy.UpdatedAt = fromMillis(updatedAtMS)
	if err := unmarshalJSON(serverNamesJSON, &policy.ServerNames); err != nil {
		return Policy{}, fmt.Errorf("decode policy server names: %w", err)
	}
	if err := unmarshalJSON(pathsJSON, &policy.Paths); err != nil {
		return Policy{}, fmt.Errorf("decode policy paths: %w", err)
	}
	if err := unmarshalJSON(flagsJSON, &policy.RcloneFlags); err != nil {
		return Policy{}, fmt.Errorf("decode policy rclone flags: %w", err)
	}
	return policy, nil
}

func ensureSQLiteDir(dsn string) error {
	path, ok := sqlitePathFromDSN(dsn)
	if !ok {
		return nil
	}
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create sqlite directory: %w", err)
	}
	return nil
}

func sqlitePathFromDSN(dsn string) (string, bool) {
	raw := strings.TrimSpace(dsn)
	if raw == "" || raw == ":memory:" {
		return "", false
	}
	if strings.Contains(strings.ToLower(raw), "mode=memory") {
		return "", false
	}
	if strings.HasPrefix(raw, "file:") {
		raw = strings.TrimPrefix(raw, "file:")
	}
	if idx := strings.Index(raw, "?"); idx >= 0 {
		raw = raw[:idx]
	}
	if raw == "" || raw == ":memory:" {
		return "", false
	}
	if len(raw) >= 3 && raw[0] == '/' && raw[2] == ':' {
		raw = raw[1:]
	}
	return filepath.Clean(raw), true
}

func rowsAffected(res sql.Result) (int64, error) {
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("read rows affected: %w", err)
	}
	return affected, nil
}

func marshalJSON(v any) (string, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(payload), nil
}

func unmarshalJSON(raw string, target any) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		raw = "null"
	}
	return json.Unmarshal([]byte(raw), target)
}

func toMillis(t time.Time) int64 {
	if t.IsZero() {
		return 0
	}
	return t.UTC().UnixMilli()
}

func fromMillis(ms int64) time.Time {
	if ms <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(ms).UTC()
}

func boolToInt(v bool) int {
	if v {
		return 1
	}
	return 0
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "unique constraint failed")
}

func isIgnorableMigrationErr(stmt string, err error) bool {
	if err == nil {
		return false
	}
	up := strings.ToUpper(strings.TrimSpace(stmt))
	msg := strings.ToLower(err.Error())
	if strings.HasPrefix(up, "ALTER TABLE SERVERS ADD COLUMN PASSPHRASE") {
		return strings.Contains(msg, "duplicate column name")
	}
	if strings.HasPrefix(up, "ALTER TABLE SCHEDULES ADD COLUMN POLICY_ID") {
		return strings.Contains(msg, "duplicate column name")
	}
	if strings.HasPrefix(up, "ALTER TABLE EXECUTIONS ADD COLUMN POLICY_ID") {
		return strings.Contains(msg, "duplicate column name")
	}
	return false
}

func (s *SQLiteRepository) ApplyRetention(ctx context.Context, executionCutoff, logCutoff, auditCutoff time.Time) (RetentionReport, error) {
	report := RetentionReport{}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return report, fmt.Errorf("begin retention tx: %w", err)
	}
	defer func() {
		_ = tx.Rollback()
	}()

	if res, err := tx.ExecContext(ctx, `DELETE FROM executions WHERE started_at < ?`, toMillis(executionCutoff)); err != nil {
		return report, fmt.Errorf("delete old executions: %w", err)
	} else if n, err := res.RowsAffected(); err == nil {
		report.Executions = int(n)
	}
	if res, err := tx.ExecContext(ctx, `DELETE FROM execution_logs WHERE timestamp < ?`, toMillis(logCutoff)); err != nil {
		return report, fmt.Errorf("delete old execution logs: %w", err)
	} else if n, err := res.RowsAffected(); err == nil {
		report.ExecutionLogs = int(n)
	}
	if res, err := tx.ExecContext(ctx, `DELETE FROM audit_logs WHERE timestamp < ?`, toMillis(auditCutoff)); err != nil {
		return report, fmt.Errorf("delete old audit logs: %w", err)
	} else if n, err := res.RowsAffected(); err == nil {
		report.AuditLogs = int(n)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM execution_logs WHERE execution_id NOT IN (SELECT id FROM executions)`); err != nil {
		return report, fmt.Errorf("delete orphan execution logs: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return report, fmt.Errorf("commit retention tx: %w", err)
	}
	return report, nil
}
