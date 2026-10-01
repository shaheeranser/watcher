package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/shaheeranser/watcher/internal/backend"
	"github.com/shaheeranser/watcher/internal/config"
	"github.com/shaheeranser/watcher/internal/engine"
	"github.com/shaheeranser/watcher/internal/guard"
	"github.com/shaheeranser/watcher/internal/sink"
	"github.com/shaheeranser/watcher/internal/source"
)

const usage = `watcher explains application crashes from a log stream.

Usage:
  watcher run [flags]              start the daemon (the long-running process)
  watcher     [flags]              alias for 'watcher run'
  watcher --help                   list the verbs and run's flags

Verbs:
  run                              read the configured sources and report crashes

Sources:
  watcher run                      read stdin (unless a Docker project is detected)
  watcher run --file /var/log/app.log
  watcher run --source backend=/var/log/backend.log --source worker=-

Flags (environment variable in parentheses):
  --file PATH             single log file to tail, shorthand for one source (WATCHER_FILE)
  --source LABEL=PATH     labeled source, repeatable; PATH '-' is stdin (WATCHER_SOURCES)
  --model NAME            Ollama model to use (WATCHER_MODEL; required)
  --ollama-url URL        Ollama base URL (WATCHER_OLLAMA_URL; default http://localhost:11434)
  --context-before N      context lines before a crash (WATCHER_CONTEXT_BEFORE; default 20)
  --context-after N       context lines after a crash (WATCHER_CONTEXT_AFTER; default 10)
  --context-budget BYTES  excerpt size budget (WATCHER_CONTEXT_BUDGET; default 8192)
  --workers N             concurrent model calls (WATCHER_WORKERS; default 1)
  --ollama-timeout DUR    per-request timeout (WATCHER_OLLAMA_TIMEOUT; default 60s)
  --explain-window DUR    min interval between explanations of one crash (WATCHER_EXPLAIN_WINDOW; default 15m)
  --max-block-lines N     maximum lines kept per crash block (WATCHER_MAX_BLOCK_LINES; default 200)
  --from-start            read a file from the beginning (WATCHER_FROM_START; default false)

Results go to stdout as text on a terminal or JSON Lines otherwise. All
diagnostics go to stderr.`

func runDaemon(args []string) int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Parse(args, os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, usage)
			return 0
		}
		logger.Error("invalid configuration", "error", err)
		return 2
	}

	sources := buildSources(cfg, logger)
	for _, src := range sources {
		if self, reason := guard.IsSelf(src); self {
			logger.Error("refusing to watch Watcher's own output", "source", src.Name(), "reason", reason)
			return 1
		}
	}

	eng := engine.New(engine.Options{
		Sources:       sources,
		Backend:       backend.NewOllama(cfg.OllamaURL, cfg.Model, cfg.OllamaTimeout),
		Sink:          sink.New(os.Stdout, sink.IsTerminal(os.Stdout)),
		Logger:        logger,
		ContextBefore: cfg.ContextBefore,
		ContextAfter:  cfg.ContextAfter,
		ContextBudget: cfg.ContextBudget,
		MaxBlockLines: cfg.MaxBlockLines,
		Workers:       cfg.Workers,
		ExplainWindow: cfg.ExplainWindow,
	})

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := eng.Run(ctx); err != nil {
		logger.Error("watcher stopped", "error", err)
		return 1
	}
	return 0
}

// buildSources turns the resolved configuration into streams to watch. With no
// source configured it reads stdin, which is milestone 01's behaviour.
func buildSources(cfg config.Config, log *slog.Logger) []source.Source {
	specs := cfg.ResolvedSources()
	if len(specs) == 0 {
		return []source.Source{source.NewStdin(os.Stdin, "", log)}
	}

	sources := make([]source.Source, 0, len(specs))
	for _, spec := range specs {
		label := spec.EffectiveLabel()
		if spec.IsStdin() {
			sources = append(sources, source.NewStdin(os.Stdin, label, log))
			continue
		}
		sources = append(sources, source.NewFile(spec.Path, label, cfg.FromStart, log))
	}
	return sources
}
