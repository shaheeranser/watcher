package main

import (
	"bytes"
	"log/slog"
	"os"
	"strings"
	"testing"

	"github.com/shaheeranser/watcher/internal/config"
)

// TestMain keeps config resolution hermetic: no test should read the
// developer's real ~/.config/watcher/config.toml.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "watcher-cmd-test")
	if err == nil {
		os.Setenv("XDG_CONFIG_HOME", dir)
	}
	code := m.Run()
	if err == nil {
		os.RemoveAll(dir)
	}
	os.Exit(code)
}

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

func TestUseDockerDecision(t *testing.T) {
	tests := []struct {
		name       string
		cfg        config.Config
		configured int
		want       bool
	}{
		{
			name: "explicitly disabled",
			cfg:  config.Config{Containers: config.ContainersDisabled, ContainersSet: true},
			want: false,
		},
		{
			name:       "explicit selector applies alongside other sources",
			cfg:        config.Config{Containers: "project=shop", ContainersSet: true},
			configured: 2,
			want:       true,
		},
		{
			name: "the default applies when nothing else is configured",
			cfg:  config.Config{},
			want: true,
		},
		{
			name:       "an explicit source wins over the default",
			cfg:        config.Config{},
			configured: 1,
			want:       false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := useDocker(tt.cfg, tt.configured); got != tt.want {
				t.Errorf("useDocker = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestConfiguredSourcesCarryLabels(t *testing.T) {
	fromFile := configuredSources(config.Config{File: "/var/log/app.log"}, nil)
	if len(fromFile) != 1 || fromFile[0].Name() != "/var/log/app.log" {
		t.Errorf("file shorthand sources = %+v, want one named after the path", fromFile)
	}

	fromSpecs := configuredSources(config.Config{Sources: []config.SourceSpec{
		{Label: "backend", Path: "/var/log/backend.log"},
		{Label: "worker", Path: "-"},
	}}, nil)
	if len(fromSpecs) != 2 {
		t.Fatalf("sources = %d, want 2", len(fromSpecs))
	}
	if fromSpecs[0].Name() != "backend" || fromSpecs[1].Name() != "worker" {
		t.Errorf("labels = %q, %q, want backend, worker", fromSpecs[0].Name(), fromSpecs[1].Name())
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

func TestHintOnboarding(t *testing.T) {
	tests := []struct {
		name     string
		cfg      config.Config
		terminal bool
		want     bool
	}{
		{"first run on a terminal", config.Config{}, true, true},
		{"not a terminal", config.Config{}, false, false},
		{"config file loaded", config.Config{ConfigFileLoaded: true}, true, false},
		{"source configured", config.Config{Sources: []config.SourceSpec{{Label: "a", Path: "/a.log"}}}, true, false},
		{"docker scope set", config.Config{Containers: "project=x", ContainersSet: true}, true, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			log := slog.New(slog.NewTextHandler(&buf, nil))
			hintOnboarding(log, tt.cfg, tt.terminal)
			got := strings.Contains(buf.String(), "watcher onboard")
			if got != tt.want {
				t.Errorf("hint = %v, want %v (output: %q)", got, tt.want, buf.String())
			}
		})
	}
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
