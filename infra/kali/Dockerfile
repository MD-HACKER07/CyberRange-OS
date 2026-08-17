# Kali attacker image for the Red Team range. Built once and referenced by the
# orchestrator as cyberrange/kali-attacker:latest. Tools listed in the spec
# (Nmap, Metasploit, Burp CLI) are installed from Kali's own repositories.
FROM kalilinux/kali-rolling:latest

ENV DEBIAN_FRONTEND=noninteractive
RUN apt-get update && apt-get install -y --no-install-recommends \
      bash ca-certificates iproute2 iputils-ping dnsutils curl wget \
      nmap metasploit-framework sqlmap gobuster nikto hydra netcat-traditional \
      whatweb wpscan seclists python3 python3-pip \
 && rm -rf /var/lib/apt/lists/*

# The orchestrator keeps the container alive and exec's student commands into
# it; no long-running services or exposed ports are needed.
CMD ["sleep", "infinity"]
