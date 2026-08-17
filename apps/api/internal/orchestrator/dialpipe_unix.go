//go:build !windows

package orchestrator

import (
	"context"
	"fmt"
	"net"
)

// dialNamedPipe is only reachable on Windows; on unix builds a npipe://
// DOCKER_HOST is a misconfiguration.
func dialNamedPipe(_ context.Context, pipe string) (net.Conn, error) {
	return nil, fmt.Errorf("named pipes are Windows-only; use unix:// or tcp:// DOCKER_HOST (got %q)", pipe)
}
