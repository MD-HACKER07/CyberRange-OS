# CyberRange OS — Presentation & Startup Guide

An AI-assisted cybersecurity training platform for the department. Students run
**Red Team** attacks and **Blue Team** SOC triage inside isolated sandboxes,
guided by our own on-premise **CyberSec** language model. Nothing leaves the lab.

---

## 1. The one-line pitch

> A self-hosted "cyber range" where students launch real attacks against
> intentionally-vulnerable targets in an isolated sandbox, and defend against
> them from a live SOC console — with an AI copilot (our own CyberSec model,
> running fully on-premise) assisting on both sides and grading their work.

---

## 2. What it does

| Capability | What the student sees |
|---|---|
| **Red Team range** | Click **Scan Network → pick a target → launch an attack**. Real tools (nmap, sqlmap, gobuster, hydra, whatweb) run inside an isolated Kali box against a real vulnerable target. |
| **Blue Team SOC** | A **live alert feed** lights up the instant the Red Team acts. Triage each alert (true/false positive), get an AI summary, record the response. MTTD/MTTR are tracked. |
| **AI Pentest Copilot** | Suggests the next attack step with a MITRE ATT&CK tag; the student approves before it runs. |
| **AI SOC Copilot** | Summarises an alert, proposes a verdict + confidence, drafts the incident note. |
| **Department Assistant** | Voice/text Q&A about routines, schedules, and the platform — answered by our CyberSec model. |
| **Reports & grading** | Students write pentest/incident reports; AI drafts a rubric score, faculty finalise; portfolio PDF export. |
| **Admin panel** | LLM model registry, range targets, users/RBAC, append-only audit log, egress assertion (proves inference stays on-box). |

Everything is mapped to **MITRE ATT&CK** and **course outcomes (CO/PO)** for
accreditation evidence.

---

## 3. Architecture (high level)

```
                       Browser (student / faculty / admin)
                                    │  HTTPS-ready
                                    ▼
                         ┌──────────────────────┐
                         │   Web app (Next.js)   │  :3000
                         └──────────┬───────────┘
                        REST /api   │   WebSocket /ws  (live terminal + alerts)
                                    ▼
                         ┌──────────────────────┐
                         │     API (Go/Fiber)    │  :8080
                         │  auth · RBAC · audit  │
                         └───┬─────────┬─────────┘
             ┌───────────────┘         └───────────────┐
             ▼                                          ▼
   ┌───────────────────┐                    ┌────────────────────────┐
   │ PostgreSQL+pgvector│                    │  CyberSec model (local) │
   │ Redis (streams)    │                    │  general + SOC + embed  │
   └───────────────────┘                    └────────────────────────┘
             │
             ▼  (orchestrator — Docker driver)
   ┌────────────────────────────────────────────────────┐
   │  Per-session ISOLATED network (no internet route)   │
   │   ┌──────────┐        attacks        ┌───────────┐  │
   │   │  Kali    │ ────────────────────▶ │  Target   │  │
   │   │ attacker │  nmap/sqlmap/hydra …   │ DVWA/Juice│  │
   │   └──────────┘                        └───────────┘  │
   │        │ real traffic → IDS/telemetry → SIEM alerts  │
   └────────┴───────────────────────────────────────────┘
                                    │
                                    ▼  live alert feed → Blue Team console
```

**Key guarantees**
- **Isolation:** every range session gets its own internal Docker network with
  no route to the internet; targets come only from a vetted registry, so
  students can never attack an arbitrary external host.
- **On-premise AI:** the CyberSec model runs on the box itself; an egress guard
  refuses to start if the inference endpoint resolves to a public address.
- **Evidence:** every command, suggestion, and decision is logged to an
  append-only audit trail and tagged to MITRE ATT&CK.

---

## 4. Technology stack

| Layer | Technology |
|---|---|
| Web | Next.js 14 (App Router), TypeScript, Tailwind |
| API | Go 1.25, Fiber, JWT auth, RBAC, WebSockets |
| Data | PostgreSQL 16 + pgvector (semantic MITRE search), Redis (live streams, rate/budget) |
| AI | On-box CyberSec model (general + SOC-tuned) + embedding model |
| Range | Docker driver — isolated per-session networks, Kali attacker + vulnerable targets |
| Infra | Docker Compose, multi-arch (x86_64 + ARM64 via QEMU) |

