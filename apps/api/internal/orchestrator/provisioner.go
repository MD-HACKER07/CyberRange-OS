// Package orchestrator provisions per-session training ranges. The
// RangeProvisioner interface has two drivers: DockerDriver (dev / small lab)
// and ProxmoxDriver (production, full VM isolation). Both create an isolated,
// internet-egress-denied network, start the requested vulnerable targets plus
// a Kali attacker box, and tear everything down on session end so no state
// leaks between students.
package orchestrator

import (
	"context"
	"errors"
)

var ErrNotImplemented = errors.New("driver not implemented")

// TargetSpec describes one pre-registered vulnerable image to bring up.
// Targets are ALWAYS chosen from the range_targets registry — never a
// free-text host — which is what makes attacking arbitrary external hosts
// structurally impossible.
type TargetSpec struct {
	TargetID     string
	Slug         string
	Hostname     string
	Image        string
	ExposedPorts []int
	CPUQuota     float64
	MemoryMB     int64
	Privileged   bool
}

type ProvisionSpec struct {
	SessionID string
	UserID    string
	KaliImage string
	Targets   []TargetSpec
	CPUQuota  float64
	MemoryMB  int64
}

// ProvisionedTarget is a live target instance the student can attack.
type ProvisionedTarget struct {
	TargetID    string `json:"target_id"`
	Hostname    string `json:"hostname"`
	ContainerID string `json:"container_id"`
	IPAddress   string `json:"ip_address"`
	Image       string `json:"image"`
	Status      string `json:"status"`
}

// Range is the result of provisioning: an isolated network, the attacker box,
// and the live targets.
type Range struct {
	NetworkID     string              `json:"network_id"`
	NetworkName   string              `json:"network_name"`
	AttackerID    string              `json:"attacker_id"`
	AttackerName  string              `json:"attacker_name"`
	AttackerIP    string              `json:"attacker_ip"`
	Subnet        string              `json:"subnet"`
	Targets       []ProvisionedTarget `json:"targets"`
	Driver        string              `json:"driver"`
}

// ExecResult is the captured stdout/stderr and exit code of a command run in
// the attacker container.
type ExecResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

type RangeProvisioner interface {
	Name() string
	// Ping verifies the backend (Docker daemon / Proxmox API) is reachable.
	Ping(ctx context.Context) error
	// Provision builds the isolated range for a session.
	Provision(ctx context.Context, spec ProvisionSpec) (*Range, error)
	// Exec runs a single command inside the session's attacker container and
	// returns its captured output. This is how "Approve & Run" executes.
	Exec(ctx context.Context, attackerID string, command string) (*ExecResult, error)
	// Teardown destroys the attacker box, all targets, and the network.
	Teardown(ctx context.Context, rng *Range) error
	// Stats returns live resource usage for a session's containers.
	Stats(ctx context.Context, rng *Range) (map[string]ContainerStats, error)
}

type ContainerStats struct {
	CPUPercent  float64 `json:"cpu_percent"`
	MemUsedMB   float64 `json:"mem_used_mb"`
	MemLimitMB  float64 `json:"mem_limit_mb"`
	Name        string  `json:"name"`
}
