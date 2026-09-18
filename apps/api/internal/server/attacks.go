package server

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/cyberrange-os/api/internal/httpx"
	"github.com/cyberrange-os/api/internal/realtime"
	"github.com/cyberrange-os/api/internal/store"
)

// AttackDef is one canned, click-to-run offensive action. Every command is a
// real tool already installed in the Kali attacker container (see
// infra/kali/Dockerfile) — nothing here is simulated. Running it produces
// real network traffic against the session's own isolated target, and its
// outcome is logged to the command log exactly like a manual or copilot
// command, then raised as a real SIEM alert so the Blue Team sees it live.
type AttackDef struct {
	ID              string `json:"id"`
	Label           string `json:"label"`
	Description     string `json:"description"`
	Tool            string `json:"tool"`
	MitreTechnique  string `json:"mitre_technique_id"`
	Severity        string `json:"severity"`
	RuleDescription string `json:"rule_description"`
	commandTemplate string // "{target}" is substituted with the target's IP
}

// attackCatalog is intentionally small and safe: every command is
// non-destructive recon/enumeration/probing against pre-vetted vulnerable
// lab images (DVWA, Juice Shop, Metasploitable) on an internet-egress-denied
// network, matching the tooling already named in the pentest-copilot prompt.
var attackCatalog = []AttackDef{
	{
		ID: "port_scan", Label: "Port & Service Scan", Tool: "nmap",
		Description:     "Enumerate open ports and running service versions on the target.",
		MitreTechnique:  "T1046", Severity: "low",
		RuleDescription: "Port/service scan detected against target host",
		commandTemplate: "nmap -sV -T4 {target}",
	},
	{
		ID: "vuln_scan", Label: "Vulnerability Scan", Tool: "nmap",
		Description:     "Run nmap's vuln script category to flag known CVEs on exposed services.",
		MitreTechnique:  "T1595.002", Severity: "medium",
		RuleDescription: "Vulnerability scan (nmap --script vuln) detected against target host",
		commandTemplate: "nmap -sV --script vuln -T4 {target}",
	},
	{
		ID: "web_recon", Label: "Web Recon", Tool: "whatweb",
		Description:     "Fingerprint the web application stack (CMS, server, frameworks).",
		MitreTechnique:  "T1592", Severity: "low",
		RuleDescription: "Web technology fingerprinting detected against target host",
		commandTemplate: "whatweb -a 3 http://{target}/",
	},
	{
		ID: "dir_brute", Label: "Directory Brute Force", Tool: "gobuster",
		Description:     "Brute-force hidden directories/files on the web root.",
		MitreTechnique:  "T1595", Severity: "medium",
		RuleDescription: "Directory/file brute-force enumeration detected against target host",
		commandTemplate: "gobuster dir -u http://{target}/ -w /usr/share/seclists/Discovery/Web-Content/common.txt -q -t 20",
	},
	{
		ID: "sqli_probe", Label: "SQL Injection Probe", Tool: "sqlmap",
		Description:     "Probe a known injectable parameter for SQL injection and grab the DB banner.",
		MitreTechnique:  "T1190", Severity: "high",
		RuleDescription: "SQL injection attempt detected against target host",
		commandTemplate: `sqlmap -u "http://{target}/vulnerabilities/sqli/?id=1&Submit=Submit#" --batch --banner --level=1`,
	},
	{
		ID: "login_bruteforce", Label: "Login Brute Force", Tool: "hydra",
		Description:     "Attempt credential brute-forcing against an exposed login form.",
		MitreTechnique:  "T1110.001", Severity: "high",
		RuleDescription: "Repeated authentication failures (credential brute-force) detected against target host",
		commandTemplate: "hydra -l admin -P /usr/share/seclists/Passwords/Common-Credentials/10-common-passwords.txt {target} http-get / -t 4 -f",
	},
}

func attackByID(id string) (AttackDef, bool) {
	for _, a := range attackCatalog {
		if a.ID == id {
			return a, true
		}
	}
	return AttackDef{}, false
}

// listAttacks returns the canned attack catalog for the Red Team UI.
func (h *rangeHandler) listAttacks(c *fiber.Ctx) error {
	// Load-and-discard validates the session belongs to the caller so a
	// student can't probe the catalog for a session that isn't theirs; the
	// catalog itself is static and doesn't depend on the session otherwise.
	if _, err := h.loadOwnedSession(c); err != nil {
		return err
	}
	return httpx.OK(c, fiber.Map{"items": attackCatalog})
}

var nmapHostRe = regexp.MustCompile(`Nmap scan report for (?:\S+ \()?([0-9]{1,3}(?:\.[0-9]{1,3}){3})\)?`)

