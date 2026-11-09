# Contributing to Persona

Persona is a self-hosted personal SuperAgent. A Go server, `personad`, runs as the **hub** on an always-on machine and
holds all state; the clients (iPhone, Apple Watch, Mac, Vision Pro, Android and the web) talk only to the hub; coding
Agents run on your own computers, connected through Persona Link; tools, data sources, channels and voice plug in as
extensions. The product overview is [README.md](README.md), and the architecture is in
[docs/server/architecture.md](docs/server/architecture.md).

> **Humans start here.** [CLAUDE.md](CLAUDE.md) is the handbook for the repository's coding Agents: it carries the
> super rules, the mandatory conventions and the full docs index. Human contributors follow this file. Where the two
> appear to differ, [docs/SUPER_RULES.md](docs/SUPER_RULES.md) wins, then `CLAUDE.md`.

## Three ways in

Pick the path that matches your change. Each one lists its entry docs and the portable commands that reproduce what
hosted CI runs.

### 1. Hub only — Go, no client toolchain

The hub and its managers live in `server/`. All server state lives in the Postgres container; new business logic
(endpoints, tables, model calls) goes into the managers, not the hub routes.

- Start with [docs/server/server.md](docs/server/server.md) and [docs/operations/config.md](docs/operations/config.md).
- Toolchain: the Go version named by the `go` directive of `server/go.mod`; Docker is only needed for the full test
  lane, which starts a disposable Postgres 18 container.
- Verify locally:

  ```sh
  scripts/ci/server.sh build   # format, vet, build and Windows cross-build; no Docker needed
  scripts/ci/server.sh         # full lane, including the Go tests against Postgres 18
  ```

### 2. Clients — Apple, Android or web

- **Apple platforms** (`apple/`): one generated Xcode project for iOS, watchOS, macOS and visionOS, with a shared
  design system. Entry: [docs/apple/ios.md](docs/apple/ios.md). Build and install on a simulator with
  `scripts/app/sim.sh`; the portable CI lane is `scripts/ci/apple.sh`.
- **Android** (`android/`): Kotlin + Compose modules with their own design system. Entry:
  [docs/android/android.md](docs/android/android.md). `scripts/app/android.sh` builds, lints, tests and takes
  screenshots; the CI lane is `scripts/ci/android.sh`.
- **Web** (`web/`): Vite + React + Tailwind, including the hub admin console. Entry:
  [docs/web/web.md](docs/web/web.md). With Node `>=22.13.0`:

  ```sh
  scripts/ci/web.sh   # install, lint, typecheck, tests and production build
  ```

UI changes are held to the app-store quality bar: every changed screen, watch included, is checked in a simulator or
screenshot run in both dark and light mode, and the screenshots go on the PR. Compiling is not enough; see
[docs/operations/testing.md](docs/operations/testing.md).

### 3. Extensions and the SDK

This repository ships only generic extension points. Connectors and channels tied to a specific service or
organization live in their own extension packages and are not part of this repository.

- Entry: [docs/extensions/extensions.md](docs/extensions/extensions.md). Manifest schemas live in `schema/`, the Go
  SDK in `sdk/go/`, and conformance tests run as part of `scripts/ci/server.sh`.
- An extension is a manifest plus an optional process program; its permissions are declared explicitly and shown at
  install time.

## Development workflow

People and Agents follow the same flow. The full detail with copyable `gh` commands is
[docs/operations/collaboration.md](docs/operations/collaboration.md).

1. **Issue first.** Search open and closed issues and PRs for duplicates, then open an issue (`bug`, `enhancement` or
   `documentation`) saying what and why, and claim it with a comment before you start work.
2. **Work on a branch named after the issue**, for example `fix/123-short-name`. Bring it up to date by merging
   `origin/main`; never force-push a shared branch.
3. **One PR per issue**, with `Fixes #N`, `Closes #N` or `Refs #N` in the **description** (the title does not count).
   A PR without an issue link is closed automatically and reopened once the link is added.
4. **English in everything people read**: code comments, commit messages, issues, PR titles and bodies. User-facing
   text always goes through the localization resources and ships in English, Simplified Chinese and Japanese together;
   see [docs/protocol/i18n.md](docs/protocol/i18n.md).
5. **Write as if anyone can read it.** This is a public repository: no internal services, host names, URLs, people's
   names, tokens or keys. Examples use placeholders such as `<hub address>` and `me@example.com`.
6. **Run the privacy scan before every commit** (the commit hook runs it as well):

   ```sh
   python3 scripts/privacy_check.py
   ```

   For docs changes, also run `python3 scripts/test/test_doc_paths.py`, which checks every quoted repository path and
   Markdown link.
7. **CI must be green before merge.** Maintainers squash-merge with `scripts/hooks/merge-pr.sh <number>`, which repeats
   the privacy scan with this machine's private values; do not merge by hand.

## Conventions at a glance

- UI uses only the design-system tokens (`apple/Shared/Design`, `android/core/designsystem`): never hard-code colors,
  sizes, fonts or icon names, and never change an existing screen's look in place — add a new style or theme color so
  the defaults stay pixel-identical.
- Prompts are one file per prompt under `server/internal/prompts/`, composed with templates, never built with string
  concatenation.
- Write reusable pitfalls into `docs/lessons/` and design trade-offs into the module doc; one-off plans and acceptance
  reports go to `docs/archive/` when finished.
- The complete rule set — releases, simulators, localization and the docs index — is [CLAUDE.md](CLAUDE.md).
