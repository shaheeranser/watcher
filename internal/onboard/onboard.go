// Package onboard implements `watcher onboard`: an interactive, bare-metal-only
// step that collects the configuration a run needs, verifies it against a live
// Ollama, and writes the TOML config file. It never infers what to watch and
// never writes a config it has not validated (INST-ONB-11, INST-ONB-12).
package onboard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shaheeranser/watcher/internal/config"
)

// DefaultModel is the model onboarding pre-fills. It is the one the live
// harness scored best on the crashing stack (0.5b scored 0/5 there, this one
// 3/5); the operator can accept it with enter or name another.
const DefaultModel = "qwen2.5:1.5b"

// ErrContainerRefused reports that onboarding was asked to run inside a
// container, where it must not run (INST-ONB-2).
var ErrContainerRefused = errors.New("refusing to onboard inside a container")

// Options wires the flow to its environment. Every external effect is injected
// so the flow can be driven by a scripted reader and a fake Ollama.
type Options struct {
	In         io.Reader
	Out        io.Writer
	ConfigPath string
	Model      string

	// InContainer reports whether the process is containerized.
	InContainer func() bool

	// NewProber builds a Prober for the Ollama URL the operator typed.
	NewProber func(baseURL string) Prober

	// ServiceAvailable reports whether systemd is present; InstallService
	// installs and enables the unit. When the service is unavailable the step
	// is skipped and the manual next step is printed.
	ServiceAvailable bool
	InstallService   func(ctx context.Context) error
}

// Outcome reports what onboarding did, so the caller can tailor the exit.
type Outcome struct {
	ConfigPath  string
	UnitEnabled bool
}

// Run drives the onboarding flow. It refuses in a container, honors an existing
// config, verifies Ollama before writing anything, writes the file, then offers
// the systemd unit (INST-ONB-3..10).
func Run(ctx context.Context, opts Options) (Outcome, error) {
	if opts.Model == "" {
		opts.Model = DefaultModel
	}
	prompt := NewPrompter(opts.In, opts.Out)
	out := opts.Out

	if opts.InContainer != nil && opts.InContainer() {
		fmt.Fprintln(out, "watcher onboard refuses to run inside a container.")
		fmt.Fprintln(out, "A container is configured through the Compose `environment:` block, not a config file.")
		return Outcome{}, ErrContainerRefused
	}

	if _, err := os.Stat(opts.ConfigPath); err == nil {
		overwrite, err := prompt.Confirm(opts.ConfigPath+" already exists; overwrite it?", false)
		if err != nil {
			return Outcome{}, err
		}
		if !overwrite {
			fmt.Fprintln(out, "Keeping the existing configuration; nothing written.")
			return Outcome{ConfigPath: opts.ConfigPath}, nil
		}
	}

	ollamaURL, err := prompt.Ask("Ollama URL", config.DefaultOllamaURL)
	if err != nil {
		return Outcome{}, err
	}
	prober := opts.NewProber(ollamaURL)

	// Verify connectivity before continuing; a config that only fails later, at
	// `watcher run`, is worse than none (INST-ONB-4, INST-ONB-12).
	if err := prober.Ping(ctx); err != nil {
		fmt.Fprintf(out, "Cannot reach Ollama at %s: %v\n", ollamaURL, err)
		fmt.Fprintln(out, "Start Ollama (or fix the URL) and try again; nothing was written.")
		return Outcome{}, fmt.Errorf("cannot reach Ollama at %s: %w", ollamaURL, err)
	}

	model, err := prompt.Ask("Model", opts.Model)
	if err != nil {
		return Outcome{}, err
	}
	present, err := prober.HasModel(ctx, model)
	if err == nil && !present {
		pull, err := prompt.Confirm(fmt.Sprintf("Model %q is not present locally; pull it now?", model), true)
		if err != nil {
			return Outcome{}, err
		}
		if pull {
			fmt.Fprintf(out, "Pulling %s...\n", model)
			if err := prober.Pull(ctx, model); err != nil {
				return Outcome{}, err
			}
		}
	}

	sources, err := promptSources(prompt, out)
	if err != nil {
		return Outcome{}, err
	}

	webhookURL, err := prompt.Ask("Webhook URL (blank to skip)", "")
	if err != nil {
		return Outcome{}, err
	}
	webhookFormat := ""
	if webhookURL != "" {
		webhookFormat, err = prompt.Ask("Webhook provider (generic, slack, discord)", "generic")
		if err != nil {
			return Outcome{}, err
		}
	}

	value := Value{
		OllamaURL:     ollamaURL,
		Model:         model,
		Sources:       sources,
		WebhookURL:    webhookURL,
		WebhookFormat: webhookFormat,
	}
	// Write the config before enabling the unit: the unit runs `watcher run`,
	// which reads this file, so enabling first would start an unconfigured
	// daemon (a deviation from design §4.1's ordering).
	if err := writeConfig(opts.ConfigPath, value); err != nil {
		return Outcome{}, err
	}
	fmt.Fprintf(out, "Wrote %s\n", opts.ConfigPath)

	outcome := Outcome{ConfigPath: opts.ConfigPath}
	if opts.ServiceAvailable {
		enable, err := prompt.Confirm("Install and enable the systemd unit now?", false)
		if err != nil {
			return Outcome{}, err
		}
		if enable {
			if err := opts.InstallService(ctx); err != nil {
				return Outcome{}, err
			}
			outcome.UnitEnabled = true
		}
	} else {
		fmt.Fprintln(out, "systemd not found; start it manually with `watcher run`.")
	}

	if outcome.UnitEnabled {
		fmt.Fprintln(out, "Watcher is enabled and running under systemd (`systemctl status watcher`).")
	} else {
		fmt.Fprintln(out, "Next: start it with `watcher run`, or `systemctl enable --now watcher` once the unit is installed.")
	}
	return outcome, nil
}

// promptSources collects one or more labelled file sources in a loop. There is
// no "watch everything" option: the operator always names each source
// (INST-ONB-6, INST-ONB-7, INST-ONB-11).
func promptSources(prompt *Prompter, out io.Writer) ([]config.SourceSpec, error) {
	var sources []config.SourceSpec
	for {
		label, err := prompt.Ask("Source label (blank to finish)", "")
		if err != nil {
			if len(sources) == 0 {
				return nil, fmt.Errorf("at least one source is required: %w", err)
			}
			return sources, nil
		}
		if label == "" {
			if len(sources) == 0 {
				fmt.Fprintln(out, "At least one source is required.")
				continue
			}
			return sources, nil
		}
		path, err := prompt.Ask(fmt.Sprintf("Path for %q", label), "")
		if err != nil {
			return nil, err
		}
		if path == "" {
			fmt.Fprintln(out, "A path is required.")
			continue
		}
		sources = append(sources, config.SourceSpec{Label: label, Path: path})
	}
}

// writeConfig writes the rendered TOML at mode 0600, since it can hold a
// webhook secret (OD-05-5).
func writeConfig(path string, v Value) error {
	if dir := filepath.Dir(path); dir != "" {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return fmt.Errorf("create config directory: %w", err)
		}
	}
	if err := os.WriteFile(path, []byte(Render(v)), 0o600); err != nil {
		return fmt.Errorf("write config file %s: %w", path, err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("restrict config file %s: %w", path, err)
	}
	return nil
}
