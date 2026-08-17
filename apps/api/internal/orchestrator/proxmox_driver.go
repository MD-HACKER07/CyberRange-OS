package orchestrator

import (
	"context"
	"fmt"

	"github.com/rs/zerolog"

	"github.com/cyberrange-os/api/internal/config"
)

// ProxmoxDriver is the production driver for full VM-level isolation. It is
// stubbed behind the RangeProvisioner interface per the spec (DockerDriver
// first, Proxmox for later completion). The connection settings are wired so
// the concrete API calls can be filled in without touching callers.
type ProxmoxDriver struct {
	url       string
	tokenID   string
	tokenKey  string
	node      string
	subnet    string
	log       zerolog.Logger
}

func NewProxmoxDriver(cfg *config.Config, log zerolog.Logger) *ProxmoxDriver {
	return &ProxmoxDriver{
		url:      cfg.ProxmoxURL,
		tokenID:  cfg.ProxmoxTokenID,
		tokenKey: cfg.ProxmoxTokenSecret,
		node:     cfg.ProxmoxNode,
		subnet:   cfg.RangeSubnetPrefix,
		log:      log,
	}
}

func (p *ProxmoxDriver) Name() string { return "proxmox" }

func (p *ProxmoxDriver) Ping(ctx context.Context) error {
	if p.url == "" || p.tokenID == "" {
		return fmt.Errorf("proxmox driver not configured (set PROXMOX_URL and PROXMOX_TOKEN_ID)")
	}
	// Concrete /api2/json/version call is added when the VM-clone flow is
	// implemented; interface contract is intentionally the same as Docker.
	return fmt.Errorf("proxmox driver: %w", ErrNotImplemented)
}

func (p *ProxmoxDriver) Provision(ctx context.Context, spec ProvisionSpec) (*Range, error) {
	return nil, fmt.Errorf("proxmox provisioning: %w", ErrNotImplemented)
}

func (p *ProxmoxDriver) Exec(ctx context.Context, attackerID, command string) (*ExecResult, error) {
	return nil, fmt.Errorf("proxmox exec: %w", ErrNotImplemented)
}

func (p *ProxmoxDriver) Teardown(ctx context.Context, rng *Range) error {
	return fmt.Errorf("proxmox teardown: %w", ErrNotImplemented)
}

func (p *ProxmoxDriver) Stats(ctx context.Context, rng *Range) (map[string]ContainerStats, error) {
	return nil, fmt.Errorf("proxmox stats: %w", ErrNotImplemented)
}
