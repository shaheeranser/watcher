package main

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAttachVerbIsRegistered(t *testing.T) {
	if _, ok := verbs["attach"]; !ok {
		t.Fatal("the attach verb must be registered")
	}
}

func TestAttachHelpExitsZero(t *testing.T) {
	silenceStderr(t)
	if code := attachCommand([]string{"--help"}); code != 0 {
		t.Errorf("attach --help exit = %d, want 0", code)
	}
}

func TestAttachNoDaemonFailsFast(t *testing.T) {
	silenceStderr(t)
	socket := filepath.Join(t.TempDir(), "absent.sock")

	start := time.Now()
	code := attachCommand([]string{"--api-socket", socket})
	elapsed := time.Since(start)

	if code == 0 {
		t.Error("attach with no daemon should exit non-zero")
	}
	if elapsed > 3*time.Second {
		t.Errorf("attach took %s; it must fail fast rather than hang (DASH-8)", elapsed)
	}
}
