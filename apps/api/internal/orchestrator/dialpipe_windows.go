//go:build windows

package orchestrator

import (
	"context"
	"net"
	"time"
)

// dialNamedPipe connects to the Docker Engine over a Windows named pipe.
// This uses a simple synchronous open; for dev use on Windows it is
// sufficient, and Linux lab hosts use the unix socket path instead.
func dialNamedPipe(ctx context.Context, pipe string) (net.Conn, error) {
	deadline := time.Now().Add(15 * time.Second)
	for {
		conn, err := openPipe(pipe)
		if err == nil {
			return conn, nil
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(200 * time.Millisecond):
		}
	}
}
