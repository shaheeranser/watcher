package systemd

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const wantUnit = `[Unit]
Description=Watcher
After=network-online.target

[Service]
ExecStart=/usr/local/bin/watcher run
Restart=always
RestartSec=2
Type=simple

[Install]
WantedBy=multi-user.target
`

func TestUnitGolden(t *testing.T) {
	if got := Unit(); got != wantUnit {
		t.Errorf("unit mismatch:\n--- want ---\n%s\n--- got ---\n%s", wantUnit, got)
	}
}

func TestUnitHasRequiredDirectives(t *testing.T) {
	unit := Unit()
	for _, want := range []string{
		"ExecStart=/usr/local/bin/watcher run",
		"Restart=always",
		"Type=simple",
		"WantedBy=multi-user.target",
	} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit missing %q", want)
		}
	}
	// Watcher must not self-daemonize; the unit is what backgrounds it
	// (INST-SVC-3).
	for _, forbidden := range []string{"setsid", "fork"} {
		if strings.Contains(unit, forbidden) {
			t.Errorf("unit should not contain %q", forbidden)
		}
	}
}

type call struct {
	name string
	args []string
}

func recordingRunner(calls *[]call, err error) Runner {
	return func(_ context.Context, name string, args ...string) error {
		*calls = append(*calls, call{name: name, args: args})
		return err
	}
}

func TestInstallWritesUnitAndReloads(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watcher.service")
	var calls []call

	if err := Install(context.Background(), recordingRunner(&calls, nil), path); err != nil {
		t.Fatalf("Install: %v", err)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("unit not written: %v", err)
	}
	if string(got) != Unit() {
		t.Errorf("installed unit differs from the shipped one")
	}
	if len(calls) != 1 || calls[0].name != "systemctl" || strings.Join(calls[0].args, " ") != "daemon-reload" {
		t.Errorf("calls = %+v, want one daemon-reload", calls)
	}
}

func TestInstallPropagatesWriteFailure(t *testing.T) {
	// A path whose parent does not exist cannot be written.
	path := filepath.Join(t.TempDir(), "missing", "watcher.service")
	if err := Install(context.Background(), recordingRunner(new([]call), nil), path); err == nil {
		t.Fatal("expected a write error")
	}
}

func TestEnableCallsSystemctl(t *testing.T) {
	var calls []call
	if err := Enable(context.Background(), recordingRunner(&calls, nil)); err != nil {
		t.Fatalf("Enable: %v", err)
	}
	if len(calls) != 1 || strings.Join(calls[0].args, " ") != "enable --now watcher" {
		t.Errorf("calls = %+v, want enable --now watcher", calls)
	}
}

func TestRemoveDeletesUnitAndIsBestEffort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "watcher.service")
	if err := os.WriteFile(path, []byte(Unit()), 0o644); err != nil {
		t.Fatal(err)
	}
	var calls []call
	// systemctl failing (no systemd, unit not enabled) must not stop removal.
	failing := recordingRunner(&calls, errors.New("systemctl unavailable"))

	if err := Remove(context.Background(), failing, path); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("unit still present after Remove")
	}

	// Removing when the file is already gone is not an error.
	if err := Remove(context.Background(), failing, path); err != nil {
		t.Errorf("Remove of a missing unit = %v, want nil", err)
	}
}

func TestAvailable(t *testing.T) {
	orig := lookPath
	t.Cleanup(func() { lookPath = orig })

	lookPath = func(string) (string, error) { return "/usr/bin/systemctl", nil }
	if !Available() {
		t.Error("Available = false, want true when systemctl is on PATH")
	}

	lookPath = func(string) (string, error) { return "", errors.New("not found") }
	if Available() {
		t.Error("Available = true, want false when systemctl is absent")
	}
}
