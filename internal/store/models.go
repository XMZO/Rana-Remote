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

// SystemSettings holds all runtime-configurable settings.
// Stored in the database (system_settings table) instead of config.yaml.
type SystemSettings struct {
	// Global settings
	GlobalTimeout            string   `json:"global_timeout"`
	GlobalConcurrency        int      `json:"global_concurrency"`
	GlobalSSHStrictHostKey   bool     `json:"global_ssh_strict_host_key"`
	GlobalSSHKnownHostsPath  string   `json:"global_ssh_known_hosts_path"`

	// Web settings
	WebCSRFEnabled           bool     `json:"web_csrf_enabled"`
	WebCORSAllowOrigins      []string `json:"web_cors_allow_origins"`
	WebIPAllowList           []string `json:"web_ip_allow_list"`

	// Module settings
	ModulesSchedule          bool     `json:"modules_schedule"`
	ModulesAudit             bool     `json:"modules_audit"`
	ModulesNotify            bool     `json:"modules_notify"`
	ModulesUsers             bool     `json:"modules_users"`

	// I18N settings
	I18NDefaultLocale        string   `json:"i18n_default_locale"`

	// Notify settings
	NotifyWebhookURL         string   `json:"notify_webhook_url"`
	NotifyOnSuccess          bool     `json:"notify_on_success"`
	NotifyOnFailure          bool     `json:"notify_on_failure"`
	NotifySuppressionWindow  string   `json:"notify_suppression_window"`
	NotifyEmailEnabled       bool     `json:"notify_email_enabled"`
	NotifyEmailSMTPHost      string   `json:"notify_email_smtp_host"`
	NotifyEmailSMTPPort      int      `json:"notify_email_smtp_port"`
	NotifyEmailUsername      string   `json:"notify_email_username"`
	NotifyEmailPassword      string   `json:"notify_email_password"`
	NotifyEmailFrom          string   `json:"notify_email_from"`
	NotifyEmailTo            []string `json:"notify_email_to"`
	NotifyEmailUseTLS        bool     `json:"notify_email_use_tls"`

	// Timestamp for tracking last update
	UpdatedAt                time.Time `json:"updated_at"`
}
