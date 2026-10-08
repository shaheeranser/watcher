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
	"sync"
	"syscall"
	"time"

	"github.com/shaheeranser/watcher/internal/api"
	"github.com/shaheeranser/watcher/internal/backend"
	"github.com/shaheeranser/watcher/internal/config"
	"github.com/shaheeranser/watcher/internal/engine"
	"github.com/shaheeranser/watcher/internal/guard"
	"github.com/shaheeranser/watcher/internal/heartbeat"
	"github.com/shaheeranser/watcher/internal/sink"
	"github.com/shaheeranser/watcher/internal/sink/webhook"
	"github.com/shaheeranser/watcher/internal/source"
	"github.com/shaheeranser/watcher/internal/source/docker"
	"github.com/shaheeranser/watcher/internal/store"
)

const usage = `watcher explains application crashes from one or more log sources.

Usage:
  watcher run [flags]              start the daemon (the long-running process)
  watcher     [flags]              alias for 'watcher run'
  watcher attach [flags]           attach a live dashboard to a running daemon
  watcher eval [flags]             score a captured run against ground truth
  watcher --help                   list the verbs and run's flags

Verbs:
  run                              read the configured sources and report crashes
  attach                           render a running daemon's incident history
  eval                             score explanations against ground truth

Dashboard:
  The daemon is always headless and serves a read API over a Unix socket; the
  'attach' command connects to it and renders a live two-pane view. Incident
  history is persisted to SQLite so it survives a restart.

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
  --api BOOL              serve the read API the dashboard attaches to (WATCHER_API; default true)
  --api-socket PATH       unix socket path for the read API (WATCHER_API_SOCKET; default $XDG_RUNTIME_DIR/watcher.sock)
  --db PATH               SQLite incident history file (WATCHER_DB; default watcher.db)
  --retention DUR         how long incident history is kept (WATCHER_RETENTION; default 720h; 0 disables)
  --occurrence-cap N      occurrence rows kept per incident (WATCHER_OCCURRENCE_CAP; default 100; 0 disables)
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
  --run-id ID             identifier echoed on every result for the eval verb (WATCHER_RUN_ID)

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
	localSink, webhookSink, err := buildSinks(ctx, cfg, isTerminal, logger)
	if err != nil {
		logger.Error("cannot start sinks", "error", err)
		return 1
	}

	startedAt := time.Now()

	// History is a projection for the dashboard. It is opened before the engine
	// so the recorder is ready, but a failure to open it degrades history only:
	// detection and notification continue (DASH-26).
	history := openHistory(cfg, logger)
	storeCtx, storeCancel := context.WithCancel(ctx)
	var storeDone sync.WaitGroup
	storeDone.Add(1)
	go func() {
		defer storeDone.Done()
		history.Run(storeCtx)
	}()

	apiCancel, apiDone, err := startAPI(ctx, cfg, history, logger, startedAt)
	if err != nil {
		logger.Error("cannot start the read API", "socket", cfg.APISocket, "error", err)
		storeCancel()
		storeDone.Wait()
		history.Close()
		return 1
	}

	logStartup(logger, cfg, sources, isTerminal, history.Persistent())

	opts := engine.Options{
		Sources:        sources,
		Backend:        backend.NewOllama(cfg.OllamaURL, cfg.Model, cfg.OllamaTimeout, cfg.OllamaMaxTokens),
		Sink:           localSink,
		Recorder:       history,
		Logger:         logger,
		ContextBefore:  cfg.ContextBefore,
		ContextAfter:   cfg.ContextAfter,
		ContextBudget:  cfg.ContextBudget,
		MaxBlockLines:  cfg.MaxBlockLines,
		Workers:        cfg.Workers,
		ExplainWindow:  cfg.ExplainWindow,
		ThrottleWindow: cfg.ThrottleWindow,
		ResolveWindow:  cfg.ResolveWindow,
		RunID:          cfg.RunID,
	}
	if webhookSink != nil {
		opts.Notifications = webhookSink
	}
	eng := engine.New(opts)

	// The heartbeat runs on its own schedule for as long as the daemon does.
	hb := heartbeat.New(heartbeat.Options{
		URL:      cfg.HeartbeatURL,
		Interval: cfg.HeartbeatInterval,
		Logger:   logger,
		Stats:    liveness(eng, webhookSink, startedAt),
	})

	hbCtx, hbCancel := context.WithCancel(ctx)
	defer hbCancel()
	var hbDone sync.WaitGroup
	if hb.Enabled() {
		hbDone.Add(1)
		go func() {
			defer hbDone.Done()
			hb.Run(hbCtx)
		}()
	}

	runErr := eng.Run(ctx)
	hbCancel()
	hbDone.Wait()
	stopAPI(apiCancel, apiDone, logger)
	storeCancel()
	storeDone.Wait()
	if err := history.Close(); err != nil {
		logger.Warn("closing incident history failed", "error", err)
	}
	if runErr != nil {
		logger.Error("watcher stopped", "error", runErr)
		return 1
	}
	return 0
}

// openHistory opens the SQLite history, falling back to an in-memory backend
// when the database cannot be opened so detection and notification keep working
// (DASH-26). The fallback serves current state and loses it on restart, which
// the attach UI reports as "history unavailable".
func openHistory(cfg config.Config, log *slog.Logger) store.Backend {
	s, err := store.Open(store.Options{
		Path:          cfg.DBPath,
		Retention:     cfg.Retention,
		OccurrenceCap: cfg.OccurrenceCap,
		Logger:        log,
	})
	if err != nil {
		log.Error("cannot open incident history; continuing without persistence",
			"db", cfg.DBPath, "error", err)
		return store.NewMemory(cfg.OccurrenceCap)
	}
	return s
}

// startAPI binds and serves the read API. A bind failure is returned so the
// daemon fails clearly rather than serving without the API the operator asked
// for, and so two daemons never share a socket silently (DASH-5). Disabling the
// API leaves the daemon otherwise unchanged (DASH-6).
func startAPI(ctx context.Context, cfg config.Config, history store.Backend, log *slog.Logger, startedAt time.Time) (context.CancelFunc, *sync.WaitGroup, error) {
	if !cfg.APIEnabled {
		log.Info("read API disabled by configuration")
		return nil, &sync.WaitGroup{}, nil
	}
	srv := api.New(api.Options{
		Socket:  cfg.APISocket,
		Reader:  history,
		Events:  history,
		Logger:  log,
		Started: startedAt,
	})
	l, err := srv.Listen()
	if err != nil {
		return nil, nil, err
	}
	apiCtx, cancel := context.WithCancel(ctx)
	var done sync.WaitGroup
	done.Add(1)
	go func() {
		defer done.Done()
		if err := srv.Serve(apiCtx, l); err != nil {
			log.Error("read API stopped", "error", err)
		}
	}()
	log.Info("read API listening", "socket", cfg.APISocket)
	return cancel, &done, nil
}

func stopAPI(cancel context.CancelFunc, done *sync.WaitGroup, log *slog.Logger) {
	if cancel == nil {
		return
	}
	cancel()
	done.Wait()
}

// liveness adapts the engine's counters and the webhook's dropped count into the
// heartbeat payload.
func liveness(eng *engine.Engine, wh *webhook.Webhook, startedAt time.Time) func() heartbeat.Stats {
	return func() heartbeat.Stats {
		s := eng.Stats()
		var dropped int64
		if wh != nil {
			dropped = wh.Dropped()
		}
		return heartbeat.Stats{
			Uptime:               time.Since(startedAt),
			LinesProcessed:       s.LinesProcessed,
			IncidentsTracked:     s.IncidentsTracked,
			NotificationsSent:    s.NotificationsSent,
			NotificationsDropped: dropped,
		}
	}
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
func buildSinks(ctx context.Context, cfg config.Config, isTerminal bool, log *slog.Logger) (local sink.Sink, webhooks *webhook.Webhook, err error) {
	local = sink.New(os.Stdout, isTerminal)
	if cfg.WebhookURL == "" {
		return local, nil, nil
	}
	webhooks, err = webhook.New(ctx, webhook.Options{
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

// logStartup reports the sources attached, the sinks configured, the state-
// machine windows, and the history/API surface, so an operator can verify
// configuration without attaching a UI (RT-CFG-4, PROD-NFR-4).
func logStartup(log *slog.Logger, cfg config.Config, sources []source.Source, isTerminal, persistent bool) {
	labels := make([]string, len(sources))
	for i, src := range sources {
		labels[i] = src.Name()
	}

	sinkNames := []string{"results=jsonl"}
	if isTerminal {
		sinkNames[0] = "results=terminal"
	}
	if cfg.WebhookURL != "" {
		sinkNames = append(sinkNames, "notifications=webhook("+cfg.WebhookFormat+")")
	}
	if cfg.HeartbeatURL != "" {
		sinkNames = append(sinkNames, "heartbeat="+cfg.HeartbeatInterval.String())
	}

	history := "history=memory"
	if persistent {
		history = "history=" + cfg.DBPath
	}
	api := "api=disabled"
	if cfg.APIEnabled {
		api = "api=" + cfg.APISocket
	}

	log.Info("watcher starting",
		"sources", strings.Join(labels, ", "),
		"sinks", strings.Join(sinkNames, ", "),
		"model", cfg.Model,
		"ollama_url", cfg.OllamaURL,
		"throttle_window", cfg.ThrottleWindow,
		"resolve_window", cfg.ResolveWindow,
		"history", history,
		"api", api,
	)
}
