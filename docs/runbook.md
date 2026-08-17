# CyberRange OS — Operations Runbook

## Prerequisites

- Docker Engine 24+ and Docker Compose v2 on the platform host (Linux).
- A GPU host running Ollama or vLLM for the local model (an RTX 4090 / A6000
  class GPU comfortably serves a 7B–14B quantized model for ~60 concurrent
  students with request queuing). CPU-only works for development.
- The vulnerable target images and the Kali attacker image reachable by the
  Docker daemon (built or pulled ahead of a lab session).

## First-time bring-up

```bash
cp .env.example .env
# Edit .env: set a strong JWT_SECRET, SEED_ADMIN_PASSWORD, and LLM_BASE_URL.

# Build the Kali attacker image referenced by the orchestrator:
docker build -t cyberrange/kali-attacker:latest infra/kali

# Start the platform:
docker compose -f infra/docker-compose.yml up -d --build

# Pull a model into the local runtime (Ollama example):
docker compose -f infra/docker-compose.yml exec ollama ollama pull llama3.1:8b
docker compose -f infra/docker-compose.yml exec ollama ollama pull nomic-embed-text
```

The API applies migrations and seeds reference data (MITRE ATT&CK, cert
objectives, default range targets, and the Lab Admin) automatically on first
boot. To (re-)seed manually, including full ATT&CK coverage:

```bash
# Optional: point at the official STIX bundle for the complete technique set
export MITRE_STIX_PATH=/infra/seed/enterprise-attack.json
docker compose -f infra/docker-compose.yml exec api /usr/local/bin/seed
```

Log in at http://localhost:3000 with the seeded admin account.

## Network isolation (spec Section 20)

Three logical networks:

1. **platform** — web, api, db, redis, wazuh (institution LAN).
2. **llmnet** — ollama/vLLM, reachable only from the API.
3. **per-session range** — created by the orchestrator with `internal: true`,
   which means Docker attaches **no gateway to the WAN**. Targets are reachable
   only from the session's Kali box, which students reach only through the
   browser-terminal proxy — never direct SSH.

Verify a running range network is egress-denied:

```bash
docker network inspect cr-range-<id> --format '{{.Internal}}'   # -> true
```

For production VM-level isolation, set `RANGE_DRIVER=proxmox` and configure the
`PROXMOX_*` variables (the ProxmoxDriver implements the same interface).

## Verifying the local-inference guarantee

Admin panel → Health → "Verify Now", or:

```bash
curl -s localhost:8080/readyz | jq .llm_egress
```

`all_private: true` confirms the endpoint resolves only to private addresses.
The API refuses to start if `LLM_BASE_URL` is public (unless
`LLM_ALLOW_PUBLIC_IP=true` is explicitly set).

## Blue Team telemetry

The log-ingest service polls the Wazuh manager REST API and tails the Suricata
EVE JSON file (`SURICATA_EVE_FILE`). Point Suricata at the range bridge
interface so Red Team activity generates real IDS events. Alerts normalize into
the common schema, auto-tag to MITRE, persist, and stream to the Blue Team
console live.

## Backups & retention

- `pgdata` volume holds all evidence (command logs, alerts, reports, audit).
  Back it up on the institution's normal schedule.
- Data retention is configurable via Admin settings; the audit log is
  append-only and should be shipped to the Wazuh/ELK stack for dogfooded
  monitoring.

## Common operations

| Task | Command |
|------|---------|
| Tail API logs | `docker compose -f infra/docker-compose.yml logs -f api` |
| Reap stuck ranges | Automatic every 30s; or end via the console |
| Rotate a model | Admin → LLM Registry → register new local model, mark default |
| Edit a copilot prompt | Admin → prompts create a new version (old versions retained) |
| Export NBA attainment | Faculty dashboard → "Export NBA CSV" |
| Run an AI-security scan | AI Security → Run Scan (targets the local model only) |

## Scaling notes

- p95 API latency target < 300ms excluding LLM generation and provisioning.
- The LLM gateway enforces per-user rate limits and per-session token budgets
  to protect the GPU under a full lab batch.
- Run multiple `api` replicas behind a load balancer; Redis pub/sub keeps
  WebSocket fan-out correct across replicas.
```
