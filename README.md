# cojira

`cojira` is an agent-first CLI for Jira and Confluence work.
It is built for the operating mode where a human gives intent in plain language, an automation agent does the work, and the tool itself provides stable commands, previewable mutations, structured output, and resumable retries.

## Who It Is For

- coding agents that should not hand-roll Atlassian REST calls,
- engineers and operators who want one CLI for both Jira and Confluence,
- teams that care about `--dry-run`, predictable JSON output, low dependency count, and safe retries.

## Why It Exists

Most Atlassian tooling is optimized either for browser clicks or for bespoke integrations.
`cojira` sits in the middle:

- it gives agents a stable command tree instead of ad hoc HTTP payload construction,
- it keeps destructive work previewable with `--dry-run`, `cojira plan ...`, and validation commands,
- it accepts flexible identifiers, so users can paste issue keys, page URLs, board URLs, page ids, or `SPACE:"Title"` forms,
- it exposes structured envelopes and stable error codes so another tool can reason about failures,
- it now emits resumable partial-failure state for the highest-risk multi-item mutation flows.

## Install

### Canonical agent bootstrap prompt

For another coding agent, the canonical prompt is:

> hey, run `curl -fsSL https://cojira.notabhay.xyz/install.sh | bash` and then follow the instructions in `/tmp/cojira/COJIRA-BOOTSTRAP.md`

That flow:

- installs from the stable custom-domain endpoint,
- uses the `beta`-branch installer script, which resolves the latest tagged release by default,
- writes `/tmp/cojira/COJIRA-BOOTSTRAP.md`,
- then uses the bootstrap guide as the only required source of truth.

If you just want the shell command itself:

```bash
curl -fsSL https://cojira.notabhay.xyz/install.sh | bash
```

### Source build

Requires Go 1.22 or newer:

```bash
go build -o "${GOBIN:-$HOME/.local/bin}/cojira" .
```

## Quickstart

### Human setup

1. Install `cojira`.
2. Run `cojira init` or write `.env` / global credentials manually.
3. Run `cojira doctor`.
4. Run `cojira describe --with-context --output-mode json` to inspect the live capability surface.

### Agent setup

Use the canonical one-line prompt above, then treat [`COJIRA-BOOTSTRAP.md`](COJIRA-BOOTSTRAP.md) as the complete operating guide.

## Safety and Recovery Model

`cojira` is opinionated about safe automation:

- mutating flows expose `--dry-run` or can be wrapped with `cojira plan ...`,
- Confluence page editing is storage-format XHTML first; the tool assumes you preserve macros rather than converting through Markdown,
- multi-item mutation flows now emit machine-readable `resumable_state` on partial failure,
- rerunning the same command with the emitted `--idempotency-key` resumes from the frozen operation snapshot instead of replaying succeeded items,
- output modes are explicit, JSON output is designed for chaining, and supported Jira mutation commands can emit scalar keys directly.

The resumable partial-failure contract currently covers:

- `cojira jira batch`
- `cojira jira bulk-update`
- `cojira jira bulk-transition`
- `cojira jira bulk-update-summaries`
- `cojira confluence batch`
- `cojira confluence copy-tree`

## Command Reference

### Top-level commands

| Command | Purpose |
| --- | --- |
| `bootstrap` | Write `COJIRA-BOOTSTRAP.md`, `.env.example`, and example templates into a directory |
| `completion` | Generate shell completion |
| `confluence` | Confluence page management |
| `describe` | Machine-readable capability report for agents |
| `do` | Parse natural-language intent into a concrete command |
| `doctor` | Diagnose setup and connectivity |
| `init` | Interactive setup wizard |
| `jira` | Jira issue and board automation |
| `plan` | Preview a command without applying it |

### Jira commands

