// Package guard refuses to attach Watcher to a source that is its own output.
// It is load-bearing: without it, `watcher ... > app.log` followed by
// `watcher --file app.log` would feed explanations back into the detector
// forever (CORE-GRD-1).
package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/shaheeranser/watcher/internal/source"
)

// IsSelf reports whether src is Watcher's own output, and why. A caller must
// treat a positive result as fatal (CORE-GRD-2).
func IsSelf(src source.Source) (bool, string) {
	if cs, ok := src.(interface{ ContainerID() string }); ok {
		if self, reason := IsSelfContainer(cs.ContainerID()); self {
			return true, reason
		}
	}

	pathSource, ok := src.(interface{ Path() string })
	if !ok || pathSource.Path() == "" {
		return false, ""
	}
	path := pathSource.Path()

	if same, err := sameFileAsStdio(path); err == nil && same {
		return true, "source file is the same file as Watcher's stdout or stderr"
	}

	if pids, err := writerPIDs(path); err == nil && len(pids) > 0 && onlySelf(pids) {
		return true, fmt.Sprintf("the only writer of %s is Watcher itself", path)
	}

	return false, ""
}

// sameFileAsStdio compares the file against our stdout and stderr, both through
// the os.File handles and, on Linux, through /proc/self/fd so a redirect is
// caught even when the handle's metadata is stale.
func sameFileAsStdio(path string) (bool, error) {
	info, err := os.Stat(path)
	if err != nil {
		return false, err
	}
	for _, fd := range []*os.File{os.Stdout, os.Stderr} {
		if other, err := fd.Stat(); err == nil && os.SameFile(info, other) {
			return true, nil
		}
	}
	for _, fdPath := range []string{"/proc/self/fd/1", "/proc/self/fd/2"} {
		if other, err := os.Stat(fdPath); err == nil && os.SameFile(info, other) {
			return true, nil
		}
	}
	return false, nil
}

// writerPIDs lists the processes holding path open. Reading another process's
// file descriptors can be denied, so failures are skipped rather than fatal;
// the check is best-effort and the file-identity check is authoritative where
// it applies.
func writerPIDs(path string) ([]int, error) {
	target, err := filepath.EvalSymlinks(path)
	if err != nil {
		target = path
	}

	procs, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}

	var pids []int
	for _, proc := range procs {
		pid, err := strconv.Atoi(proc.Name())
		if err != nil {
			continue
		}
		if opensPath(pid, target, path) {
			pids = append(pids, pid)
		}
	}
	return pids, nil
}

func opensPath(pid int, targets ...string) bool {
	fdDir := filepath.Join("/proc", strconv.Itoa(pid), "fd")
	fds, err := os.ReadDir(fdDir)
	if err != nil {
		return false
	}
	for _, fd := range fds {
		link, err := os.Readlink(filepath.Join(fdDir, fd.Name()))
		if err != nil {
			continue
		}
		link = strings.TrimSuffix(link, " (deleted)")
		for _, target := range targets {
			if link == target {
				return true
			}
		}
	}
	return false
}

func onlySelf(pids []int) bool {
	me := os.Getpid()
	for _, pid := range pids {
		if pid != me {
			return false
		}
	}
	return true
}

// containerIDPattern matches a full Docker container id as it appears in a
// cgroup path.
var containerIDPattern = regexp.MustCompile(`\b[0-9a-f]{64}\b`)

// SelfContainerID resolves Watcher's own container id when it runs inside a
// container: from the cgroup path first, then from the hostname (Docker sets it
// to the short id). It reports false outside a container, which is what keeps
// a bare-metal run from treating itself as a container.
func SelfContainerID() (string, bool) {
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		if id := containerIDPattern.FindString(string(data)); id != "" {
			return id, true
		}
	}
	if host, err := os.Hostname(); err == nil && looksLikeContainerID(host) {
		return host, true
	}
	return "", false
}

// IsSelfContainer reports whether id names Watcher's own container. It is the
// container-identity arm of the self-watch guard (RT-DOCK-7): a container-log
// source calls it to refuse to attach to Watcher itself.
func IsSelfContainer(id string) (bool, string) {
	if id == "" {
		return false, ""
	}
	self, ok := SelfContainerID()
	if !ok {
		return false, ""
	}
	if SameContainer(self, id) {
		return true, "container is Watcher's own container"
	}
	return false, ""
}

func looksLikeContainerID(s string) bool {
	if len(s) < 12 {
		return false
	}
	for _, r := range s {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// SameContainer compares ids that may differ in truncation: Docker accepts a
// 12-character short id for a 64-character one.
func SameContainer(a, b string) bool {
	if len(a) < 12 || len(b) < 12 {
		return false
	}
	if len(a) < len(b) {
		a, b = b, a
	}
	return strings.HasPrefix(a, b)
}
