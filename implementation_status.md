# Implementation Status

This file reflects current code reality against `implementation_plan.md`.

## P0
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

## P1
- Server group: **Not Done**
- Misfire / schedule semantics hardening: **Not Done in this pass**
- Implementation status document: **Done**
- Env-first deployment entry (`docker-compose.yml` + `.env`, auto-init internal config): **Done**
  - Evidence: `docker-compose.yml`, `.env.example`, `cmd/rana-api/main.go`, `cmd/rana/main.go`, `internal/config/config.go`, `README.md`
  - Notes: default deployment no longer mounts `config.example.yaml`; runtime still keeps an internal YAML file for compatibility and settings persistence.

## Explicitly Not Done
- True WebSocket log streaming
- Full Playwright against real Go backend state
- Server group model/API/UI
- PG/HA/distributed scheduler work
