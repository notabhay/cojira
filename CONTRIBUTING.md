# Contributing

`cojira` is small enough that structural discipline matters.
It is not trying to be every Atlassian integration under the sun; it is trying to be a dependable operational CLI that other agents can trust when the cost of a wrong mutation is real.

## Working Principles

- Keep the command surface explicit.
- Keep mutations previewable.
- Preserve Confluence fidelity.
- Keep the dependency footprint lean.
- Treat output contracts, error codes, and diagnostics as product surface, not polish.
- Make multi-item operations resumable instead of best-effort replay.

## Prerequisites

- Go 1.22 or newer
- `git`
- optional: `golangci-lint` if you want the same lint behavior as CI
- Jira and Confluence credentials only when you need live end-to-end checks

## Repository Layout and Package Responsibilities

| Path | Responsibility |
| --- | --- |
| [`main.go`](main.go) | Root command assembly, dotenv bootstrap, top-level registration, and exit handling |
| [`install.sh`](install.sh) | Curl installer that downloads source, ensures Go exists, builds the binary, and emits bootstrap assets |
| [`internal/cli`](internal/cli) | Shared Cobra helpers, root alias expansion, output-mode normalization, retry flags, idempotency flags |
| [`internal/meta`](internal/meta) | Meta commands: `describe`, `doctor`, `init`, `bootstrap`, `plan`, `do` |
| [`internal/jira`](internal/jira) | Jira client, identifier handling, JQL helpers, command handlers, and sync flows |
| [`internal/confluence`](internal/confluence) | Confluence client, identifier handling, XHTML validation, page/tree flows, and batch operations |
| [`internal/board`](internal/board) | Experimental board-configuration features built on GreenHopper endpoints |
| [`internal/dotenv`](internal/dotenv) | `.env` parsing, merged credential loading, provenance, placeholder detection |
| [`internal/config`](internal/config) | `.cojira.json` parsing, aliases, and project defaults |
| [`internal/httpclient`](internal/httpclient) | Shared retry and backoff behavior |
| [`internal/output`](internal/output) | Structured envelopes, output modes, and related helpers |
| [`internal/errors`](internal/errors) | Error codes, messages, hints, and recovery metadata |
| [`internal/idempotency`](internal/idempotency) | Local idempotency store, checkpoints, and resumable-state helpers |
| [`internal/assets`](internal/assets) | Embedded bootstrap guide, env template, and example payloads used by `cojira bootstrap` |
| [`.github/workflows/ci.yml`](.github/workflows/ci.yml) | GitHub Actions vet, race test, build, and lint workflow |
| [`.goreleaser.yml`](.goreleaser.yml) | Release packaging and cross-platform build definition |

## Build and Test

Primary loop:

```bash
go build -o cojira .
go test ./...
go vet ./...
go test -race ./...
```

Make wrappers:

```bash
make build
make test
make vet
make test-race
make lint
```

Quick smoke checks after structural changes:

```bash
./cojira --help
./cojira describe --output-mode json
./cojira jira --help
./cojira confluence --help
```

## Architecture Notes Worth Preserving

### Thin root, real work in packages

`main.go` should stay thin.
It wires the command tree together, loads environment/config, and delegates real behavior into internal packages.

### Output envelope is an external contract

If you change envelope fields or semantics, you are changing agent-facing API behavior.
Be explicit and test it.

### Bootstrap docs are product surface

The bootstrap markdown is not ancillary documentation.
It is how other coding agents learn how to use `cojira` from a clean workspace.

### Resumable mutation state matters

For multi-item mutation flows, a safe retry story is more important than a clever happy path.
If a command can partially succeed, it should preserve enough information for an agent to resume safely without replaying completed work.

### Experimental board commands are deliberately separate

Board configuration commands use internal Jira APIs and should stay clearly marked as experimental in help text, docs, and user expectations.

## Bootstrap and Agent-Doc Synchronization

There are three markdown files that must stay aligned:

