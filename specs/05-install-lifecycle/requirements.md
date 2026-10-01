# 05 — Install Lifecycle: Requirements

## 1. Purpose

Take a person from "I heard about Watcher" to "Watcher is quietly running in
the background on my machine":

1. **install** it without a build step;
2. **configure** it through an optional, first-run onboarding instead of a pile
   of flags;
3. leave it **running in the background**, supervised by the platform, with no
   self-managed daemonization.

The engine (01) and its runtime (01b) already exist; this milestone is the
*local device* lifecycle around them. It also names the one deployment shape it
deliberately does **not** touch: the container, whose configuration is the
Compose `environment:` block (02).

## 2. Scope

**In scope**

- A `curl`-style shell installer: OS/arch detection, checksum verification,
  placement on `PATH`, and an uninstall path.
- `watcher onboard`: an interactive, bare-metal-only configuration step that
  writes a config file.
- A TOML config file and the extended `flag > env > file > default` precedence.
- A minimal systemd unit, installable by onboarding or documented for manual
  setup.
- Wiring the README roadmap and quickstart to the new flow.

**Out of scope**

- Package-manager distribution (Homebrew, apt, scoop) — a stretch goal.
- launchd/macOS support — an open item (OD-05-4), not built here.
- Any onboarding in a container — explicitly forbidden (INST-ONB-2).
- Changing detection, fingerprinting, the Docker source, or the state machine.

Requirement IDs use the form `INST-<AREA>-<n>`. Requirements build on
`../01b-runtime/requirements.md` (the `run` verb, labeled sources, config
precedence).

## 3. Functional requirements

### 3.1 Installer (`INST-PKG`)

- **INST-PKG-1** — THE SYSTEM SHALL provide a shell installer invocable as
  `curl -fsSL <url> | sh` that installs a released binary with no Go toolchain
  or build step.
- **INST-PKG-2** — THE SYSTEM SHALL detect the host OS and architecture and
  fetch the matching release artifact.
- **INST-PKG-3** — THE SYSTEM SHALL verify the artifact against a published
  checksum before installing, and SHALL abort on mismatch.
- **INST-PKG-4** — THE SYSTEM SHALL install to a system bin directory
  (`/usr/local/bin`) or a per-user one (`~/.local/bin`), selectable, and SHALL
  make the binary runnable from `PATH`.
- **INST-PKG-5** — THE SYSTEM SHALL provide an uninstall path that removes the
  binary and the installed unit, and removes the config file only on explicit
  confirmation.
- **INST-PKG-6** — WHEN any step fails (download, checksum, permission), THE
  SYSTEM SHALL abort with a clear message and a non-zero exit, leaving no
  partial install.
- **INST-PKG-7** — Per-user installation SHALL NOT require root.

### 3.2 Config file (`INST-CFG`)

- **INST-CFG-1** — THE SYSTEM SHALL read a TOML config file at
  `~/.config/watcher/config.toml` (or the platform-appropriate equivalent).
- **INST-CFG-2** — Configuration precedence SHALL become
  `flag > env > file > default`, additively: no existing flag or environment
  variable changes meaning.
- **INST-CFG-3** — WHEN the config file is absent, THE SYSTEM SHALL run with
  defaults and no error.
- **INST-CFG-4** — WHEN the config file is malformed or holds an invalid value,
  THE SYSTEM SHALL fail with a clear error naming the file and the problem,
  rather than starting with partial configuration.
- **INST-CFG-5** — The config file's path SHALL be overridable by a flag and an
  environment variable.

### 3.3 Onboarding (`INST-ONB`)

- **INST-ONB-1** — THE SYSTEM SHALL provide `watcher onboard`, which
  interactively collects configuration and writes the config file.
- **INST-ONB-2** — `watcher onboard` SHALL NOT run as part of a container's
  startup; inside a container it SHALL refuse with a clear message, because the
  Compose `environment:` block is the container's configuration.
- **INST-ONB-3** — WHEN a config file already exists, onboarding SHALL ask to
  overwrite or abort, and SHALL never silently replace it.
- **INST-ONB-4** — Onboarding SHALL prompt for the Ollama URL (default
  `http://localhost:11434`) and SHALL verify connectivity to it before
  continuing, failing clearly and immediately if it is unreachable.
- **INST-ONB-5** — Onboarding SHALL prompt for a model, pre-filled with the
  recommended default, and SHALL offer to run the pull if the chosen model is
  not present locally.
- **INST-ONB-6** — Onboarding SHALL prompt for one or more sources to watch
  (file paths), labelled, looping to add more.
- **INST-ONB-7** — Onboarding SHALL NOT offer an unscoped "watch everything on
  this machine" option; the operator always draws the source boundary.
- **INST-ONB-8** — Onboarding SHALL optionally configure a webhook (URL and
  provider: Slack, Discord, or generic), and that step SHALL be skippable with
  no loss of core functionality.
- **INST-ONB-9** — Onboarding SHALL offer to install and enable the systemd
  unit, so it can take someone from "just installed" to "running in the
  background" in one pass.
- **INST-ONB-10** — Onboarding SHALL write the config file and print the
  appropriate next step depending on whether the unit was enabled.
- **INST-ONB-11** — Onboarding SHALL NOT infer what to watch from log content;
  every watched thing is an explicitly named, labelled source (per 01b
  RT-SRC-7).
- **INST-ONB-12** — Onboarding SHALL NOT write a config that has not been
  validated against a live Ollama connection.
- **INST-ONB-13** — WHEN `watcher run` starts interactively (a terminal, no
  config file, no explicit source), THE SYSTEM SHALL print a one-line hint that
  `watcher onboard` exists, and SHALL NOT block waiting for input.
- **INST-ONB-14** — *Future extension (not built here):* onboarding SHOULD
  eventually accept a flag for every prompt (`--file`, `--model`, `--yes`, …)
  so provisioning tools (Ansible, cloud-init, image build steps) can drive it
  non-interactively.

### 3.4 Service unit (`INST-SVC`)

- **INST-SVC-1** — THE SYSTEM SHALL provide a minimal systemd unit
  (`ExecStart=watcher run`, `Restart=always`, `Type=simple`), installable by
  onboarding or documented for manual setup.
- **INST-SVC-2** — `systemctl enable --now watcher` SHALL be sufficient to run
  Watcher at boot and stop it on shutdown; enabling SHALL be optional.
- **INST-SVC-3** — Watcher SHALL NOT self-daemonize: no double-fork, no
  `setsid`, no PID-file backgrounding. It always runs in the foreground and
  relies on the supervisor.
- **INST-SVC-4** — A launchd (macOS) equivalent SHALL be an open, undecided
  item rather than built here.

## 4. Non-functional requirements

- **INST-NFR-1** — Installation SHALL NOT require a Go toolchain, a compiler,
  or a build step on the target machine.
- **INST-NFR-2** — Onboarding SHALL be optional: a run configured entirely by
  flags and environment variables SHALL need no config file.
- **INST-NFR-3** — The binary SHALL remain a single static artifact with
  `CGO_ENABLED=0`.
- **INST-NFR-4** — The installed unit and the config file SHALL be plain,
  inspectable text; no opaque state.

## 5. Traceability

Design rationale is in [`design.md`](./design.md); the task breakdown is in
[`tasks.md`](./tasks.md).
