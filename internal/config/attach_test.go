package config

import (
	"strings"
	"testing"
	"time"
)

func TestParseAttachDefaults(t *testing.T) {
	got, err := ParseAttach(nil, envFrom(nil))
	if err != nil {
		t.Fatal(err)
	}
	if got.APISocket != DefaultSocketFallback {
		t.Errorf("api-socket = %q, want %q", got.APISocket, DefaultSocketFallback)
	}
	if got.RefreshInterval != DefaultRefreshInterval {
		t.Errorf("refresh-interval = %s, want %s", got.RefreshInterval, DefaultRefreshInterval)
	}
	if got.ListFraction != DefaultListFraction {
		t.Errorf("list-fraction = %g, want %g", got.ListFraction, DefaultListFraction)
	}
}

func TestParseAttachPrecedence(t *testing.T) {
	env := map[string]string{
		"XDG_RUNTIME_DIR":          "/run/user/7",
		"WATCHER_REFRESH_INTERVAL": "5s",
		"WATCHER_LIST_FRACTION":    "0.6",
	}
	got, err := ParseAttach([]string{"--refresh-interval", "1s"}, envFrom(env))
	if err != nil {
		t.Fatal(err)
	}
	if got.APISocket != "/run/user/7/watcher.sock" {
		t.Errorf("api-socket = %q", got.APISocket)
	}
	if got.RefreshInterval != time.Second {
		t.Errorf("refresh-interval = %s, want the flag's 1s", got.RefreshInterval)
	}
	if got.ListFraction != 0.6 {
		t.Errorf("list-fraction = %g, want the env's 0.6", got.ListFraction)
	}

	got, err = ParseAttach(nil, envFrom(map[string]string{
		"WATCHER_API_SOCKET":    "/tmp/x.sock",
		"WATCHER_LIST_FRACTION": "0.3",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if got.APISocket != "/tmp/x.sock" || got.ListFraction != 0.3 {
		t.Errorf("unexpected attach config: %+v", got)
	}
}

func TestParseAttachValidation(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantSub string
	}{
		{"zero refresh", []string{"--refresh-interval", "0s"}, nil, "refresh-interval must be positive"},
		{"negative refresh", []string{"--refresh-interval", "-1s"}, nil, "refresh-interval must be positive"},
		{"fraction below range", []string{"--list-fraction", "0"}, nil, "list-fraction must be between"},
		{"fraction above range", []string{"--list-fraction", "1"}, nil, "list-fraction must be between"},
		{"empty socket", []string{"--api-socket", ""}, nil, "api-socket must not be empty"},
		{"bad fraction env", nil, map[string]string{"WATCHER_LIST_FRACTION": "half"}, "WATCHER_LIST_FRACTION"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseAttach(tt.args, envFrom(tt.env))
			if err == nil || !strings.Contains(err.Error(), tt.wantSub) {
				t.Fatalf("error = %v, want substring %q", err, tt.wantSub)
			}
		})
	}
}
