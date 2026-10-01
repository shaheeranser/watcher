// Command watcher is the Watcher daemon: it reads a log stream, notices crash
// and error events on its own, deduplicates them by fingerprint, and reports an
// explanation from a local model — without a human pointing it at an incident.
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
  watcher                          read log lines from stdin
  watcher --file /var/log/app.log  tail a single log file

Flags (environment variable in parentheses):
  --file PATH             log file to tail (WATCHER_FILE; empty reads stdin)
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

func main() {
	os.Exit(run())
}

func run() int {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	cfg, err := config.Parse(os.Args[1:], os.Getenv)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprintln(os.Stderr, usage)
			return 0
		}
		logger.Error("invalid configuration", "error", err)
		return 2
	}

	var src source.Source
	if cfg.File == "" {
		src = source.NewStdin(os.Stdin, "", logger)
	} else {
		src = source.NewFile(cfg.File, "", cfg.FromStart, logger)
	}

	if self, reason := guard.IsSelf(src); self {
		logger.Error("refusing to watch Watcher's own output", "reason", reason)
		return 1
	}

	eng := engine.New(engine.Options{
		Source:        src,
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