- [`AGENTS.md`](AGENTS.md): canonical repo source
- [`COJIRA-BOOTSTRAP.md`](COJIRA-BOOTSTRAP.md): exported copy used in clean workspaces
- [`internal/assets/COJIRA-BOOTSTRAP.md`](internal/assets/COJIRA-BOOTSTRAP.md): embedded runtime copy used by `cojira bootstrap`

There is also one symlink:

- [`CLAUDE.md`](CLAUDE.md) -> `AGENTS.md`

Rules:

- edit `AGENTS.md` first,
- copy it to `COJIRA-BOOTSTRAP.md`,
- copy that file to `internal/assets/COJIRA-BOOTSTRAP.md`,
- never hand-edit `CLAUDE.md`; recreate the symlink instead,
- verify the root bootstrap file and embedded bootstrap file are byte-for-byte identical before you commit.

Related assets:

- [`.env.example`](.env.example): checked-in repo template
- [`internal/assets/env.example`](internal/assets/env.example): template emitted by `cojira bootstrap`

Keep those aligned in intent and defaults.

## How To Add or Change a Command

### 1. Put it in the right package

- Jira behavior belongs in `internal/jira`
- Confluence behavior belongs in `internal/confluence`
- setup, planning, diagnostics, and agent-helper flows belong in `internal/meta`
- GreenHopper-based board behavior belongs in `internal/board`

### 2. Follow the existing command shape

Usual pattern:

- `New...Cmd()` returns a `*cobra.Command`
- environment and flags are normalized through shared helpers
- structured output goes through `internal/output`
- structured failures go through `internal/errors`

Common helpers:

- `cli.AddOutputFlags(...)`
- `cli.AddHTTPRetryFlags(...)`
- `cli.AddIdempotencyFlags(...)`
- `cli.NormalizeOutputMode(...)`
- `output.BuildEnvelope(...)`

### 3. Register it explicitly

- Jira and Confluence subcommands are registered in their package-level command constructors
- top-level meta commands are registered from `main.go`
- board commands hang off the Jira tree rather than a separate root tool

### 4. Preserve product conventions

- prefer flexible identifiers over forcing users to normalize inputs first
- prefer preview or validation surfaces on mutation commands
- keep Confluence storage-format fidelity intact
- keep raw API surfaces narrow and allowlisted
- mark unstable/internal API features as experimental in both docs and help text

### 5. Test the real behavior

Prefer narrow regression tests that prove the actual contract:

- parsing edge cases
- output-mode differences
- root registration and smoke wiring
- resumable partial-failure behavior
- identifier resolution
- error and warning envelopes

### 6. Update docs in the same change

A command change is incomplete until you update:

- [`README.md`](README.md) when the top-level capability surface changed
- [`AGENTS.md`](AGENTS.md)
- [`COJIRA-BOOTSTRAP.md`](COJIRA-BOOTSTRAP.md)
- [`internal/assets/COJIRA-BOOTSTRAP.md`](internal/assets/COJIRA-BOOTSTRAP.md)
- any examples or templates that changed

## Coding Conventions

- Prefer ASCII unless the file already needs something else.
- Reach for the standard library before new dependencies.
- Add comments where behavior is not self-evident, but do not narrate obvious code.
- Keep error messages actionable and specific.
- Preserve structured recovery metadata when returning user-facing errors.
- Avoid silent fallback on destructive or lossy behavior.
- When a mutation partially succeeds, emit enough state for safe resume.

## Release and Installer Notes

- `install.sh` is a first-class distribution surface, not an internal convenience script.
- The beta-branch curl install path should keep working without additional environment variables.
- If you change installer defaults, bootstrap output paths, or embedded assets, treat that as a user-facing change and document it.

## Before You Commit

- run `go test ./...`
- run `go vet ./...`
- run `go test -race ./...`
- verify `AGENTS.md`, `COJIRA-BOOTSTRAP.md`, and `internal/assets/COJIRA-BOOTSTRAP.md` are synchronized
- verify `CLAUDE.md` is a symlink to `AGENTS.md`
- verify there are no stale organization-specific URLs or remotes left in tracked files
