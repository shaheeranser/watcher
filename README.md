# Watcher

A background daemon that continuously watches application logs, notices crash
and error events on its own, and uses a small self-hosted model (via
[Ollama](https://ollama.com)) to explain the root cause.

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)

## Quickstart

Build from source (Go 1.26 or newer):

```sh
git clone https://github.com/shaheeranser/watcher
cd watcher
go build -o bin/watcher ./cmd/watcher
```

Watcher talks to a local Ollama instance, so pull a small model first:

```sh
ollama serve
ollama pull <model>
```

> The default model is still an open decision
> (`specs/01-core-engine/design.md`, OD-01-1) because it depends on the
> hardware you deploy on. Until it is chosen, pass `--model` explicitly.

### Watch stdin

```sh
tail -f app.log | ./bin/watcher
```

### Watch a single file

```sh
./bin/watcher --file /var/log/app.log
```

When stdout is a terminal the results are rendered for humans. When it is not,
Watcher emits one JSON object per line, so a pipe or a redirect gives you a
machine-readable incident stream:

```sh
./bin/watcher --file /var/log/app.log > incidents.jsonl
```

## Architecture

Log lines flow through four interfaces, with the crash-dedup and context work
sitting in the middle so that the expensive model call happens rarely and with
clean input:

```
                     ┌──────────────────────────────────────────────┐
  Sources            │              Core engine                     │
  ───────            │  ┌───────────┐   ┌───────────┐   ┌────────┐  │
  stdin              │  │ context   │   │  finger-  │   │incident│  │
  file   ──▶ Detector┤  │ curation  │──▶│ printing  │──▶│ state  │──┤
  docker             │  └───────────┘   └───────────┘   │/counts │  │
                     │                                  └────────┘  │
                     └───────────────────────┬──────────────────────┘
                                             │ curated excerpt
                                             ▼
                                    ┌─────────────────┐
                                    │ Backend (Ollama)│
                                    └────────┬────────┘
                                             │ structured explanation
                                             ▼
                                    ┌─────────────────┐
                                    │ Sinks: terminal │
                                    │   jsonl webhook │
                                    └─────────────────┘
```

- **Source** — produces log lines. stdin and a single tailed file locally;
  Docker container streams in production.
- **Detector** — matches crash/error patterns (Go panics, Python tracebacks,
  generic `FATAL`/`Exception` lines) and assembles the multi-line block.
- **Backend** — sends a bounded, curated excerpt to Ollama and returns
  structured fields: summary, likely cause, evidence, suggested fix,
  confidence, severity.
- **Sink** — delivers the finished result: to the terminal, as JSON lines, or
  to a webhook.

Two properties this design protects: it is a **push** system (it notices and
reports on its own, and never waits for you to invoke it against an incident),
and it **deduplicates** (a crash-looping process produces one incident with a
live count, not one alert per occurrence).

## Roadmap

- [ ] `00` — Scaffolding (repository hygiene)
- [ ] `01` — Core engine (stdin/file source, detector, fingerprinting, context
      curation, Ollama backend, guardrail, output)
- [ ] `02` — Production shape (Docker source, Compose packaging, incident state
      machine, webhook sink, heartbeat)
- [ ] `03` — Dashboard (headless daemon, read API, `watcher attach` TUI, SQLite
      history)
- [ ] `04` — Evaluation (ground-truth scoring, pass/fail tally)

Detailed design documentation for each milestone lives in
[`specs/`](./specs), which is committed and public: `requirements.md` (testable
acceptance criteria), `design.md` (rationale and interfaces), and `tasks.md`.
Uncommitted planning notes live in `docs/`, which is not tracked.

## Evaluation harness

Scoring Watcher's explanations against ground truth is driven by a separate
test-harness repository, which produces the scenario logs and the ground-truth
file that `watcher eval` consumes. See
`specs/04-evaluation/design.md` §7 for the integration contract.

> Link forthcoming. <!-- TODO: replace with the harness repository URL -->

## Contributing

See [CONTRIBUTING.md](./CONTRIBUTING.md). AI coding agents should start at
[AGENTS.md](./AGENTS.md).

## License

MIT — see [LICENSE](./LICENSE).
