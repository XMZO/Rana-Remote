# Rana-Remote

Web-first remote backup orchestrator. The control plane runs on one host and executes agentless SSH backups on remote servers.

## Features

- Web-first control plane (`rana-api`) with i18n (`zh-CN` / `en-US`)
- Modular capabilities (`backup`, `schedule`, `audit`, `notify`, `users`)
- Agentless backup flow: `tar.gz -> rclone copy -> cleanup`
- Concurrent runner with timeout and per-host streaming logs
- CLI (`rana`) kept as compatibility mode: `dry-run`, one-shot run, connectivity check

## Container image

Published image:

```text
ghcr.io/xmzo/rana-remote:latest
```

GitHub Actions builds and pushes a multi-architecture image for:

- `linux/amd64`
- `linux/arm64`

On pushes, the workflow also publishes branch tags and commit SHA tags. On the default branch, it additionally updates `latest`.

## Quick start with Docker Compose

You do **not** need to build locally.

1. Prepare `.env` and data directory:

```bash
cp .env.example .env
mkdir -p data
```

2. Edit `.env` and set the deployment secrets:

```dotenv
RANA_IMAGE=ghcr.io/xmzo/rana-remote:latest
RANA_ADMIN_USER=admin
RANA_ADMIN_PASS=change_me_now
RANA_JWT_SECRET=change_me_please_with_a_long_random_string
RANA_DATA_KEY=change_me_with_a_long_random_string
```

3. Start directly from GHCR:

```bash
docker compose up -d
```

4. Open the web UI and finish the rest in Web Settings / Servers / Policies:

```text
http://localhost:8080/
```

The default `docker-compose.yml` is now env-first: it pulls `ghcr.io/xmzo/rana-remote:latest`, stores runtime data in `./data`, and auto-initializes `/data/config.yaml` with built-in defaults on first boot. Users no longer need to copy `config.yaml` for the normal deployment path.

## Optional: local image build

If you want to build from the local source tree instead of pulling GHCR:

```bash
docker compose -f docker-compose.yml -f docker-compose.build.yml up --build
```

You can also override the pulled image explicitly:

```bash
RANA_IMAGE=ghcr.io/xmzo/rana-remote:latest docker compose up -d
```

## Quick start without Docker

The non-Docker path also supports env-first startup now.

1. Set required environment variables:

```bash
export RANA_ADMIN_USER=admin
export RANA_ADMIN_PASS='change_me_now'
export RANA_JWT_SECRET='change_me_please_with_a_long_random_string'
export RANA_DATA_KEY='change_me_with_a_long_random_string'
mkdir -p data
```

2. Run API:

```bash
go run ./cmd/rana-api -migrate
```

3. Open the web UI at `http://localhost:8080/`

By default the API now auto-creates `./data/config.yaml` with minimal internal defaults if the file does not exist. `config.example.yaml` remains for compatibility / advanced manual editing, not as the primary deployment entry.

## Release pipeline

Repository includes `.github/workflows/docker-publish.yml`.

Workflow behavior:

1. Trigger on every push and manual dispatch.
2. Check out the repository.
3. Set up QEMU and Docker Buildx.
4. Log in to GitHub Container Registry using `GITHUB_TOKEN`.
5. Build a multi-arch image from `Dockerfile` for `linux/amd64,linux/arm64`.
6. Push image tags to GHCR.
7. Reuse GitHub Actions cache for faster rebuilds.

The Dockerfile is Buildx-ready and uses `TARGETOS` / `TARGETARCH` so cross-platform builds produce the correct Go binaries.

## API

- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `POST /api/v1/auth/logout`
- `GET /api/v1/me`
- `PUT /api/v1/me/locale`
- `GET /api/v1/i18n/locales`
- `GET /api/v1/modules`
- `GET /api/v1/servers`
- `POST /api/v1/servers`
- `GET /api/v1/servers/{id}`
- `PUT /api/v1/servers/{id}`
- `DELETE /api/v1/servers/{id}`
- `POST /api/v1/servers/{id}/test-connection`
- `GET /api/v1/policies`
- `POST /api/v1/policies`
- `GET /api/v1/policies/{id}`
- `PUT /api/v1/policies/{id}`
- `DELETE /api/v1/policies/{id}`
- `GET /api/v1/users`
- `POST /api/v1/users`
- `GET /api/v1/users/{id}`
- `PUT /api/v1/users/{id}`
- `DELETE /api/v1/users/{id}`
- `PUT /api/v1/users/{id}/password`
- `GET /api/v1/settings`
- `PUT /api/v1/settings`
- `POST /api/v1/executions`
- `GET /api/v1/executions`
- `GET /api/v1/executions/{id}`
- `GET /api/v1/executions/{id}/logs`
- `GET /api/v1/executions/{id}/stream` (SSE)
- `POST /api/v1/executions/{id}/cancel`
- `POST /api/v1/executions/{id}/retry`
- `GET /api/v1/schedules`
- `POST /api/v1/schedules`
- `GET /api/v1/schedules/{id}`
- `PUT /api/v1/schedules/{id}`
- `DELETE /api/v1/schedules/{id}`
- `GET /api/v1/audit-logs`

## Notes

- Remote servers must have `bash`, `tar`, and `rclone` installed.
- Strict host key check is enabled by default.
- `database.driver` supports `memory` and `sqlite` (real SQLite database).
- Schedule expressions support: `@every 5m`, `5m`, and a cron subset like `*/5 * * * *`.
- List APIs support `page` / `page_size` (default 20, max 200).
- Write APIs support `Idempotency-Key` replay.
- When `web.csrf_enabled=true`, unsafe methods require `rana_csrf` cookie + `X-CSRF-Token` header.
- `web.ip_allow_list` supports IP/CIDR allow-list (applies to all HTTP endpoints).
- Login is IP rate-limited by `auth.login_rate_limit`.
- If `RANA_DATA_KEY` is set, server passphrases are stored encrypted at rest.
- Execution trigger supports `policy_id` (policy-based paths/remote/flags/timeout/retry).
- `notify.webhook_url` and `notify.email` support execution success/failure notifications with suppression window.

- Runtime settings edited in Web are persisted to the SQLite database (`/data/rana.db` in Docker by default). Settings survive restarts without requiring config file persistence.

- `config.yaml` is generated on first boot with internal defaults only; it is not the primary settings persistence layer.

- `config.example.yaml` is a compatibility / advanced reference file, not the main user deployment entry.

- Web UI is served from `web/dist` and includes auth, server/policy/user/settings, executions, schedules, audit, logs stream/download, and i18n switch.

## Deployment model transition

Primary deployment path is now:

1. prepare `.env`
2. `docker compose up -d`
3. log into Web and configure servers / policies / runtime settings there

What moved out of the main deployment path:

- copying `config.example.yaml`
- predeclaring `servers` in YAML
- predeclaring `notify` settings in YAML
- predeclaring most `web`, `modules`, and `i18n` runtime knobs in YAML
- using `config.yaml` as the main operator-facing setup file

### Settings Persistence

Settings are now stored in the SQLite database (`/data/rana.db`), not in `config.yaml`. This means:

- Settings persist across restarts automatically
- No need to manually edit YAML files for runtime configuration
- `PUT /api/v1/settings` writes to the database
- System startup reads settings from DB to populate runtime config

### Config File Role (Reduced)

The `config.yaml` file is now only used for:

- Initial default values (on first boot or when DB has no settings)
- `database.*`, `auth.bootstrap_admin.*`, and `global.temp_dir` (still read from YAML)
- Compatibility: advanced operators can still provide `-c path/to/config.yaml`

Note: If both YAML config and DB settings exist, DB settings take precedence on startup.

## Implementation status

See `implementation_status.md` for a code-backed Done / Partial / Not Done matrix. Browser E2E is now scaffolded instead of no-op, but it is not yet a full production-backend loop.
