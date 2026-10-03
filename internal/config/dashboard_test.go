package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseDashboardDefaults(t *testing.T) {
	got, err := Parse([]string{"--model", "m"}, envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if !got.APIEnabled {
		t.Error("API should be enabled by default")
	}
	if got.APISocket != DefaultSocketFallback {
		t.Errorf("api-socket = %q, want fallback %q without XDG_RUNTIME_DIR", got.APISocket, DefaultSocketFallback)
	}
	if got.DBPath != DefaultDBPath {
		t.Errorf("db = %q, want %q", got.DBPath, DefaultDBPath)
	}
	if got.Retention != DefaultRetention {
		t.Errorf("retention = %s, want %s", got.Retention, DefaultRetention)
	}
	if got.OccurrenceCap != DefaultOccurrenceCap {
		t.Errorf("occurrence-cap = %d, want %d", got.OccurrenceCap, DefaultOccurrenceCap)
	}
}

func TestParseDashboardSettings(t *testing.T) {
	tests := []struct {
		name string
		args []string
		env  map[string]string
		want func(Config) bool
	}{
		{
			name: "socket prefers the runtime directory",
			args: []string{"--model", "m"},
			env:  map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000"},
			want: func(c Config) bool { return c.APISocket == "/run/user/1000/watcher.sock" },
		},
		{
			name: "explicit socket env wins over the runtime directory",
			args: []string{"--model", "m"},
			env:  map[string]string{"XDG_RUNTIME_DIR": "/run/user/1000", "WATCHER_API_SOCKET": "/tmp/custom.sock"},
			want: func(c Config) bool { return c.APISocket == "/tmp/custom.sock" },
		},
		{
			name: "flag wins over environment",
			args: []string{"--model", "m", "--api-socket", "/flag.sock", "--db", "/flag.db", "--retention", "1h", "--occurrence-cap", "9"},
			env:  map[string]string{"WATCHER_API_SOCKET": "/env.sock", "WATCHER_DB": "/env.db", "WATCHER_RETENTION": "2h", "WATCHER_OCCURRENCE_CAP": "5"},
			want: func(c Config) bool {
				return c.APISocket == "/flag.sock" && c.DBPath == "/flag.db" &&
					c.Retention == time.Hour && c.OccurrenceCap == 9
			},
		},
		{
			name: "environment settings",
			args: []string{"--model", "m"},
			env: map[string]string{
				"WATCHER_DB":             "/var/lib/watcher/watcher.db",
				"WATCHER_RETENTION":      "720h",
				"WATCHER_OCCURRENCE_CAP": "250",
				"WATCHER_API":            "false",
			},
			want: func(c Config) bool {
				return c.DBPath == "/var/lib/watcher/watcher.db" && c.Retention == 720*time.Hour &&
					c.OccurrenceCap == 250 && !c.APIEnabled
			},
		},
		{
			name: "api can be disabled by flag",
			args: []string{"--model", "m", "--api=false"},
			env:  nil,
			want: func(c Config) bool { return !c.APIEnabled },
		},
		{
			name: "retention and occurrence cap can be disabled",
			args: []string{"--model", "m", "--retention", "0s", "--occurrence-cap", "0"},
			env:  nil,
			want: func(c Config) bool { return c.Retention == 0 && c.OccurrenceCap == 0 },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Parse(tt.args, envFrom(tt.env))
			if err != nil {
				t.Fatalf("Parse: %v", err)
			}
			if !tt.want(got) {
				t.Errorf("unexpected config: %+v", got)
			}
		})
	}
}

func TestParseDashboardValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantSub string
	}{
		{"negative retention", []string{"--model", "m", "--retention", "-1s"}, "retention must not be negative"},
		{"negative occurrence cap", []string{"--model", "m", "--occurrence-cap", "-1"}, "occurrence-cap must not be negative"},
		{"empty db path", []string{"--model", "m", "--db", ""}, "db path must not be empty"},
		{"empty api socket", []string{"--model", "m", "--api-socket", ""}, "api-socket must not be empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Parse(tt.args, envFrom(nil))
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}