// scan runs a real ping-sweep of the session's isolated subnet from the
// attacker container ("who is on the network"), logs it exactly like any
// other command, and raises a low-severity network-scan alert so the Blue
// Team sees reconnaissance activity before the exploitation attempt lands.
func (h *rangeHandler) scan(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	if sess.Subnet == "" {
		return httpx.BadRequest("session has no isolated subnet on record")
	}

	entry, err := h.execCore(c, sess, fmt.Sprintf("nmap -sn %s", sess.Subnet), false, "", "T1595.001", false)
	if err != nil {
		return err
	}

	discovered := []fiber.Map{}
	seen := map[string]bool{}
	for _, ip := range nmapHostRe.FindAllStringSubmatch(entry.Output, -1) {
		host := ip[1]
		if host == "" || host == sess.AttackerIP || seen[host] {
			continue
		}
		seen[host] = true
		hostname := ""
		for _, t := range sess.Targets {
			if t.IPAddress == host {
				hostname = t.Hostname
				break
			}
		}
		discovered = append(discovered, fiber.Map{"ip_address": host, "hostname": hostname})
	}

	h.raiseAttackAlert(c, sess, entry, AttackDef{
		MitreTechnique: "T1595.001", Severity: "low",
		RuleDescription: "Network scan detected from attacker host",
	}, sess.Subnet)

	return httpx.OK(c, fiber.Map{"command_log": entry, "discovered_hosts": discovered})
}

// attack runs one canned offensive action against a target IP that must
// belong to this session (a registered target or a host the student already
// discovered via /scan on the session's own isolated subnet — never an
// arbitrary external host). It logs the real command/output like any other
// exec, then raises a real, correlated SIEM alert (real attacker IP -> real
// target IP, correct MITRE technique and severity for the chosen tool) so
// the Blue Team's live feed reflects exactly what the Red Team just did.
func (h *rangeHandler) attack(c *fiber.Ctx) error {
	sess, err := h.loadOwnedSession(c)
	if err != nil {
		return err
	}
	body, err := httpx.Bind[struct {
		AttackID string `json:"attack_id"`
		TargetIP string `json:"target_ip"`
	}](c)
	if err != nil {
		return err
	}
	def, ok := attackByID(body.AttackID)
	if !ok {
		return httpx.BadRequest("unknown attack_id")
	}
	targetIP := strings.TrimSpace(body.TargetIP)
	if targetIP == "" {
		return httpx.BadRequest("target_ip is required")
	}
	if !h.targetInSessionSubnet(sess, targetIP) {
		return httpx.Forbidden("target_ip is not part of this session's isolated range")
	}

	command := strings.ReplaceAll(def.commandTemplate, "{target}", targetIP)
	entry, err := h.execCore(c, sess, command, false, "", def.MitreTechnique, false)
	if err != nil {
		return err
	}

	alert := h.raiseAttackAlert(c, sess, entry, def, targetIP)
	return httpx.OK(c, fiber.Map{"command_log": entry, "alert": alert})
}

// targetInSessionSubnet enforces that a click-to-attack can only ever reach
// a host inside the student's own already-isolated per-session network —
// either a registered target container or a peer the student's own scan
// discovered on that same subnet. This mirrors the registry-only guarantee
// resolveTargets already gives free-text hosts at session start.
func (h *rangeHandler) targetInSessionSubnet(sess *store.RangeSession, ip string) bool {
	for _, t := range sess.Targets {
		if t.IPAddress == ip {
			return true
		}
	}
	if sess.Subnet == "" {
		return false
	}
	prefix := sess.Subnet
	if idx := strings.LastIndex(prefix, "."); idx > 0 {
		prefix = prefix[:idx+1] // "10.66.42." from "10.66.42.0/24"
	}
	return strings.HasPrefix(ip, prefix)
}

func (h *rangeHandler) raiseAttackAlert(c *fiber.Ctx, sess *store.RangeSession, entry *store.CommandLogEntry, def AttackDef, targetIP string) *store.Alert {
	raw, _ := json.Marshal(fiber.Map{
		"command": entry.Command, "tool": def.Tool, "exit_code": entry.ExitCode,
		"output_excerpt": truncate(entry.Output, 2000),
	})
	technique := def.MitreTechnique
	a := store.Alert{
		SessionID: &sess.ID, ExerciseID: &sess.ExerciseID,
		Source:          "platform-audit",
		ExternalID:      strPtr(entry.ID.String()),
		RuleID:          def.Tool,
		RuleDescription: def.RuleDescription,
		Severity:        def.Severity,
		SrcIP:           sess.AttackerIP,
		DstIP:           targetIP,
		RawLog:          raw,
		EventAt:         time.Now().UTC(),
	}
	if technique != "" {
		a.MitreTechniqueID = &technique
	}
	// Same insert-then-publish sequence ingest.Service uses for real
	// Suricata/Wazuh alerts, so the Blue Team live feed (and its
	// session-scoped channel) reacts identically regardless of source.
	saved, isNew, err := h.d.store.InsertAlert(c.Context(), a)
	if err != nil || saved == nil || !isNew {
		return saved
	}
	h.d.Hub.Publish(c.Context(), realtime.ChannelAlerts("all"), "alert.new", saved)
	h.d.Hub.Publish(c.Context(), realtime.ChannelAlerts(sess.ID.String()), "alert.new", saved)
	return saved
}

func strPtr(s string) *string { return &s }

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
