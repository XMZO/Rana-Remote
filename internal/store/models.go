package store

import "time"

type Role string

const (
	RoleAdmin    Role = "admin"
	RoleOperator Role = "operator"
	RoleViewer   Role = "viewer"
)

type ExecutionStatus string

const (
	StatusPending  ExecutionStatus = "pending"
	StatusRunning  ExecutionStatus = "running"
	StatusRetrying ExecutionStatus = "retrying"
	StatusSuccess  ExecutionStatus = "success"
	StatusFailed   ExecutionStatus = "failed"
	StatusCanceled ExecutionStatus = "canceled"
)

type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	Role         Role      `json:"role"`
	Locale       string    `json:"locale"`
	CreatedAt    time.Time `json:"created_at"`
	LastLoginAt  time.Time `json:"last_login_at"`
}

type Server struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	Host         string    `json:"host"`
	Port         int       `json:"port"`
	User         string    `json:"user"`
	KeyPath      string    `json:"key_path"`
	Passphrase   string    `json:"passphrase,omitempty"`
	Tags         []string  `json:"tags,omitempty"`
	Enabled      bool      `json:"enabled"`
	Paths        []string  `json:"paths"`
	RcloneRemote string    `json:"rclone_remote"`
	RcloneFlags  []string  `json:"rclone_flags,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
}

type Policy struct {
	ID           string    `json:"id"`
	Name         string    `json:"name"`
	ServerNames  []string  `json:"server_names"`
	Paths        []string  `json:"paths"`
	RcloneRemote string    `json:"rclone_remote"`
	RcloneFlags  []string  `json:"rclone_flags,omitempty"`
	TimeoutSec   int       `json:"timeout_sec"`
	RetryLimit   int       `json:"retry_limit"`
	Enabled      bool      `json:"enabled"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

type Schedule struct {
	ID            string    `json:"id"`
	Name          string    `json:"name"`
	PolicyID      string    `json:"policy_id,omitempty"`
	ServerNames   []string  `json:"server_names"`
	CronExpr      string    `json:"cron_expr"`
	Timezone      string    `json:"timezone"`
	Enabled       bool      `json:"enabled"`
	MisfirePolicy string    `json:"misfire_policy"`
	LastRunAt     time.Time `json:"last_run_at,omitempty"`
	NextRunAt     time.Time `json:"next_run_at,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type Execution struct {
	ID          string          `json:"id"`
	PolicyID    string          `json:"policy_id,omitempty"`
	Status      ExecutionStatus `json:"status"`
	TriggerType string          `json:"trigger_type"`
	ServerNames []string        `json:"server_names"`
	StartedAt   time.Time       `json:"started_at"`
	EndedAt     time.Time       `json:"ended_at,omitempty"`
	DurationMS  int64           `json:"duration_ms,omitempty"`
	Error       string          `json:"error,omitempty"`
	Results     []Result        `json:"results,omitempty"`
}

type Result struct {
	Server     string `json:"server"`
	Success    bool   `json:"success"`
	DurationMS int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

type ExecutionLog struct {
	ExecutionID string    `json:"execution_id"`
	Timestamp   time.Time `json:"timestamp"`
	Level       string    `json:"level"`
	Line        string    `json:"line"`
}

type RetentionReport struct {
	Executions    int `json:"executions"`
	ExecutionLogs int `json:"execution_logs"`
	AuditLogs     int `json:"audit_logs"`
}

type AuditLog struct {
	ID           string    `json:"id"`
	ActorID      string    `json:"actor_id"`
	Action       string    `json:"action"`
	ResourceType string    `json:"resource_type"`
	ResourceID   string    `json:"resource_id"`
	Diff         string    `json:"diff,omitempty"`
	IP           string    `json:"ip,omitempty"`
	TraceID      string    `json:"trace_id,omitempty"`
	Timestamp    time.Time `json:"timestamp"`
}
