# CyberRange OS

**AI-Augmented Red Team / Blue Team Training Platform** for a college
cybersecurity lab. Self-hosted, with a **locally-hosted** open-weight LLM so no
student data, lab logs, or exploit attempts ever leave the institution network.

Students practice, under supervision, both offensive security (recon,
exploitation, reporting against isolated vulnerable targets) and defensive
security (SOC-style log triage, incident response). Every exercise produces
structured evidence — command logs, rubric-graded reports, MTTD/MTTR metrics —
that maps to Course Outcomes / Programme Outcomes for **NBA** accreditation and
aggregates innovation evidence for **NAAC**.

## What's here

```
/apps
  /web                 Next.js 14 (App Router) + TypeScript + Tailwind ("Vault" theme)
  /api                 Go + Fiber modular monolith (all backend subsystems)
/packages
  /ui                  Vault design-system reference
  /shared-types        Shared TS enums / envelopes
/infra
  docker-compose.yml         platform services
  docker-compose.range.yml   per-session isolated range template
  api.Dockerfile             Go API + seed (with headless Chromium for PDFs)
  kali/Dockerfile            Kali attacker image
/docs
  architecture.md
  runbook.md
```

## Feature coverage (spec phases 1–10)

- **Auth & RBAC** — JWT (rotating refresh), institution OIDC SSO + local
  fallback, four roles, append-only audit trail.
- **Red Team range** — Docker-based per-session provisioning on an
  internet-egress-denied network, browser terminal exec, full command logging,
  LLM pentest copilot (suggest → **Approve & Run** → execute), MITRE auto-tagging.
- **LLM Gateway** — local-inference-only guard (refuses public endpoints),
  config-driven model registry, versioned system prompts, streaming, per-session
  token budgets, full prompt/completion logging.
- **Blue Team SOC** — Wazuh + Suricata ingestion into a common alert schema,
  live alert console, SOC copilot summaries/verdicts, MTTD/MTTR timers,
  playbooks, and CyberSOCEval-style accuracy tracking (ground truth vs AI vs
  student).
- **MITRE ATT&CK engine** — dataset ingest, pgvector semantic search, LLM
  disambiguation, technique tracker.
- **Reporting** — Markdown editor with live preview, AI grading assistant
  (faculty score authoritative), PDF export, portfolio PDF.
- **Accreditation analytics** — CO/PO matrix, weighted direct/indirect
  attainment, heatmap, NBA CSV + NAAC evidence exports, course-exit surveys.
- **AI Security** — PyRIT/Garak (or built-in probe battery) run against the
  institution's own local model; results dashboard.
- **Gamification** — XP weighted by difficulty and inverse copilot reliance,
  batch leaderboards (red/blue/combined), pluggable external CTF feed.
- **Admin & observability** — range target provisioning, LLM registry, RBAC,
  audit viewer with CSV export, system health, Prometheus metrics + Grafana.

## Quick start

```bash
cp .env.example .env          # set JWT_SECRET, SEED_ADMIN_PASSWORD, LLM_BASE_URL
docker build -t cyberrange/kali-attacker:latest infra/kali
docker compose -f infra/docker-compose.yml up -d --build
docker compose -f infra/docker-compose.yml exec ollama ollama pull llama3.1:8b
docker compose -f infra/docker-compose.yml exec ollama ollama pull nomic-embed-text
```

Then open http://localhost:3000. See [docs/runbook.md](docs/runbook.md) for
full operations and [docs/architecture.md](docs/architecture.md) for design.

## Local development

```bash
# Backend (needs a local Postgres w/ pgvector + Redis; or run infra compose):
cd apps/api
go run ./cmd/api            # applies migrations + seeds on boot
go test ./...

# Frontend:
cd apps/web
npm install
npm run dev                 # proxies /api to NEXT_PUBLIC_API_BASE
```

## Safety guarantees (non-negotiable, per spec Section 2)

- Attack targets come from a pre-registered dropdown only — **no free-text host
  field exists anywhere** in the UI or API.
- Range networks are provisioned with no route to the public internet.
- The copilot never auto-executes; a human clicks **Approve & Run**, and every
  approved action is logged with student, target, timestamp, and command.
- All LLM traffic goes to `LLM_BASE_URL`; the gateway refuses to start if that
  resolves to a public IP.
