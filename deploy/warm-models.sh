#!/usr/bin/env bash
# =============================================================================
# CyberRange OS — pre-warm the CyberSec models
# Loads all models into RAM so the first copilot/assistant request is instant
# (no cold-load delay during a live demo). Run after a reboot or service
# restart. Models stay resident because OLLAMA_KEEP_ALIVE=-1 (set in provision).
#
#   Usage:  bash deploy/warm-models.sh
# =============================================================================
set -euo pipefail
BASE=http://localhost:11434

echo "Warming cybersec ..."
curl -s $BASE/api/chat -d '{"model":"cybersec","messages":[{"role":"user","content":"hi"}],"stream":false,"keep_alive":-1}' \
  -o /dev/null -w "  cybersec HTTP=%{http_code} in %{time_total}s\n" --max-time 120

echo "Warming cybersec-soc ..."
curl -s $BASE/api/chat -d '{"model":"cybersec-soc","messages":[{"role":"user","content":"hi"}],"stream":false,"keep_alive":-1}' \
  -o /dev/null -w "  cybersec-soc HTTP=%{http_code} in %{time_total}s\n" --max-time 120 || true

echo "Warming embedding model ..."
curl -s $BASE/api/embeddings -d '{"model":"nomic-embed-text","prompt":"hi","keep_alive":-1}' \
  -o /dev/null -w "  nomic-embed HTTP=%{http_code} in %{time_total}s\n" --max-time 60

echo "Resident models:"
curl -s $BASE/api/ps | python3 -c 'import sys,json;[print("  -",m["name"]) for m in json.load(sys.stdin).get("models",[])]' 2>/dev/null || true
