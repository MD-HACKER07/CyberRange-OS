# CyberRange OS — Technical Reference

Companion to `PRESENTATION.md`. Covers the internals a technical audience will
ask about: request flows, the AI model split, the range sandbox, the live
alert pipeline, and the exact commands to operate the system.

---

## 1. Component responsibilities

| Service | Responsibility |
|---|---|
| **web** (Next.js) | UI + browser client. Proxies `/api` to the API over the compose network; opens WebSockets straight to the API's public port for live streams. |
| **api** (Go/Fiber) | Auth (JWT + refresh, RBAC), range orchestration, LLM gateway, SIEM/alerts, reports, analytics, audit. Single binary. |
| **postgres** (pgvector) | System of record + vector store for semantic MITRE ATT&CK search. |
| **redis** | Live pub/sub fan-out (terminal + alert streams), per-session LLM token budget, per-user rate limiting. |
| **CyberSec model** (on-box) | `cybersec` (general copilots + assistant), `cybersec-soc` (SOC triage), embedding model. |
| **orchestrator** (Docker driver) | Builds/destroys per-session isolated ranges (Kali attacker + vetted targets). |

---

## 2. The AI model split (why two models)

The department model is served in two builds, both fully on-box:

| Model | Bound modules | Why |
|---|---|---|
| `cybersec` (general) | pentest-copilot, red-teaming-target, mitre-tagging, report-grading, assistant | Follows arbitrary structured-JSON schemas reliably. |
| `cybersec-soc` (SOC-tuned) | soc-copilot only | Specialised on SOC alert-triage JSON; kept off the other modules because a narrow fine-tune can return empty/unparseable output for schemas outside its training data. |

The API's **LLM gateway** resolves, per request, which model serves a given
module from the `llm_models` registry table, and reconciles the bindings on
boot from `LLM_DEFAULT_MODEL` / `LLM_SOC_MODEL`. The Admin → LLM Registry page
shows this mapping; the egress guard asserts the endpoint stays private.

**Reliability:** `OLLAMA_KEEP_ALIVE=-1` pins models in RAM so the first request
after boot is instant (no cold-load timeout). `deploy/warm-models.sh` pre-loads
them on demand.

---

## 3. Red Team — click-to-attack flow

```
UI: Scan Network ──▶ POST /api/range-sessions/:id/scan
      └─ runs `nmap -sn <session-subnet>` in the Kali box → parses live hosts

UI: Select target + attack ──▶ POST /api/range-sessions/:id/attack
      └─ validates target_ip ∈ this session's isolated subnet (never external)
      └─ runs the real tool (nmap / sqlmap / gobuster / hydra / whatweb)
      └─ logs the command (evidence) + auto-tags MITRE ATT&CK
      └─ raises a correlated SIEM alert (attacker_ip → target_ip)
```

Attack catalog (each = a real tool, non-destructive, in-sandbox):

| Attack | Tool | MITRE | Severity |
|---|---|---|---|
| Port & Service Scan | nmap | T1046 | low |
| Vulnerability Scan | nmap --script vuln | T1595.002 | medium |
| Web Recon | whatweb | T1592 | low |
| Directory Brute Force | gobuster | T1595 | medium |
| SQL Injection Probe | sqlmap | T1190 | high |
| Login Brute Force | hydra | T1110.001 | high |

**Sandbox guarantees:** per-session Docker network with `Internal: true` (no
WAN gateway); targets resolved only from the `range_targets` registry; the Kali
box can reach targets but nothing outside.

---

## 4. Blue Team — live alert pipeline

Both real IDS telemetry and click-to-attack events flow through the same path:

```
attack / IDS event
   └─ store.InsertAlert (idempotent on source+external_id)
        └─ Redis publish → channel "alerts:all" (+ per-session channel)
             └─ API WebSocket /ws/siem/alerts/live
                  └─ Blue Team console re-renders the feed instantly
```

Because the click-to-attack handler publishes to the **same channel** the IDS
uses, the Blue Team console needs no special casing — a synthesized attack and a
real Suricata alert look identical to the SOC analyst. MTTD/MTTR are computed
from `detected_at` / `resolved_at` timestamps.

---

## 5. Networking notes (important for deploys)

- **WebSockets bypass the web proxy.** Next.js `rewrites()` only forwards plain
  HTTP, not the WS upgrade handshake. Browser WS clients therefore connect
  directly to the API's public port (`PUBLIC_WS_BASE=ws://<IP>:8080`). Port 8080
  must be open at the cloud firewall.
- **Two firewalls.** The host firewall is opened by `provision.sh`; the cloud
  provider's network firewall (AWS Security Group / Oracle VCN Security List or
  VNIC NSG) must independently allow inbound TCP 3000 + 8080.
- **Multi-arch.** On ARM64 hosts, `provision.sh` registers QEMU binfmt so
  amd64-only target images (e.g. DVWA) still run. Native-arm64 targets (Juice
  Shop, WebGoat) provision faster — prefer them for live demos.

---

## 6. Operator command cheat-sheet

```bash
# ---- status ----
sudo docker compose -f infra/docker-compose.deploy.yml ps
sudo docker logs cyberrange-os-api-1 --tail 50
curl -s localhost:8080/healthz

# ---- restart just the API (after an env change) ----
sudo docker compose -f infra/docker-compose.deploy.yml --env-file .env.deploy up -d api

# ---- rebuild everything ----
bash deploy/deploy.sh

# ---- re-warm models after a reboot ----
bash deploy/warm-models.sh

# ---- inspect the model registry ----
sudo docker exec cyberrange-os-postgres-1 \
  psql -U cyberrange -d cyberrange -c "SELECT name,is_default,modules FROM llm_models ORDER BY name;"

# ---- list loaded (resident) models ----
curl -s localhost:11434/api/ps

# ---- disk pressure (small cloud volumes) ----
df -h / ; sudo docker system df ; sudo docker builder prune -af
```

---

## 7. Data model highlights

| Table | Notes |
|---|---|
| `users`, `batches`, `enrollments` | RBAC: student / faculty / admin / auditor |
| `range_sessions`, `session_targets`, `session_command_log` | isolated session state + full command evidence (`attacker_ip`, `subnet`) |
| `copilot_suggestions` | every AI suggestion stored, even if never approved (human-in-the-loop proof) |
| `siem_alerts` | src/dst IP, severity, MITRE tag, detect/resolve timestamps, verdicts |
| `llm_models`, `llm_prompts`, `llm_calls` | model registry, versioned prompts, full call log (accreditation) |
| `mitre_techniques` | ATT&CK set + pgvector embeddings for semantic tagging |
| `audit_log` | append-only trail of every sensitive action |

---

## 8. Security posture (summary)

- On-premise inference only; egress guard blocks a public model endpoint at boot.
- Per-session network isolation; targets restricted to a vetted registry.
- JWT access tokens (short-lived) + rotating refresh tokens; RBAC on every route.
- Append-only audit log; every command and AI suggestion recorded.
- Per-session LLM token budget + per-user rate limiting (Redis).
