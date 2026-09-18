#!/usr/bin/env bash
# =============================================================================
# CyberRange OS — one-shot host provisioner
# Prepares a fresh Linux server (Oracle Linux 9 / Ubuntu, x86_64 or ARM64) to
# run the platform: container engine, local inference runtime, CyberSec models,
# and host firewall. Idempotent — safe to re-run.
#
#   Usage:  sudo bash provision.sh
# =============================================================================
set -euo pipefail

echo "[1/6] Detecting OS + package manager ..."
if command -v dnf >/dev/null 2>&1; then PKG=dnf; FAMILY=rhel
elif command -v apt-get >/dev/null 2>&1; then PKG=apt; FAMILY=debian
else echo "Unsupported OS"; exit 1; fi
echo "      family=$FAMILY  arch=$(uname -m)"

echo "[2/6] Installing base packages ..."
if [ "$FAMILY" = "rhel" ]; then
  sudo dnf install -y dnf-utils tar git curl zstd policycoreutils-python-utils || true
else
  sudo apt-get update -y && sudo apt-get install -y ca-certificates curl git tar zstd
fi

echo "[3/6] Installing the container engine ..."
if ! command -v docker >/dev/null 2>&1; then
  if [ "$FAMILY" = "rhel" ]; then
    sudo dnf config-manager --add-repo https://download.docker.com/linux/centos/docker-ce.repo
    sudo dnf install -y docker-ce docker-ce-cli containerd.io docker-compose-plugin
  else
    curl -fsSL https://get.docker.com | sudo sh
  fi
  sudo systemctl enable --now docker
  sudo usermod -aG docker "$(whoami)" || true
fi
sudo docker --version && sudo docker compose version

echo "[4/6] Installing the local inference runtime ..."
if ! command -v ollama >/dev/null 2>&1; then
  curl -fsSL https://ollama.com/install.sh | sudo sh
fi
# Bind to all interfaces + keep models resident so the first request is instant.
sudo mkdir -p /etc/systemd/system/ollama.service.d
sudo tee /etc/systemd/system/ollama.service.d/override.conf >/dev/null <<'EOF'
[Service]
Environment="OLLAMA_HOST=0.0.0.0:11434"
Environment="OLLAMA_KEEP_ALIVE=-1"
Environment="OLLAMA_MAX_LOADED_MODELS=3"
EOF
sudo systemctl daemon-reload && sudo systemctl enable ollama && sudo systemctl restart ollama
sleep 3

echo "[5/6] Building the CyberSec models (general + SOC) and the embedding model ..."
ollama pull llama3.2:3b
ollama pull nomic-embed-text
# General department model.
printf 'FROM llama3.2:3b\nPARAMETER temperature 0.3\nSYSTEM "You are CyberSec, the department cybersecurity assistant for red team, blue team, reporting and MITRE ATT&CK guidance."\n' > /tmp/cybersec.Modelfile
ollama create cybersec -f /tmp/cybersec.Modelfile
# SOC-specialised build (fine-tuned GGUF must be present at /tmp/cybersec-soc.gguf).
if [ -f /tmp/cybersec-soc.gguf ]; then
  printf 'FROM /tmp/cybersec-soc.gguf\nPARAMETER temperature 0.3\nPARAMETER num_ctx 4096\nSYSTEM "You are CyberSec-SOC, tuned for SOC alert triage and MITRE ATT&CK tagging."\n' > /tmp/cybersec-soc.Modelfile
  ollama create cybersec-soc -f /tmp/cybersec-soc.Modelfile
fi

echo "[6/6] Registering QEMU emulation (multi-arch targets) + opening host firewall ..."
sudo docker run --privileged --rm tonistiigi/binfmt --install all || true
if command -v firewall-cmd >/dev/null 2>&1; then
  sudo firewall-cmd --permanent --add-port=3000/tcp || true
  sudo firewall-cmd --permanent --add-port=8080/tcp || true
  sudo firewall-cmd --reload || true
fi
echo "PROVISION DONE — now run deploy.sh"
