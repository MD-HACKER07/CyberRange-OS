#!/usr/bin/env bash
# =============================================================================
# CyberRange OS — build & start the platform stack
# Run from the repo root on the server, after provision.sh has completed and
# .env.deploy is in place. Builds the API + web images and starts the full
# stack (Postgres/pgvector, Redis, API, web).
#
#   Usage:  bash deploy/deploy.sh
# =============================================================================
set -euo pipefail
cd "$(dirname "$0")/.."

COMPOSE="sudo docker compose -f infra/docker-compose.deploy.yml --env-file .env.deploy"

echo "[1/4] Building the Kali attacker image (Red Team range) ..."
sudo docker image inspect cyberrange/kali-attacker:latest >/dev/null 2>&1 || \
  sudo docker build -t cyberrange/kali-attacker:latest infra/kali

echo "[2/4] Building the API image ..."
$COMPOSE build api
sudo docker builder prune -f >/dev/null 2>&1 || true

echo "[3/4] Building the web image ..."
$COMPOSE build web
sudo docker builder prune -f >/dev/null 2>&1 || true

echo "[4/4] Starting the stack ..."
$COMPOSE up -d
sleep 6
$COMPOSE ps
echo
echo "DEPLOY DONE."
echo "  Web : http://\$(curl -s ifconfig.me):3000"
echo "  API : http://\$(curl -s ifconfig.me):8080/healthz"
