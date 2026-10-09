package main

import (
	"os"
	"testing"
)

func TestOnboardVerbIsRegistered(t *testing.T) {
	if _, ok := verbs["onboard"]; !ok {
		t.Fatal("the onboard verb must be registered")
	}
}

func TestOnboardHelpExitsZero(t *testing.T) {
	silenceStderr(t)
	if code := onboardCommand([]string{"--help"}); code != 0 {
		t.Errorf("--help exit = %d, want 0", code)
	}
}

func TestOnboardBadFlagExitsTwo(t *testing.T) {
	silenceStderr(t)
	if code := onboardCommand([]string{"--no-such-flag"}); code != 2 {
		t.Errorf("unknown flag exit = %d, want 2", code)
	}
}

// TestOnboardClosedInputFails drives the verb's wiring without a terminal: with
// stdin at EOF and a fresh config path the flow cannot proceed and must exit
// non-zero rather than hang.
func TestOnboardClosedInputFails(t *testing.T) {
	silenceStderr(t)
	t.Setenv("WATCHER_CONFIG", t.TempDir()+"/config.toml")

	devNull, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer devNull.Close()
	orig := os.Stdin
	os.Stdin = devNull
	t.Cleanup(func() { os.Stdin = orig })

	if code := onboardCommand(nil); code != 1 {
		t.Errorf("exit = %d, want 1", code)
	}
}
