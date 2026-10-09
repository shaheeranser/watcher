package onboard

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shaheeranser/watcher/internal/config"
)

type fakeProber struct {
	pingErr error
	models  map[string]bool
	pulled  []string
	pullErr error
	calls   []string
}

func (f *fakeProber) Ping(context.Context) error {
	f.calls = append(f.calls, "ping")
	return f.pingErr
}

func (f *fakeProber) HasModel(_ context.Context, name string) (bool, error) {
	f.calls = append(f.calls, "has:"+name)
	return f.models[name], nil
}

func (f *fakeProber) Pull(_ context.Context, name string) error {
	f.calls = append(f.calls, "pull:"+name)
	f.pulled = append(f.pulled, name)
	return f.pullErr
}

func options(t *testing.T, input string, prober Prober) (Options, *bytes.Buffer, string) {
	t.Helper()
	var out bytes.Buffer
	path := filepath.Join(t.TempDir(), "watcher", "config.toml")
	return Options{
		In:               strings.NewReader(input),
		Out:              &out,
		ConfigPath:       path,
		Model:            DefaultModel,
		InContainer:      func() bool { return false },
		NewProber:        func(string) Prober { return prober },
		ServiceAvailable: false,
	}, &out, path
}

func TestContainerRefusal(t *testing.T) {
	opts, out, path := options(t, "", &fakeProber{})
	opts.InContainer = func() bool { return true }

	_, err := Run(context.Background(), opts)
	if !errors.Is(err, ErrContainerRefused) {
		t.Fatalf("err = %v, want ErrContainerRefused", err)
	}
	if !strings.Contains(out.String(), "environment") {
		t.Errorf("refusal should point at the Compose environment:\n%s", out.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("onboarding wrote a config in a container")
	}
}

func TestUnreachableOllamaWritesNothing(t *testing.T) {
	prober := &fakeProber{pingErr: errors.New("connection refused")}
	opts, out, path := options(t, "\n", prober)

	_, err := Run(context.Background(), opts)
	if err == nil {
		t.Fatal("expected an error for an unreachable Ollama")
	}
	if !strings.Contains(out.String(), "Cannot reach Ollama") {
		t.Errorf("output missing the connectivity failure:\n%s", out.String())
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a config was written despite the failed connectivity check")
	}
}

func TestExistingConfigAbortAndOverwrite(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{DefaultModel: true}}
	opts, _, path := options(t, "n\n", prober)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("model = \"old\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if got, _ := os.ReadFile(path); string(got) != "model = \"old\"\n" {
		t.Errorf("aborting overwrote the file: %q", got)
	}

	// Overwrite: accept the prompt, then answer every subsequent prompt.
	opts, _, _ = options(t, "y\n\n\nbackend\n/x.log\n\n\n", prober)
	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run overwrite: %v", err)
	}
}

func TestHappyPathWritesValidatedConfig(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{DefaultModel: true}}
	opts, out, path := options(t, "\n\nbackend\n/var/log/backend.log\n\n\n", prober)

	outcome, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.ConfigPath != path || outcome.UnitEnabled {
		t.Errorf("outcome = %+v, want the written path and no unit", outcome)
	}
	if !strings.Contains(out.String(), "Wrote "+path) {
		t.Errorf("output missing the write notice:\n%s", out.String())
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("config not written: %v", err)
	}
	want := Render(Value{
		OllamaURL: config.DefaultOllamaURL,
		Model:     DefaultModel,
		Sources:   []config.SourceSpec{{Label: "backend", Path: "/var/log/backend.log"}},
	})
	if string(got) != want {
		t.Errorf("config mismatch:\n--- want ---\n%s\n--- got ---\n%s", want, got)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("config mode = %o, want 0600", info.Mode().Perm())
	}

	// The written file must be loadable by the daemon's own parser.
	cfg, err := config.Parse([]string{"--config", path}, func(string) string { return "" })
	if err != nil {
		t.Fatalf("written config does not parse: %v", err)
	}
	if cfg.Model != DefaultModel || cfg.OllamaURL != config.DefaultOllamaURL {
		t.Errorf("parsed config = %+v", cfg)
	}
	if len(cfg.Sources) != 1 || cfg.Sources[0] != (config.SourceSpec{Label: "backend", Path: "/var/log/backend.log"}) {
		t.Errorf("parsed sources = %+v", cfg.Sources)
	}
}

