package systemd

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// UnitPath is where a system install places the unit.
const UnitPath = "/etc/systemd/system/watcher.service"

// Runner executes one external command. Injecting it keeps the systemd
// integration testable without a live systemd.
type Runner func(ctx context.Context, name string, args ...string) error

// ExecRunner runs the command for real and folds its output into the error.
func ExecRunner(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s %v: %w: %s", name, args, err, out)
	}
	return nil
}

// lookPath is a seam so tests can simulate systemd's absence.
var lookPath = exec.LookPath

// Available reports whether systemctl is present, so onboarding can skip the
// service step where systemd is absent — macOS, for instance (INST-SVC-4).
func Available() bool {
	_, err := lookPath("systemctl")
	return err == nil
}

// Install writes the unit to path and reloads the manager so it is picked up.
func Install(ctx context.Context, run Runner, path string) error {
	if err := os.WriteFile(path, []byte(Unit()), 0o644); err != nil {
		return fmt.Errorf("install unit to %s: %w", path, err)
	}
	return run(ctx, "systemctl", "daemon-reload")
}

// Enable starts the unit now and at boot; enabling is optional, a foreground
// `watcher run` works without it (INST-SVC-2).
func Enable(ctx context.Context, run Runner) error {
	if err := run(ctx, "systemctl", "enable", "--now", "watcher"); err != nil {
		return fmt.Errorf("enable watcher: %w", err)
	}
	return nil
}

// Remove stops and disables the unit, then deletes it. Disabling is
// best-effort: the unit may not be enabled or systemd may be gone, but the
// file is still removed.
func Remove(ctx context.Context, run Runner, path string) error {
	_ = run(ctx, "systemctl", "disable", "--now", "watcher")
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("remove unit %s: %w", path, err)
	}
	_ = run(ctx, "systemctl", "daemon-reload")
	return nil
}
