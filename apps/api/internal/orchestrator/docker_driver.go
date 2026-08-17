package orchestrator

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog"
)

// DockerDriver provisions ranges as Docker containers on an internal (no
// WAN gateway) network. Each session gets its own network so students are
// isolated from each other as well as from the internet.
type DockerDriver struct {
	api          *dockerAPI
	log          zerolog.Logger
	subnetPrefix string
	seq          int
}

const (
	labelManaged = "cyberrange.managed"
	labelSession = "cyberrange.session"
	labelRole    = "cyberrange.role"
)

func NewDockerDriver(host, version, subnetPrefix string, log zerolog.Logger) (*DockerDriver, error) {
	api, err := newDockerAPI(host, version)
	if err != nil {
		return nil, err
	}
	if subnetPrefix == "" {
		subnetPrefix = "10.66"
	}
	return &DockerDriver{api: api, log: log, subnetPrefix: subnetPrefix}, nil
}

func (d *DockerDriver) Name() string { return "docker" }

func (d *DockerDriver) Ping(ctx context.Context) error { return d.api.ping(ctx) }

func (d *DockerDriver) Provision(ctx context.Context, spec ProvisionSpec) (*Range, error) {
	shortID := shortSession(spec.SessionID)
	netName := "cr-range-" + shortID
	subnet := d.pickSubnet(spec.SessionID)

	// 1) Isolated, internet-egress-denied network (Internal=true => Docker
	//    attaches no default gateway to the host/WAN).
	netID, err := d.api.createNetwork(ctx, networkCreateRequest{
		Name:           netName,
		Driver:         "bridge",
		Internal:       true,
		CheckDuplicate: true,
		Labels:         map[string]string{labelManaged: "true", labelSession: spec.SessionID},
		IPAM:           &ipam{Config: []ipamConfig{{Subnet: subnet}}},
	})
	if err != nil {
		return nil, fmt.Errorf("create isolated network: %w", err)
	}

	rng := &Range{
		NetworkID:   netID,
		NetworkName: netName,
		Subnet:      subnet,
		Driver:      "docker",
	}

	cleanup := func() { _ = d.Teardown(context.WithoutCancel(ctx), rng) }

	// 2) Vulnerable targets (from the registry only).
	for _, t := range spec.Targets {
		if err := d.ensureImage(ctx, t.Image); err != nil {
			cleanup()
			return nil, err
		}
		name := fmt.Sprintf("cr-tgt-%s-%s", shortID, sanitize(t.Hostname))
		req := containerCreateRequest{
			Image:    t.Image,
			Hostname: t.Hostname,
			Labels: map[string]string{
				labelManaged: "true", labelSession: spec.SessionID, labelRole: "target",
			},
			HostConfig: hostConfig{
				NetworkMode: netName,
				Privileged:  t.Privileged,
				NanoCPUs:    int64(orDefault(t.CPUQuota, spec.CPUQuota, 1.0) * 1e9),
				Memory:      orDefaultInt(t.MemoryMB, spec.MemoryMB, 1024) * 1024 * 1024,
				AutoRemove:  false,
			},
		}
		id, err := d.api.createContainer(ctx, name, req)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("create target %s: %w", t.Hostname, err)
		}
		if err := d.api.startContainer(ctx, id); err != nil {
			cleanup()
			return nil, fmt.Errorf("start target %s: %w", t.Hostname, err)
		}
		ip := d.waitForIP(ctx, id, netName)
		rng.Targets = append(rng.Targets, ProvisionedTarget{
			TargetID: t.TargetID, Hostname: t.Hostname, ContainerID: id,
			IPAddress: ip, Image: t.Image, Status: "running",
		})
	}

	// 3) Kali attacker box the student drives via the browser terminal.
	if err := d.ensureImage(ctx, spec.KaliImage); err != nil {
		cleanup()
		return nil, err
	}
	attackerName := "cr-kali-" + shortID
	attackerReq := containerCreateRequest{
		Image:    spec.KaliImage,
		Hostname: "kali-attacker",
		Tty:      true,
		// Keep the box alive so we can exec into it repeatedly.
		Cmd: []string{"sleep", "infinity"},
		Labels: map[string]string{
			labelManaged: "true", labelSession: spec.SessionID, labelRole: "attacker",
		},
		Env: []string{
			"CR_SESSION_ID=" + spec.SessionID,
			"CR_TARGETS=" + targetsEnv(rng.Targets),
		},
		HostConfig: hostConfig{
			NetworkMode: netName,
			NanoCPUs:    int64(orDefault(spec.CPUQuota, 0, 2.0) * 1e9),
			Memory:      orDefaultInt(spec.MemoryMB, 0, 2048) * 1024 * 1024,
			CapAdd:      []string{"NET_RAW", "NET_ADMIN"}, // nmap raw sockets inside the range only
			AutoRemove:  false,
		},
	}
	attackerID, err := d.api.createContainer(ctx, attackerName, attackerReq)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("create attacker: %w", err)
	}
	if err := d.api.startContainer(ctx, attackerID); err != nil {
		cleanup()
		return nil, fmt.Errorf("start attacker: %w", err)
	}
	rng.AttackerID = attackerID
	rng.AttackerName = attackerName
	rng.AttackerIP = d.waitForIP(ctx, attackerID, netName)

	// Populate /etc/hosts in the attacker box so students use friendly names.
	d.writeHosts(ctx, attackerID, rng.Targets)

	d.log.Info().Str("session", spec.SessionID).Str("network", netName).
		Int("targets", len(rng.Targets)).Msg("range provisioned")
	return rng, nil
}

