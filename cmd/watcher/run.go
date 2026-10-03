package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/shaheeranser/watcher/internal/backend"
	"github.com/shaheeranser/watcher/internal/config"
	"github.com/shaheeranser/watcher/internal/engine"
	"github.com/shaheeranser/watcher/internal/guard"
	"github.com/shaheeranser/watcher/internal/sink"
	"github.com/shaheeranser/watcher/internal/sink/webhook"
	"github.com/shaheeranser/watcher/internal/source"
	"github.com/shaheeranser/watcher/internal/source/docker"
)

const usage = `watcher explains application crashes from one or more log sources.

Usage:
  watcher run [flags]              start the daemon (the long-running process)
  watcher     [flags]              alias for 'watcher run'
  watcher --help                   list the verbs and run's flags

Verbs:
  run                              read the configured sources and report crashes

Sources (repeatable; a lone file reads its path as its label, stdin is "stdin"):
  watcher run                                 stdin, or the Compose project when in one
  watcher run --file /var/log/app.log         one file
  watcher run --source backend=/var/log/app.log --source worker=-
  watcher run --containers project=shop       a Compose project's containers

Flags (environment variable in parentheses):
  --file PATH             single log file, shorthand for one source (WATCHER_FILE)
  --source LABEL=PATH     labeled source, repeatable; PATH '-' is stdin (WATCHER_SOURCES)
  --containers SELECTOR   Docker scope: project=, label=, name=, service=, or 'none' (WATCHER_CONTAINERS)
  --docker-host HOST      Docker Engine host (WATCHER_DOCKER_HOST; default unix:///var/run/docker.sock)
  --docker-since DUR      existing container-log lookback (WATCHER_DOCKER_SINCE; default 0s)
  --webhook-url URL       notification webhook; empty disables it (WATCHER_WEBHOOK_URL)
  --webhook-format F      generic, slack, or discord (WATCHER_WEBHOOK_FORMAT; default generic)
  --webhook-retries N     delivery attempts before the fallback (WATCHER_WEBHOOK_RETRIES; default 5)
  --webhook-backoff-base D  first retry delay (WATCHER_WEBHOOK_BACKOFF_BASE; default 1s)
  --webhook-backoff-max D   retry delay cap (WATCHER_WEBHOOK_BACKOFF_MAX; default 30s)
  --webhook-fallback PATH   file for undeliverable notifications (WATCHER_WEBHOOK_FALLBACK; default undelivered.jsonl)
  --throttle-window DUR   min interval between notifications per incident (WATCHER_THROTTLE_WINDOW; default 15m)
  --resolve-window DUR    quiet period before an incident is reported resolved (WATCHER_RESOLVE_WINDOW; default 2m)
  --heartbeat-url URL     dead-man's-switch URL; empty disables the heartbeat (WATCHER_HEARTBEAT_URL)
  --heartbeat-interval D  interval between heartbeat pings (WATCHER_HEARTBEAT_INTERVAL; default 60s)
  --model NAME            Ollama model to use (WATCHER_MODEL; required)
  --ollama-url URL        Ollama base URL (WATCHER_OLLAMA_URL; default http://localhost:11434)
  --context-before N      context lines before a crash (WATCHER_CONTEXT_BEFORE; default 20)
  --context-after N       context lines after a crash (WATCHER_CONTEXT_AFTER; default 10)
  --context-budget BYTES  excerpt size budget (WATCHER_CONTEXT_BUDGET; default 8192)
  --workers N             concurrent model calls (WATCHER_WORKERS; default 1)
  --ollama-timeout DUR    per-request timeout (WATCHER_OLLAMA_TIMEOUT; default 60s)
  --ollama-max-tokens N   tokens the model may generate per request (WATCHER_OLLAMA_MAX_TOKENS; default 512)
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

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	sources, err := buildSources(cfg, logger)
	if err != nil {
		logger.Error("cannot start sources", "error", err)
		return 1
	}
	for _, src := range sources {
		if self, reason := guard.IsSelf(src); self {
			logger.Error("refusing to watch Watcher's own output", "source", src.Name(), "reason", reason)
			return 1
		}
	}

	isTerminal := sink.IsTerminal(os.Stdout)
	localSink, notifySink, err := buildSinks(ctx, cfg, isTerminal, logger)
	if err != nil {
		logger.Error("cannot start sinks", "error", err)
		return 1
	}

	logStartup(logger, cfg, sources, isTerminal)

	eng := engine.New(engine.Options{
		Sources:        sources,
		Backend:        backend.NewOllama(cfg.OllamaURL, cfg.Model, cfg.OllamaTimeout, cfg.OllamaMaxTokens),
		Sink:           localSink,
		Notifications:  notifySink,
		Logger:         logger,
		ContextBefore:  cfg.ContextBefore,
		ContextAfter:   cfg.ContextAfter,
		ContextBudget:  cfg.ContextBudget,
		MaxBlockLines:  cfg.MaxBlockLines,
		Workers:        cfg.Workers,
		ExplainWindow:  cfg.ExplainWindow,
		ThrottleWindow: cfg.ThrottleWindow,
		ResolveWindow:  cfg.ResolveWindow,
	})

	if err := eng.Run(ctx); err != nil {
		logger.Error("watcher stopped", "error", err)
		return 1
	}
	return 0
}

// buildSources turns the configuration into the streams to watch. With no
// source configured it reads stdin, unless Watcher is running in a Compose
// project, in which case the project's siblings are the documented default.
func buildSources(cfg config.Config, log *slog.Logger) ([]source.Source, error) {
	sources := configuredSources(cfg, log)

	if !useDocker(cfg, len(sources)) {
		if len(sources) == 0 {
			sources = append(sources, source.NewStdin(os.Stdin, "", log))
		}
		return sources, nil
	}

	d, err := docker.New(cfg.DockerHost, cfg.Containers, cfg.DockerSince, log)
	if err != nil {
		// An explicit request must not be silently downgraded; the auto default
		// falls back to stdin so a bare-metal run never requires Docker.
		if cfg.ContainersSet {
			return nil, err
		}
		log.Warn("docker source unavailable; reading stdin", "error", err)
		if len(sources) == 0 {
			sources = append(sources, source.NewStdin(os.Stdin, "", log))
		}
		return sources, nil
	}
	return append(sources, d), nil
}

// useDocker decides whether a Docker source belongs in this run. An explicit
// --containers always is (unless disabled); otherwise the default only applies
// when no other source is configured, so an explicit source wins outright.
func useDocker(cfg config.Config, configuredSources int) bool {
	if cfg.Containers == config.ContainersDisabled {
		return false
	}
	if cfg.ContainersSet {
		return true
	}
	return configuredSources == 0
}

func configuredSources(cfg config.Config, log *slog.Logger) []source.Source {
	specs := cfg.ResolvedSources()
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

// buildSinks assembles the local result stream (terminal or JSON Lines on
// stdout) and, when configured, the notification channel. They are separate
// outputs: the local stream reports every occurrence, while the notification
// channel carries only the state machine's new/ongoing/resolved decisions.
func buildSinks(ctx context.Context, cfg config.Config, isTerminal bool, log *slog.Logger) (local sink.Sink, notifications sink.Sink, err error) {
	local = sink.New(os.Stdout, isTerminal)
	if cfg.WebhookURL == "" {
		return local, nil, nil
	}
	webhooks, err := webhook.New(ctx, webhook.Options{
		URL:         cfg.WebhookURL,
		Provider:    cfg.WebhookFormat,
		Retries:     cfg.WebhookRetries,
		BackoffBase: cfg.WebhookBackoffBase,
		BackoffMax:  cfg.WebhookBackoffMax,
		Fallback:    cfg.WebhookFallback,
		Logger:      log,
	})
	if err != nil {
		return nil, nil, fmt.Errorf("webhook sink: %w", err)
	}
	return local, webhooks, nil
}

// logStartup reports the sources attached and the sinks configured, so an
// operator can verify configuration without attaching a UI (RT-CFG-4).
func logStartup(log *slog.Logger, cfg config.Config, sources []source.Source, isTerminal bool) {
	labels := make([]string, len(sources))
	for i, src := range sources {
		labels[i] = src.Name()
	}

	sinkNames := []string{"jsonl"}
	if isTerminal {
		sinkNames[0] = "terminal"
	}
	if cfg.WebhookURL != "" {
		sinkNames = append(sinkNames, "webhook("+cfg.WebhookFormat+")")
	}

	log.Info("watcher starting",
		"sources", strings.Join(labels, ", "),
		"sinks", strings.Join(sinkNames, ", "),
		"model", cfg.Model,
		"ollama_url", cfg.OllamaURL,
		"throttle_window", cfg.ThrottleWindow,
	)
}
