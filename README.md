# Rana-Remote

Web-first remote backup orchestrator. Control plane runs on one host and executes agentless SSH backups on remote servers.

## Features

- Web-first control plane (`rana-api`) with i18n (`zh-CN` / `en-US`)
- Modular capabilities (`backup`, `schedule`, `audit`, `notify`, `users`)
- Agentless backup flow: `tar.gz -> rclone copy -> cleanup`
- Concurrent runner with timeout and per-host streaming logs
- CLI (`rana`) kept as compatibility mode: `dry-run`, one-shot run, connectivity check

## Quick Start

1. Copy and edit config:

```bash
cp config.example.yaml config.yaml
```

2. Set required environment variables:

```bash
export RANA_ADMIN_USER=admin
export RANA_ADMIN_PASS='change_me'
export RANA_JWT_SECRET='change_me'
export SSH_KEY_PATH=~/.ssh/id_rsa
```

3. Run API:

```bash
go run ./cmd/rana-api -c ./config.yaml -migrate
```

4. (Optional) CLI compatibility run:

```bash
go run ./cmd/rana -c ./config.yaml -dry-run
```

## Docker Compose

```bash
docker compose up -d --build
```

## API (MVP)

- `POST /api/v1/auth/login`
- `POST /api/v1/auth/refresh`
- `GET /api/v1/me`
- `PUT /api/v1/me/locale`
- `GET /api/v1/i18n/locales`
- `GET /api/v1/modules`
- `GET /api/v1/servers`
- `POST /api/v1/executions`
- `GET /api/v1/executions`
- `GET /api/v1/executions/{id}`
- `POST /api/v1/executions/{id}/cancel`
- `GET /api/v1/audit-logs`

## Notes

- Remote servers must have `bash`, `tar`, and `rclone` installed.
- Strict host key check is enabled by default.
- This repository currently ships an in-memory store implementation for fast bootstrap.
