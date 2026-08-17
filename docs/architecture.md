# CyberRange OS — Architecture

CyberRange OS is a self-hosted red team / blue team training platform. Every
exercise is assisted by a **locally-hosted** open-weight LLM; no student data,
lab logs, or exploit attempts ever leave the institution network.

## Component map

```
[Next.js Web App] <-- HTTPS/WSS --> [Go/Fiber API]
                                        |
   +----------------+-----------------+-------------------+------------------+
   | Auth + RBAC    | Range           | LLM Gateway       | Log Ingest       |
   | (JWT + OIDC)   | Orchestrator    | (local-only guard)| (Wazuh+Suricata) |
   +----------------+-----------------+-------------------+------------------+
        |                  |                   |                   |
   [PostgreSQL 16      [Docker Engine     [Ollama / vLLM /    [Wazuh REST +
    + pgvector]         API / Proxmox]     TGI @ LLM_BASE_URL] Suricata EVE]
        |
   [Redis] — sessions, rate limits, pub/sub for live consoles + alert stream
```

The Go API is a modular monolith: one deployable binary containing the auth,
range orchestrator, LLM gateway, log-ingest, MITRE, analytics, reporting, and
AI-security subsystems (each an `internal/` package). This matches the
service-oriented design of the spec while staying operable on modest
departmental hardware. Each subsystem has a clean interface and can be split
into its own process later without changing callers.

## Key packages (`apps/api/internal`)

| Package        | Responsibility |
|----------------|----------------|
| `config`       | Env-driven configuration; every external dependency is a configurable URL |
| `auth`         | JWT access/refresh (rotating), OIDC SSO, local password, RBAC middleware |
| `audit`        | Append-only audit trail (DB trigger blocks UPDATE/DELETE) |
| `llm`          | Local-inference gateway: egress guard, model registry, versioned prompts, streaming, budgets, full call logging |
| `orchestrator` | `RangeProvisioner` interface + `DockerDriver` (implemented) and `ProxmoxDriver` (stub) |
| `ingest`       | Polls Wazuh REST + tails Suricata EVE JSON into the common Alert schema |
| `mitre`        | ATT&CK dataset, pgvector semantic search, LLM auto-tagging |
| `report`       | Markdown→HTML→PDF rendering (headless Chromium / wkhtmltopdf) |
| `aisec`        | PyRIT/Garak runner against the local model (built-in probe fallback) |
| `store`        | Data access for courses, batches, exercises, sessions, reports, analytics |
| `server`       | Fiber wiring + per-module HTTP/WS handlers |

## Realtime

WebSocket endpoints (`/api/range-sessions/:id/terminal`, `/api/siem/alerts/live`)
subscribe to Redis pub/sub channels. Any API instance can serve any socket, so
the platform scales horizontally without sticky sessions. When Redis is briefly
unavailable, events still deliver locally.

## Data model

See `apps/api/internal/db/migrations/0001_init.sql` for the full schema. It
implements every table from spec Section 18 plus supporting tables
(session_targets, copilot_suggestions, playbooks, surveys, xp_events,
platform_settings). `audit_log` is enforced append-only by a trigger.

## Security posture

- **Local inference guard**: `llm.AssertLocalEndpoint` resolves `LLM_BASE_URL`
  and refuses to start if it points at a public IP (unless explicitly
  overridden). The assertion is surfaced in the Admin panel.
- **Structural target safety**: attack targets are chosen from the
  `range_targets` registry only — there is no free-text host field anywhere.
- **Network isolation**: range networks are created with Docker `internal:true`
  (no WAN gateway). See `runbook.md`.
- **Human-in-the-loop**: the copilot only ever *proposes* commands; execution
  requires an explicit "Approve & Run", and every executed command is logged
  with student, target, timestamp, and full output.
- **Reproducibility**: every LLM call records the model and prompt version used.
