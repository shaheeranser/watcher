package main

import (
	"os"
	"testing"
)

type spyDaemon struct {
	called bool
	args   []string
	code   int
}

func (s *spyDaemon) run(args []string) int {
	s.called = true
	s.args = args
	return s.code
}

func TestDispatch(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantCalled bool
		wantArgs   []string
	}{
		{
			name:     "run verb dispatches with its remaining args",
			args:     []string{"run", "--model", "m"},
			wantCode: 7, wantCalled: true, wantArgs: []string{"--model", "m"},
		},
		{
			name:     "flags first is an alias for run",
			args:     []string{"--model", "m", "--file", "/a.log"},
			wantCode: 7, wantCalled: true, wantArgs: []string{"--model", "m", "--file", "/a.log"},
		},
		{
			name:     "no arguments is an alias for run",
			args:     nil,
			wantCode: 7, wantCalled: true, wantArgs: nil,
		},
		{
			name:     "unknown verb is a usage error and does not run",
			args:     []string{"attach"},
			wantCode: 2, wantCalled: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &spyDaemon{code: 7}
			table := map[string]func([]string) int{"run": spy.run}

			got := dispatch(tt.args, table)
			if got != tt.wantCode {
				t.Errorf("code = %d, want %d", got, tt.wantCode)
			}
			if spy.called != tt.wantCalled {
				t.Fatalf("daemon called = %v, want %v", spy.called, tt.wantCalled)
			}
			if tt.wantCalled && !equalStrings(spy.args, tt.wantArgs) {
				t.Errorf("daemon args = %v, want %v", spy.args, tt.wantArgs)
			}
		})
	}
}

func TestRunVerbIsRegistered(t *testing.T) {
	if _, ok := verbs["run"]; !ok {
		t.Fatal("the run verb must be registered as the daemon entry point")
	}
}

func TestVerbNamesAreSorted(t *testing.T) {
	got := verbNames(map[string]func([]string) int{"eval": nil, "run": nil, "attach": nil})
	if got != "attach, eval, run" {
		t.Errorf("verbNames = %q, want %q", got, "attach, eval, run")
	}
}

func TestHelpExitsZero(t *testing.T) {
	silenceStderr(t)
	if code := runDaemon([]string{"--help"}); code != 0 {
		t.Errorf("--help exit = %d, want 0", code)
	}
}

func TestBadConfigurationExitsWithCode2(t *testing.T) {
	silenceStderr(t)
	t.Setenv("WATCHER_MODEL", "")

	if code := runDaemon([]string{"--no-such-flag"}); code != 2 {
		t.Errorf("unknown flag exit = %d, want 2", code)
	}
	if code := runDaemon(nil); code != 2 {
		t.Errorf("missing model exit = %d, want 2", code)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func silenceStderr(t *testing.T) {
	t.Helper()
	f, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	orig := os.Stderr
	os.Stderr = f
	t.Cleanup(func() {
		os.Stderr = orig
		f.Close()
	})
}