func (d *DockerDriver) Exec(ctx context.Context, attackerID, command string) (*ExecResult, error) {
	if attackerID == "" {
		return nil, fmt.Errorf("no attacker container for session")
	}
	// Run through bash -lc so pipes/redirection in student commands work,
	// entirely inside the isolated container.
	return d.api.exec(ctx, attackerID, []string{"/bin/bash", "-lc", command})
}

func (d *DockerDriver) Teardown(ctx context.Context, rng *Range) error {
	if rng == nil {
		return nil
	}
	var firstErr error
	for _, t := range rng.Targets {
		if t.ContainerID != "" {
			if err := d.api.removeContainer(ctx, t.ContainerID); err != nil && firstErr == nil {
				firstErr = err
			}
		}
	}
	if rng.AttackerID != "" {
		if err := d.api.removeContainer(ctx, rng.AttackerID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if rng.NetworkID != "" {
		// Small delay so container detach completes before network removal.
		time.Sleep(300 * time.Millisecond)
		if err := d.api.removeNetwork(ctx, rng.NetworkID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func (d *DockerDriver) Stats(ctx context.Context, rng *Range) (map[string]ContainerStats, error) {
	out := map[string]ContainerStats{}
	ids := map[string]string{}
	if rng.AttackerID != "" {
		ids["kali-attacker"] = rng.AttackerID
	}
	for _, t := range rng.Targets {
		ids[t.Hostname] = t.ContainerID
	}
	for name, id := range ids {
		if id == "" {
			continue
		}
		s, err := d.api.stats(ctx, id)
		if err != nil {
			continue
		}
		out[name] = *s
	}
	return out, nil
}

// ------------------------------------------------------------------ helpers

func (d *DockerDriver) ensureImage(ctx context.Context, image string) error {
	if d.api.imageExists(ctx, image) {
		return nil
	}
	d.log.Info().Str("image", image).Msg("pulling range image")
	if err := d.api.pullImage(ctx, image); err != nil {
		return fmt.Errorf("image %q unavailable locally and pull failed: %w", image, err)
	}
	return nil
}

func (d *DockerDriver) waitForIP(ctx context.Context, containerID, network string) string {
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		insp, err := d.api.inspect(ctx, containerID)
		if err == nil {
			if n, ok := insp.NetworkSettings.Networks[network]; ok && n.IPAddress != "" {
				return n.IPAddress
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return ""
}

func (d *DockerDriver) writeHosts(ctx context.Context, attackerID string, targets []ProvisionedTarget) {
	for _, t := range targets {
		if t.IPAddress == "" {
			continue
		}
		line := fmt.Sprintf("%s %s", t.IPAddress, t.Hostname)
		_, _ = d.api.exec(ctx, attackerID, []string{"/bin/bash", "-lc", "grep -q '" + t.Hostname + "' /etc/hosts || echo '" + line + "' >> /etc/hosts"})
	}
}

func (d *DockerDriver) pickSubnet(sessionID string) string {
	// Derive a deterministic /24 inside the configured prefix from the
	// session id so concurrent sessions do not collide.
	h := 0
	for _, c := range sessionID {
		h = (h*31 + int(c)) % 250
	}
	return fmt.Sprintf("%s.%d.0/24", d.subnetPrefix, h+2)
}

func shortSession(id string) string {
	id = strings.ReplaceAll(id, "-", "")
	if len(id) > 10 {
		return id[:10]
	}
	return id
}

func sanitize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, c := range s {
		if (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' {
			b.WriteRune(c)
		}
	}
	out := b.String()
	if len(out) > 20 {
		out = out[:20]
	}
	return out
}

func targetsEnv(targets []ProvisionedTarget) string {
	parts := make([]string, 0, len(targets))
	for _, t := range targets {
		parts = append(parts, t.Hostname+"="+t.IPAddress)
	}
	return strings.Join(parts, ",")
}

func orDefault(vals ...float64) float64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}

func orDefaultInt(vals ...int64) int64 {
	for _, v := range vals {
		if v > 0 {
			return v
		}
	}
	return 0
}
