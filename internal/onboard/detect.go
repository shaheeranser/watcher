package onboard

import (
	"os"
	"strings"
)

// InContainer reports whether the process looks containerized, so onboarding
// can refuse and point at the Compose environment instead (INST-ONB-2).
func InContainer() bool {
	return inContainer(os.Stat, os.ReadFile)
}

// inContainer is the testable core: a marker file, or the PID-1 cgroup naming a
// container runtime.
func inContainer(stat func(string) (os.FileInfo, error), readFile func(string) ([]byte, error)) bool {
	for _, marker := range []string{"/.dockerenv", "/run/.containerenv"} {
		if _, err := stat(marker); err == nil {
			return true
		}
	}
	if data, err := readFile("/proc/1/cgroup"); err == nil {
		cgroup := string(data)
		for _, runtime := range []string{"docker", "kubepods", "containerd", "libpod", "podman"} {
			if strings.Contains(cgroup, runtime) {
				return true
			}
		}
	}
	return false
}
