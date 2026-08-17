//go:build windows

package orchestrator

import (
	"fmt"
	"net"
)

// openPipe is a placeholder for Windows named-pipe dialing. Production
// deployments run on Linux lab hosts using the unix socket. For Windows
// development, set DOCKER_HOST to a TCP endpoint (e.g.
// tcp://localhost:2375 with "Expose daemon on tcp://" enabled in Docker
// Desktop) rather than the default named pipe.
func openPipe(pipe string) (net.Conn, error) {
	return nil, fmt.Errorf("named-pipe Docker host is not supported on this build; set DOCKER_HOST=tcp://localhost:2375 (enable 'Expose daemon on tcp://localhost:2375' in Docker Desktop) — got pipe %q", pipe)
}
