// Package guard refuses to attach Watcher to a source that is its own output.
// It is load-bearing: without it, `watcher ... > app.log` followed by
// `watcher --file app.log` would feed explanations back into the detector
// forever (CORE-GRD-1).
package guard

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shaheeranser/watcher/internal/source"
)

// IsSelf reports whether src is Watcher's own output, and why. A caller must
// treat a positive result as fatal (CORE-GRD-2).
func IsSelf(src source.Source) (bool, string) {
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
