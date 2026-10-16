# Security Policy

## Supported versions

Watcher is pre-1.0 and ships release candidates. Only the latest release is
supported — please reproduce against it before reporting.

| Version | Supported |
|---|---|
| Latest release (`v0.1.0-rc.x`) | Yes |
| `main` at the current commit | Yes |
| Anything older | No |

## Reporting a vulnerability

Please **do not open a public issue** for a security problem.

Use GitHub's private vulnerability reporting on this repository:
[Report a vulnerability](https://github.com/shaheeranser/watcher/security/advisories/new).
If that form is unavailable, email shaheer.anser@gmail.com with `watcher
security` in the subject.

Include, as far as you can: the version or commit, how you are running Watcher
(stdin, `--file`, `--source`, or Docker Compose), a minimal reproduction, and the
impact you believe it has.

## What to expect

This is a small project maintained by one person, so please set your
expectations accordingly. Acknowledgement of a report is targeted within a week,
and a fix for a confirmed issue as soon as it is practical. You will be credited
in the advisory unless you ask not to be.

## Scope

Watcher deliberately reads untrusted input and holds credentials, so the
following are the areas worth looking at:

- **Log parsing** — anything that reads log text treats it as hostile. A crafted
  log line must not be able to crash the daemon, exhaust memory, or corrupt the
  incident store.
- **Model prompt construction** — log content is untrusted and reaches a local
  model. It must not be able to steer what gets executed or delivered.
- **Webhook delivery** — the webhook URL is a credential. It must not be printed,
  persisted in a spool file, or echoed into a result stream.
- **Installer** — `install.sh` downloads a release artifact and verifies it
  against the published checksum. A bypass of that verification is in scope.
- **Docker socket access** — the container source talks to the Docker socket,
  which is privileged. It must not be reachable from log content.
- **The read API** — it serves a unix socket, which must not be exposed beyond
  the local machine by default.

Out of scope: findings that require an already-compromised host, and the
behaviour of the model itself — Watcher's explanations are advisory text and are
not a security boundary. Prompt injection that only produces a misleading
explanation, without escaping the delivery path, is a quality issue rather than a
vulnerability.