---

## 5. How to start it (demo-day checklist)

### A. If the server is already provisioned (normal case)
```bash
# SSH in
ssh -i ssh-key.key opc@<SERVER_IP>

cd ~/CyberRange-OS
bash deploy/deploy.sh       # builds + starts the whole stack
bash deploy/warm-models.sh  # pre-load models so the first click is instant
```
Then open **http://<SERVER_IP>:3000**.

### B. From a brand-new server
```bash
# 1. copy the repo, the fine-tuned model, and the env file up
scp -i key.key -r CyberRange-OS opc@<IP>:~/
scp -i key.key cybersec-soc.gguf opc@<IP>:/tmp/
scp -i key.key deploy/.env.deploy opc@<IP>:~/CyberRange-OS/.env.deploy

# 2. on the server
cd ~/CyberRange-OS
sudo bash deploy/provision.sh   # engine + local AI runtime + models + firewall
bash deploy/deploy.sh           # build + start
bash deploy/warm-models.sh      # pre-warm
```

### C. Open the cloud firewall
The host firewall is handled by `provision.sh`, but the **cloud provider's**
network firewall must also allow inbound **TCP 3000 and 8080** from `0.0.0.0/0`
(AWS: Security Group · Oracle: VCN Security List **or** the VNIC's Network
Security Group). This is the single most common "site won't load" cause.

---

## 6. Live demo script (≈6 minutes)

1. **Log in** as the student — show the dashboard.
2. **Red Team** → Launch the range exercise → wait for "running".
3. Click **Scan Network** → discovered hosts appear (real nmap sweep).
4. **Select the target** → click **Port & Service Scan**, then **SQL Injection Probe**.
   - Point out: real tool output streams into the terminal; each action gets a
     MITRE ATT&CK tag.
5. Open the **Pentest Copilot** → "Suggest next action" → show the AI proposes a
   command + rationale; approve & run.
6. Switch to **Blue Team** (second browser / student 2) → the alerts from those
   attacks are **already in the live feed**, correlated attacker→target IP.
7. Select an alert → **AI Summarize** → show verdict + confidence + incident
   draft → mark resolved.
8. Open the **Department Assistant** → ask "what are the lab timings?" → instant
   spoken/typed answer from the CyberSec model.
9. (Optional) **Admin → LLM Registry** → show the CyberSec models registered,
   all inference on-box, egress assertion green.

---

## 7. Default credentials (demo)

| Role | Email | Password |
|---|---|---|
| Admin | `admin@cyberrange.local` | (see `LOGIN_CREDENTIALS.txt`) |
| Student | `student@cyberrange.local` | `Student@12345` |
| Faculty | `faculty@cyberrange.local` | `Faculty@12345` |

> Change these before any non-demo use.

---

## 8. Talking points / Q&A

- **"Is it safe? Are they attacking real systems?"** No — each session is a
  sealed Docker network with no internet route, and targets are only the vetted
  vulnerable images we ship. Attacking an outside host is structurally
  impossible.
- **"Where does the AI run? Do you send data to OpenAI?"** No. The CyberSec
  model runs on our own server. An egress guard refuses to boot if the model
  endpoint isn't a private address.
- **"How is it graded / accreditation?"** Every action is logged and mapped to
  MITRE ATT&CK and course outcomes; reports get an AI-assisted rubric score that
  faculty finalise; portfolio PDFs export the evidence.
- **"Can it scale to a class?"** Yes — sessions are per-student and isolated;
  Redis fan-out means the live consoles scale horizontally.

---

## 9. Files & scripts reference

| File | Purpose |
|---|---|
| `deploy/provision.sh` | One-shot host setup (engine, local AI runtime, models, firewall) |
| `deploy/deploy.sh` | Build + start the platform stack |
| `deploy/warm-models.sh` | Pre-load models so the first request is instant |
| `deploy/.env.deploy` | Deployment configuration (secrets, public URL, model names) |
| `infra/docker-compose.deploy.yml` | The full service definition |
| `infra/kali/Dockerfile` | The Kali attacker image (Red Team) |
| `docs/architecture.md` | Deeper architecture notes |
| `docs/runbook.md` | Operational runbook |
