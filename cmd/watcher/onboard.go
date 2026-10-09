package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/shaheeranser/watcher/internal/config"
	"github.com/shaheeranser/watcher/internal/onboard"
	"github.com/shaheeranser/watcher/internal/systemd"
)

const onboardUsage = `watcher onboard collects the settings a run needs and writes a config file.

Usage:
  watcher onboard [--config PATH]

Flags (environment variable in parentheses):
  --config PATH  where to write the config file (WATCHER_CONFIG;
                 default $XDG_CONFIG_HOME/watcher/config.toml)

It prompts for the Ollama URL, a model, one or more labelled log sources, and an
optional webhook; it verifies Ollama before writing anything, then offers to
install and enable the systemd unit. It refuses to run inside a container, where
the Compose environment: block is the configuration.`

// onboardCommand is the interactive first-run configuration step. It is
// bare-metal only: inside a container it refuses rather than inventing a second
// way to configure the deployment (INST-ONB-1, INST-ONB-2).
func onboardCommand(args []string) int {
	fs := flag.NewFlagSet("watcher onboard", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}

	defaultPath := config.DefaultConfigPath(os.Getenv)
	if v := os.Getenv("WATCHER_CONFIG"); v != "" {
		defaultPath = v
	}
	configPath := fs.String("config", defaultPath, "path to write the config file to")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, onboardUsage)
			return 0
		}
		fmt.Fprintf(os.Stderr, "watcher onboard: %v\n", err)
		return 2
	}

	_, err := onboard.Run(context.Background(), onboard.Options{
		In:               os.Stdin,
		Out:              os.Stderr,
		ConfigPath:       *configPath,
		Model:            onboard.DefaultModel,
		InContainer:      onboard.InContainer,
		NewProber:        onboard.NewProber,
		ServiceAvailable: systemd.Available(),
		InstallService: func(ctx context.Context) error {
			if err := systemd.Install(ctx, systemd.ExecRunner, systemd.UnitPath); err != nil {
				return err
			}
			return systemd.Enable(ctx, systemd.ExecRunner)
		},
	})
	if err != nil {
		if errors.Is(err, onboard.ErrContainerRefused) {
			return 1
		}
		fmt.Fprintf(os.Stderr, "watcher onboard: %v\n", err)
		return 1
	}
	return 0
}
