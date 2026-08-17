# Deploying CyberRange OS on the Ubuntu server

Single-server deployment via Docker Compose. Runs PostgreSQL (pgvector), Redis,
the Go API, and the Next.js web app, and reuses the Ollama already running
natively on the host.

## Prerequisites (already done on this box)
- Docker Engine installed and running.
- Ollama running natively and bound to `0.0.0.0:11434` with `llama3.2:3b` and
  `nomic-embed-text` pulled.

## 1. Clone the repo

```bash
cd ~
git clone https://github.com/MD-HACKER07/CyberRange-OS.git
cd CyberRange-OS
```

## 2. Create the deployment env

```bash
cp .env.deploy.example .env.deploy
nano .env.deploy      # set POSTGRES_PASSWORD, JWT_SECRET, SEED_ADMIN_PASSWORD
```
Generate a strong JWT secret with: `openssl rand -hex 32`.
Leave `LLM_BASE_URL=http://host.docker.internal:11434` to reuse the native Ollama.

## 3. (Optional) Build the Kali attacker image for Red Team ranges

```bash
docker build -t cyberrange/kali-attacker:latest infra/kali
```

## 4. Bring up the platform

```bash
docker compose -f infra/docker-compose.deploy.yml --env-file .env.deploy up -d --build
```

First build takes a few minutes (Go + Next.js). Watch progress:
```bash
docker compose -f infra/docker-compose.deploy.yml logs -f api
```
The API auto-applies migrations and seeds reference data (admin, targets, cert
objectives, MITRE ATT&CK) on first boot.

## 5. Open the firewall for the web app

```bash
sudo ufw allow 3000/tcp
```

## 6. Log in

Browse to `http://<server-ip>:3000` and sign in with the `SEED_ADMIN_EMAIL` /
`SEED_ADMIN_PASSWORD` you set in `.env.deploy`.

## Everyday operations

```bash
# status / logs
docker compose -f infra/docker-compose.deploy.yml ps
docker compose -f infra/docker-compose.deploy.yml logs -f api

# update to latest code
git pull
docker compose -f infra/docker-compose.deploy.yml up -d --build

# stop / start
docker compose -f infra/docker-compose.deploy.yml down
docker compose -f infra/docker-compose.deploy.yml up -d
```

## Notes
- Data persists in the `pgdata`, `redisdata`, and `storage` named volumes.
- The API is internal-only on the compose network; the browser reaches it
  through the web app's `/api` and `/ws` proxy.
- To add the SOC stack (Wazuh + Suricata) and observability (Prometheus +
  Grafana) later, use `infra/docker-compose.yml` which defines them.
- Security: keep the Docker API (2375) and Redis off the public internet; the
  compose services talk over the internal network and don't publish those ports.
