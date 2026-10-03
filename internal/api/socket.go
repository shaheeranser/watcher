package api

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"
)

// dialProbe bounds the check for a live listener on an existing socket path.
const dialProbe = 250 * time.Millisecond

// listenUnix binds a Unix domain socket at path with 0600 permissions. If the
// path exists, it is treated as stale only when nothing answers a dial; a live
// listener is reported as an error so two daemons never share a path silently
// (DASH-5). Access control is the socket's file mode, so no auth token is
// needed (design §2.1).
func listenUnix(path string) (net.Listener, error) {
	if path == "" {
		return nil, errors.New("api: empty socket path")
	}
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create socket directory %s: %w", dir, err)
		}
	}

	if conn, err := net.DialTimeout("unix", path, dialProbe); err == nil {
		conn.Close()
		return nil, fmt.Errorf("another watcher daemon is already listening on %s", path)
	}

	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("remove stale socket %s: %w", path, err)
	}

	l, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("listen on %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		l.Close()
		return nil, fmt.Errorf("set socket permissions on %s: %w", path, err)
	}
	return l, nil
}
