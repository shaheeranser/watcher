# 00 — Scaffolding

Milestone 00 is project hygiene, not product behavior, so it has **no
`requirements.md` and no `design.md`**. This file is the complete checklist.

Scope note: this milestone creates repository metadata plus a single stub entry
point. It contains no Watcher behavior. Every later milestone under `specs/`
assumes these files exist.

## 1. Repository files

### README.md

- [ ] One-line description of Watcher at the very top (what it is, who it is
      for).
- [ ] Quickstart covering both local input modes:
  - [ ] stdin: `tail -f app.log | watcher`
  - [ ] single file: `watcher --file /var/log/app.log`
- [ ] Short architecture overview naming the four interfaces
      (`Source → Detector → Backend → Sink`) and stating where fingerprinting,
      context curation, and incident tracking sit between them.
- [ ] Roadmap section mirroring the milestones below as checkboxes, one line
      each:
  - [ ] `00` — Scaffolding
  - [ ] `01` — Core engine
  - [ ] `02` — Production shape
  - [ ] `03` — Dashboard
  - [ ] `04` — Evaluation
- [ ] Link placeholder for the separate evaluation / test-harness repo.
- [ ] MIT license badge.

### LICENSE

- [ ] MIT license text.
- [ ] Copyright holder and year filled in.

### CONTRIBUTING.md

- [ ] How to build and test locally: `go build ./...`, `go vet ./...`,
      `go test ./...`.
- [ ] Code style expectation: all code passes `gofmt` (`gofmt -l .` is
      empty).
- [ ] Guidance to open an issue before a non-trivial PR.
- [ ] Note that low-effort/spam PRs will be closed rather than merged.
- [ ] Contains no reference to any specific event or platform.

### AGENTS.md (repository root)

Tool-agnostic instructions for AI coding agents (not tied to any specific
vendor). Must include:

- [ ] Build / test / lint commands.
- [ ] Go code style conventions.
- [ ] Package layout overview (see `specs/01-core-engine/design.md` §3).
- [ ] The `Source` / `Detector` / `Backend` / `Sink` interface boundaries
      agents must respect, and the rule that these stay separate types rather
      than being collapsed into one another.
- [ ] Testing expectations, explicitly including **property-based tests for
      the fingerprint normalization logic**.
- [ ] A pointer telling agents to read `specs/<milestone>/design.md` for
      detailed rationale before implementing that milestone's tasks.

### .gitignore

- [ ] Go build artifacts: compiled binaries, `/bin/`, `*.test`, coverage
      files.
- [ ] `.env`
- [ ] Editor folders (`.vscode/`, `.idea/`) and swap files.
- [ ] `/docs/` — the uncommitted planning-notes directory, kept distinct from
      the committed `specs/` directory.
- [ ] Local database artifacts used by later milestones (e.g. `*.db`,
      `*.sqlite`).

### Go module

- [ ] **go.mod** — module path `github.com/shaheeranser/watcher`, Go directive
      `1.26`.
- [ ] **cmd/watcher/main.go** — the daemon entry point. A stub in this
      milestone: an empty `main` and a package comment. It exists because an
      empty module cannot pass CI (`go vet ./...` and `go test ./...` exit
      non-zero with "matched no packages"), which would otherwise make the
      "CI is green" exit criterion unsatisfiable without behavior. Milestone 01
      T-1.1/T-1.3 grows this into the real entry point.

### GitHub Actions — `.github/workflows/ci.yml`

- [ ] Triggers on `push` and `pull_request`.
- [ ] Sets up Go at the module's declared version.
- [ ] Runs `go build ./...`.
- [ ] Runs `go vet ./...`.
- [ ] Runs `go test ./...`.
- [ ] Fails the job if any step fails.

### Templates

- [ ] `.github/ISSUE_TEMPLATE/bug_report.md` — minimal: version/commit,
      command and config, expected vs actual, and a redacted log excerpt.
- [ ] `.github/ISSUE_TEMPLATE/feature_request.md` — minimal: problem,
      proposed behavior, alternatives considered.
- [ ] `.github/pull_request_template.md` — short checklist: tests pass,
      `gofmt` clean, `go vet` clean, linked issue, spec/docs updated.

## 2. Exit criteria

- [ ] `specs/00-scaffolding/tasks.md` (this file) is committed.
- [ ] Every file above exists at its stated path and is committed.
- [ ] The CI workflow is green on the scaffolding commit.
- [ ] `docs/` exists locally but is **not** tracked by git.
- [ ] `.commandcode/` exists locally but is **not** tracked by git.
- [ ] No Watcher behavior exists yet: `cmd/watcher/main.go` is a stub entry
      point only, with no logic.
