# cojira

`cojira` is an agent-first CLI for Jira and Confluence work. It is built for the workflow where a human gives plain-language intent, an automation agent does the operational work, and the tool itself provides safe mutation surfaces, structured machine-readable output, and enough previewability to keep Atlassian changes boring.

It is useful for:
- Coding agents that need a stable command surface instead of hand-rolled REST calls.
- Engineers and operators who want one CLI for Jira issue work and Confluence page work.
- Teams that care about dry runs, predictable JSON output, and low-dependency binaries.

## Why cojira exists

Most Atlassian tooling is optimized either for humans clicking through UIs or for engineers building raw API integrations. `cojira` sits in the middle:
- It gives agents a consistent command tree with structured envelopes, typed error codes, and summary output modes.
- It keeps destructive work previewable with `--dry-run`, `cojira plan ...`, and explicit validation commands.
- It accepts flexible identifiers, so a user can paste a Jira key, page URL, tiny link, board URL, or page title instead of pre-normalizing inputs.
- It stays intentionally lean: Cobra plus a small supporting set of dependencies, with most behavior implemented in the standard library.

## What It Can Do

### Meta workflow

| Area | What you get |
| --- | --- |
| Capability discovery | `describe`, including live context checks with `--with-context` |
| Setup and diagnostics | `init`, `doctor`, and bootstrap asset generation |
| Safe previews | `plan <tool> <command> ...` to force preview-first execution |
| Natural-language dispatch | `do <intent>` for phrasebook-driven intent parsing |

### Jira workflow

| Area | Commands |
| --- | --- |
| Read and inspect | `info`, `get`, `search`, `fields`, `whoami`, `board-issues` |
| Write and mutate | `create`, `update`, `transition`, `delete` |
| Bulk operations | `batch`, `bulk-update`, `bulk-transition`, `bulk-update-summaries` |
| Disk sync | `sync`, `sync-from-dir` |
| Narrow raw access | `raw` on allowlisted Jira REST paths |
| Board configuration | Experimental `board-detail-view` and `board-swimlanes` commands |

### Confluence workflow

| Area | Commands |
| --- | --- |
| Read and inspect | `info`, `get`, `view`, `find`, `tree`, `comments` |
| Write and mutate | `create`, `update`, `rename`, `move`, `archive`, `copy-tree` |
| Validation and automation | `validate`, `batch`, read-only `raw` |

## Safety Model

`cojira` is opinionated about safe automation:
- Mutating flows commonly expose `--dry-run`, and `cojira plan ...` can force preview mode from the outside.
- Confluence editing is designed around storage-format XHTML. The tool assumes you preserve `<ac:...>` and `<ri:...>` macros instead of converting pages through Markdown.
- Output modes are explicit: `human`, `summary`, `json`, and `auto`.
- JSON responses use a consistent envelope with `ok`, `result`, `warnings`, `errors`, and `exit_code`.
- Idempotency helpers exist for mutation commands so agents can avoid accidental replays when retrying.

## Install

### Fastest install from the current `beta` branch

```bash
curl -fsSL https://raw.githubusercontent.com/notabhay/cojira/beta/install.sh | COJIRA_REF=refs/heads/beta bash
```

That installer:
- Downloads a source archive for the requested ref.
- Ensures a usable Go toolchain exists locally.
- Builds `cojira` into `${GOBIN:-$HOME/.local/bin}/cojira`.
- Writes the bootstrap guide to `/tmp/cojira/COJIRA-BOOTSTRAP.md`.

### Source build

Requires Go 1.22 or newer:

```bash
go build -o cojira .
```

If you prefer Make targets:

```bash
make build
make test
make test-race
make vet
```

## Quickstart

### For humans working in this repo

1. Install `cojira`.
2. Configure credentials with `cojira init` or by creating a `.env`.
3. Run `cojira doctor`.
4. Use `cojira describe --with-context --output-mode json` to inspect the live environment and command surface.

### For coding agents

If you want a single copy-paste bootstrap prompt for another agent session, use:

```bash
curl -fsSL https://raw.githubusercontent.com/notabhay/cojira/beta/install.sh | COJIRA_REF=refs/heads/beta bash && git clone --branch beta https://github.com/notabhay/cojira.git /tmp/cojira 2>/dev/null || git -C /tmp/cojira pull && follow /tmp/cojira/COJIRA-BOOTSTRAP.md
```

The authoritative setup and agent-usage document is [`COJIRA-BOOTSTRAP.md`](COJIRA-BOOTSTRAP.md).

## Common Examples

```bash
# Diagnose setup
cojira doctor

# Describe the live command surface for an agent
cojira describe --with-context --output-mode json

# Show a Jira issue
cojira jira info PROJ-123 --output-mode summary

# Preview a Jira update
cojira jira update PROJ-123 --set labels+=urgent --dry-run

# Fetch a Confluence page body
cojira confluence get 12345 -o page.html

# Preview a Confluence tree copy
cojira confluence copy-tree 12345 67890 --dry-run
```

## Experimental Board Commands

The Jira board-detail-view and board-swimlanes commands are marked experimental because they use internal GreenHopper endpoints rather than the public Jira REST surface. Treat them as powerful but brittle:
- Expect them to require board-admin permissions.
- Expect breakage across Jira upgrades.
- Prefer export, review, and `--dry-run` before apply-style mutations.

## Documentation Map

Start here, in this order:
- [`COJIRA-BOOTSTRAP.md`](COJIRA-BOOTSTRAP.md): canonical setup, credentials, safety rules, full command surface, phrasebook, and recovery guidance for agents.
- [`CONTRIBUTING.md`](CONTRIBUTING.md): repo layout, architecture, build/test workflow, and how to add or change commands safely.
- [`AGENTS.md`](AGENTS.md): repo-scoped agent instructions currently checked into the repo.
- [`CLAUDE.md`](CLAUDE.md): companion agent instructions for Claude-oriented workflows.

## Repository Layout

At a high level:
- [`main.go`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/main.go) wires the root command and top-level subcommand registration.
- [`internal/jira`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/jira) contains Jira client code and command handlers.
- [`internal/confluence`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/confluence) contains Confluence client code and command handlers.
- [`internal/meta`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/meta) contains setup, diagnostics, bootstrap, plan, describe, and `do`.
- [`internal/assets`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/assets) contains the embedded bootstrap docs and example templates used by `cojira bootstrap`.

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md) for the real development guide. It covers:
- package boundaries and responsibilities,
- how bootstrap docs and embedded assets flow through the binary,
- the local validation loop,
- current CI and release wiring,
- and the conventions to follow when adding a new command or mutation workflow.