| Command | Purpose |
| --- | --- |
| `batch` | Run batch Jira operations |
| `board-detail-view` | Experimental Issue Detail View management |
| `board-issues` | List issues on a board |
| `board-swimlanes` | Experimental swimlane management |
| `bulk-transition` | Transition many issues matched by JQL |
| `bulk-update` | Apply one payload to many issues |
| `bulk-update-summaries` | Rename many issues from CSV or JSON |
| `clone` | Create a new issue by cloning an existing one |
| `create` | Create an issue from JSON, quick flags, templates, or a cloned source issue |
| `development` | Experimental Jira Development-tab data reads |
| `delete` | Delete an issue |
| `fields` | Search Jira fields |
| `get` | Fetch full issue JSON |
| `info` | Show issue metadata, optionally with development summary |
| `raw` | Send an allowlisted Jira REST request |
| `raw-internal` | Experimental Jira internal/API-adjacent passthrough |
| `search` | Search issues using JQL |
| `sync` | Sync issues to local folders |
| `sync-from-dir` | Apply updates from local ticket folders |
| `transition` | Transition one issue |
| `transitions` | List available transitions |
| `update` | Update issue fields |
| `validate` | Validate Jira JSON payloads |
| `whoami` | Show the current Jira identity |

### Confluence commands

| Command | Purpose |
| --- | --- |
| `archive` | Archive a page under another parent |
| `batch` | Run batch Confluence operations |
| `comments` | List page comments |
| `copy-tree` | Copy a page tree under a new parent |
| `create` | Create a page from XHTML |
| `find` | Search by title or CQL |
| `get` | Download storage-format XHTML |
| `info` | Show page metadata |
| `move` | Move a page to a new parent |
| `raw` | Send a read-only Confluence REST request |
| `rename` | Rename a page |
| `tree` | Show page hierarchy |
| `update` | Update a page from XHTML |
| `validate` | Validate storage-format XHTML |
| `view` | Fetch rendered HTML, text, or markdown for reading |

## Representative Examples

```bash
# Diagnose setup
cojira doctor

# Describe the live command surface for an agent
cojira describe --with-context --output-mode json

# Read a Jira issue
cojira jira info PROJ-123 --output-mode summary

# Read issue metadata plus development summary
cojira jira info PROJ-123 --with-development --output-mode json

# Quick-create a Jira issue
cojira jira create --project PROJ --type Task --summary "Investigate login bug" --dry-run

# Read detailed Jira development data
cojira jira --experimental development summary PROJ-123 --output-mode json

# Read Confluence rendered text
cojira confluence view 12345 --format text --output-mode json

# Preview a Jira update
cojira jira update PROJ-123 --set labels+=urgent --dry-run

# Fetch and validate a Confluence page body
cojira confluence get 12345 -o page.html
cojira confluence validate page.html

# Preview a Confluence copy-tree
cojira confluence copy-tree 12345 67890 --dry-run
```

## Experimental Board Commands

`board-detail-view` and `board-swimlanes` use internal Jira GreenHopper endpoints rather than the public REST surface.
Treat them as powerful but brittle:

- expect them to require elevated board permissions,
- expect them to break across Jira upgrades,
- prefer export, review, validation, and `--dry-run` before apply-style changes.

## Documentation Map

- [`AGENTS.md`](AGENTS.md): canonical agent guide in the repo.
- [`CLAUDE.md`](CLAUDE.md): symlink to `AGENTS.md` for tools that look for that filename.
- [`COJIRA-BOOTSTRAP.md`](COJIRA-BOOTSTRAP.md): export-ready copy of the same agent guide for clean workspaces.
- [`CONTRIBUTING.md`](CONTRIBUTING.md): architecture, repo layout, build/test workflow, command-registration patterns, and doc-sync rules.

## Repository Layout

- [`main.go`](main.go): root command assembly and top-level registration.
- [`install.sh`](install.sh): curl-friendly installer that builds `cojira` and emits bootstrap assets.
- [`internal/cli`](internal/cli): shared Cobra helpers, output normalization, idempotency flags, and root behavior.
- [`internal/meta`](internal/meta): `describe`, `doctor`, `init`, `bootstrap`, `plan`, and `do`.
- [`internal/jira`](internal/jira): Jira client, parsing, identifier handling, and Jira command handlers.
- [`internal/confluence`](internal/confluence): Confluence client, identifier handling, XHTML-centric workflows, and Confluence command handlers.
- [`internal/board`](internal/board): experimental Jira board configuration flows.
- [`internal/assets`](internal/assets): embedded bootstrap markdown, env template, and example payload files.

## Development

The normal local loop is:

```bash
go test ./...
go vet ./...
go test -race ./...
```

`make build`, `make test`, `make test-race`, and `make vet` are thin wrappers around the same checks.

For architecture, extension guidance, and documentation maintenance rules, see [`CONTRIBUTING.md`](CONTRIBUTING.md).