func TestModelPullWhenAbsent(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{}}
	opts, out, _ := options(t, "\n\ny\nbackend\n/x.log\n\n\n", prober)

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(prober.pulled) != 1 || prober.pulled[0] != DefaultModel {
		t.Errorf("pulled = %v, want [%s]", prober.pulled, DefaultModel)
	}
	if !strings.Contains(out.String(), "Pulling "+DefaultModel) {
		t.Errorf("output missing the pull notice:\n%s", out.String())
	}
}

func TestWebhookCollected(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{DefaultModel: true}}
	opts, _, path := options(t, "\n\nbackend\n/x.log\n\nhttps://hooks.example/abc\ndiscord\n", prober)

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "[webhook]") ||
		!strings.Contains(string(got), `url = "https://hooks.example/abc"`) ||
		!strings.Contains(string(got), `format = "discord"`) {
		t.Errorf("webhook not written:\n%s", got)
	}
}

func TestServiceStepEnablesUnit(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{DefaultModel: true}}
	opts, out, _ := options(t, "\n\nbackend\n/x.log\n\n\ny", prober)
	installed := false
	opts.ServiceAvailable = true
	opts.InstallService = func(context.Context) error { installed = true; return nil }

	outcome, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if !installed || !outcome.UnitEnabled {
		t.Errorf("service not installed: installed=%v outcome=%+v", installed, outcome)
	}
	if !strings.Contains(out.String(), "systemd") {
		t.Errorf("next step should mention systemd:\n%s", out.String())
	}
}

func TestServiceStepSkippedWhenUnavailable(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{DefaultModel: true}}
	opts, out, _ := options(t, "\n\nbackend\n/x.log\n\n\n", prober)

	outcome, err := Run(context.Background(), opts)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if outcome.UnitEnabled {
		t.Error("unit enabled without a service step")
	}
	if !strings.Contains(out.String(), "systemd not found") {
		t.Errorf("expected the manual next step:\n%s", out.String())
	}
}

func TestNoSourceRejected(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{DefaultModel: true}}
	// URL, model, then a blank label and end of input: no source was named.
	opts, _, path := options(t, "\n\n\n", prober)

	if _, err := Run(context.Background(), opts); err == nil {
		t.Fatal("expected an error when no source is named")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("a config was written without a source")
	}
}

func TestPromptOrder(t *testing.T) {
	prober := &fakeProber{models: map[string]bool{DefaultModel: true}}
	opts, out, _ := options(t, "\n\nbackend\n/x.log\n\nhttps://hooks.example/x\n\n", prober)

	if _, err := Run(context.Background(), opts); err != nil {
		t.Fatalf("Run: %v", err)
	}
	text := out.String()
	order := []string{"Ollama URL", "Model", "Source label", "Webhook URL", "Webhook provider"}
	last := -1
	for _, want := range order {
		idx := strings.Index(text, want)
		if idx < 0 {
			t.Fatalf("prompt %q missing from output:\n%s", want, text)
		}
		if idx < last {
			t.Errorf("prompt %q out of order:\n%s", want, text)
		}
		last = idx
	}
}

func TestContainerDetection(t *testing.T) {
	statYes := func(string) (os.FileInfo, error) { return nil, nil }
	statNo := func(string) (os.FileInfo, error) { return nil, os.ErrNotExist }
	readNone := func(string) ([]byte, error) { return nil, os.ErrNotExist }

	if !inContainer(statYes, readNone) {
		t.Error("a marker file should mean containerized")
	}
	if inContainer(statNo, readNone) {
		t.Error("no marker and no cgroup should not mean containerized")
	}
	readDocker := func(string) ([]byte, error) { return []byte("0::/docker/abc\n"), nil }
	if !inContainer(statNo, readDocker) {
		t.Error("a docker cgroup should mean containerized")
	}
}
