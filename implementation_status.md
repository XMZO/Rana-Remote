# Implementation Status

This file reflects current code reality against `implementation_plan.md`.

## Phase 2: Settings Persistence Migration (DONE)

### Settings → DB Migration (Completed)
- **PUT /api/v1/settings persistence**: Now writes to SQLite DB instead of YAML
- **System restart**: Successfully loads settings from DB on startup
- **Web settings page**: Works with DB-backed settings
- **DB models**: New `settings` table with all runtime settings
- **Config system**: Downgraded to defaults-only role (no longer primary persistence)

#### Files Changed:
- `internal/store/models.go` - Added `Settings` struct
- `internal/store/repository.go` - Added `GetSettings`/`UpsertSettings` to interface and MemoryRepository
- `internal/store/sqlite_repository.go` - Added `settings` table migration and implementations
- `internal/api/handler_settings.go` - Complete rewrite to use DB instead of YAML
- `cmd/rana-api/main.go` - Added `loadSettingsFromDB()` on startup
- `internal/api/settings_test.go` - Updated tests for DB persistence

#### Settings Now Stored in DB:
| Setting | DB Column | Notes |
|---------|-----------|-------|
| Global Timeout | `global_timeout` | |
| Global Concurrency | `global_concurrency` | |
| SSH Strict Host Key | `global_ssh_strict_host_key` | |
| SSH Known Hosts Path | `global_ssh_known_hosts_path` | |
| Web CSRF Enabled | `web_csrf_enabled` | |
| Web CORS Allow Origins | `web_cors_allow_origins_json` | JSON array |
| Web IP Allow List | `web_ip_allow_list_json` | JSON array |
| Modules Schedule | `modules_schedule` | |
| Modules Audit | `modules_audit` | |
| Modules Notify | `modules_notify` | |
| Modules Users | `modules_users` | |
| I18N Default Locale | `i18n_default_locale` | |
| Notify Webhook URL | `notify_webhook_url` | |
| Notify Email Enabled | `notify_email_enabled` | |
| Notify Email SMTP Host | `notify_email_smtp_host` | |
| Notify Email SMTP Port | `notify_email_smtp_port` | |
| Notify Email Username | `notify_email_username` | |
| Notify Email Password | `notify_email_password` | |
| Notify Email From | `notify_email_from` | |
| Notify Email To | `notify_email_to_json` | JSON array |
| Notify Email Use TLS | `notify_email_use_tls` | |
| Notify On Success | `notify_on_success` | |
| Notify On Failure | `notify_on_failure` | |
| Notify Suppression Window | `notify_suppression_window` | |
| Updated At | `updated_at` | Timestamp |

#### Transition Items (Minimal Impact):
- Config file (`config.yaml`) is still created for YAML fallback on first boot
- YAML config still provides defaults for settings not yet in DB
- Legacy YAML config can be manually edited (will be overridden by DB on restart)
- The `config.yaml` file path is still tracked but no longer the primary persistence layer

#### Architecture:
```
[PUT /api/v1/settings]
       ↓
[Validate in handler_settings.go]
       ↓
[UpsertSettings() → SQLite settings table]
       ↓
[Sync to in-memory config.Config]
       ↓
[Audit log]

[System Startup]
       ↓
[loadSettingsFromDB() → Sync to in-memory config]
       ↓
[Server starts with DB-backed settings]
```

## P0 (Remaining)
- Real E2E / integration loop: **Partial**
  - Evidence: `web/playwright.config.js`, `web/e2e/smoke.spec.js`, `scripts/e2e_server.py`, `.github/workflows/ci.yml`, `web/package.json`
  - Notes: no-op E2E replaced with runnable Playwright scaffold and real browser interaction against a minimal local test server, not yet the full Go backend loop.
- Audit log export endpoint + frontend download: **Partial**
  - Evidence: backend already had text download path; frontend already had download button. Structured export still remains.
- Log protocol issue: **Partial**
  - Evidence: current runtime is SSE, not true WebSocket.
- Trace-id end-to-end: **Partial**
  - Evidence: `internal/api/middleware.go`, `internal/store/models.go`
- Module disable behavior / feature_disabled / README truthfulness: **Partial**

## P1 (Remaining)
- Server group: **Not Done**
- Misfire / schedule semantics hardening: **Not Done in this pass**
- Env-first deployment entry (`docker-compose.yml` + `.env`, auto-init internal config): **Done**
  - Evidence: `docker-compose.yml`, `.env.example`, `cmd/rana-api/main.go`, `cmd/rana/main.go`, `internal/config/config.go`, `README.md`
  - Notes: default deployment no longer mounts `config.example.yaml`; runtime settings now persisted to DB, not YAML.

## Explicitly Not Done
- True WebSocket log streaming
- Full Playwright against real Go backend state
- Server group model/API/UI
- PG/HA/distributed scheduler work
