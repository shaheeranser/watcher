# 05 — Install Lifecycle: Tasks

Each task notes the requirements it satisfies. Read
[`design.md`](./design.md) before starting, and `../01b-runtime/design.md` for
the `run` verb and configuration model this milestone extends.

Suggested order: config file → installer → service unit → onboarding → docs.

## 1. Installer

- [ ] **T-1.1** — Write the install script: OS/arch detection and release
      selection. *(INST-PKG-1, INST-PKG-2)*
- [ ] **T-1.2** — Download the artifact and verify its checksum before install;
      abort on mismatch. *(INST-PKG-3)*
- [ ] **T-1.3** — Install to the system or per-user bin directory and ensure it
      is on `PATH`; no root for per-user. *(INST-PKG-4, INST-PKG-7)*
- [ ] **T-1.4** — Implement the uninstall path (binary + unit; config only on
      confirmation). *(INST-PKG-5)*
- [ ] **T-1.5** — Fail fast and leave nothing partial on any error.
      *(INST-PKG-6)*
- [ ] **T-1.6** — Tests: fake release server; checksum pass/fail; path
      selection; uninstall; each error path.

## 2. Config file

- [ ] **T-2.1** — Add a TOML loader with the default path and the
      `--config` / `WATCHER_CONFIG` override. *(INST-CFG-1, INST-CFG-5)*
- [ ] **T-2.2** — Insert the file rung into precedence:
      `flag > env > file > default`. *(INST-CFG-2)*
- [ ] **T-2.3** — Absent file → defaults, no error; malformed/invalid →
      clear error naming the file and problem. *(INST-CFG-3, INST-CFG-4)*
- [ ] **T-2.4** — Map every flag/env setting to a file key, including
      `sources` as an array of tables.
- [ ] **T-2.5** — Tests: precedence across all four rungs; malformed; invalid;
      absent; override path.

## 3. Onboarding

- [ ] **T-3.1** — Implement `watcher onboard` and register it in the verb
      table. *(INST-ONB-1)*
- [ ] **T-3.2** — Container detection and refusal. *(INST-ONB-2)*
- [ ] **T-3.3** — Existing-config prompt (overwrite or abort). *(INST-ONB-3)*
- [ ] **T-3.4** — Ollama URL prompt with default; live connectivity check that
      aborts before writing on failure. *(INST-ONB-4, INST-ONB-12)*
- [ ] **T-3.5** — Model prompt (recommended default, accept with enter; offer
      pull if absent). *(INST-ONB-5)*
- [ ] **T-3.6** — Labelled source prompting, looping; no "watch everything"
      option. *(INST-ONB-6, INST-ONB-7, INST-ONB-11)*
- [ ] **T-3.7** — Optional webhook step (URL + provider), skippable.
      *(INST-ONB-8)*
- [ ] **T-3.8** — Service step: offer to install and enable the unit.
      *(INST-ONB-9)*
- [ ] **T-3.9** — Write the config file and print the appropriate next step.
      *(INST-ONB-10)*
- [ ] **T-3.10** — First-run hint from `watcher run` on an interactive terminal
      with no config and no explicit source; non-blocking. *(INST-ONB-13)*
- [ ] **T-3.11** — Tests with a scripted input reader and an `httptest` Ollama:
      prompt order, connectivity failure writes nothing, overwrite prompt,
      container refusal, exact TOML written.

## 4. Service unit

- [ ] **T-4.1** — Write the systemd unit (`ExecStart=watcher run`,
      `Restart=always`, `Type=simple`). *(INST-SVC-1)*
- [ ] **T-4.2** — Install/enable it from onboarding; document the manual path;
      enabling stays optional. *(INST-SVC-1, INST-SVC-2)*
- [ ] **T-4.3** — Confirm the binary never self-daemonizes. *(INST-SVC-3)*
- [ ] **T-4.4** — Golden check that the shipped unit matches the docs.

## 5. Documentation

- [ ] **T-5.1** — README quickstart: install → onboard → run, plus the manual
      (flags/env) path. *(INST-NFR-2)*
- [ ] **T-5.2** — Document the config-file location, schema, and precedence.
- [ ] **T-5.3** — Document the service unit and uninstall.

## 6. Integration

- [ ] **T-6.1** — Install into a temp prefix from the fake release, onboard
      against a fake Ollama, then `watcher run` starts from the config and
      reports an incident from a fixture log.
- [ ] **T-6.2** — Verify the whole flow needs no Go toolchain.
      *(INST-NFR-1)*

## 7. Exit criteria

- [ ] Every `INST-*` requirement has a passing test.
- [ ] `gofmt -l .` empty, `go vet ./...` clean, `go test ./...` passes.
- [ ] A clean machine can go install → onboard → `systemctl enable --now` with
      no manual file editing.
- [ ] `watcher onboard` never runs in a container and never writes an
      unvalidated config.
- [ ] Resolve the open decisions in `design.md` §9 and update the spec with the
      chosen values.
