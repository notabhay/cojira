# cojira

`cojira` is an agent-first CLI for Jira and Confluence.

It is designed to work well for both humans and coding agents:

- safe previews with `cojira plan ...` and `--dry-run`
- structured JSON output for follow-up automation
- human and summary output for fast operator use
- flexible Jira and Confluence identifier resolution
- local install and workspace bootstrap flows

## Install

If this repo or release bundle already contains a packaged binary, use the local installer:

```bash
./install.sh
```

On Windows PowerShell:

```powershell
powershell -ExecutionPolicy Bypass -File .\install.ps1
```

If you are building from source:

```bash
go build -o ~/.local/bin/cojira .
```

Or install directly with Go:

```bash
go install github.com/notabhay/cojira@latest
```

Or install from the bundled Homebrew formula in this repo:

```bash
brew install --HEAD ./Formula/cojira.rb
```

Or run the published container image:

```bash
docker run --rm \
  --env-file .env \
  -v "$PWD:/workspace" \
  -w /workspace \
  ghcr.io/notabhay/cojira:latest describe --with-context --output-mode json
```

Release archives include the platform binary, `install.sh`, `install.ps1`, `.env.example`, and `COJIRA-BOOTSTRAP.md`.

## Setup

1. Copy `.env.example` to `.env`.
2. Fill in your Jira and Confluence credentials locally.
3. Run:

```bash
cojira doctor
cojira describe --with-context --output-mode json
```

`cojira` also supports a global credentials file at `~/.config/cojira/credentials`.

Generate shell completions:

```bash
cojira completion zsh > "${fpath[1]}/_cojira"
cojira completion bash > ~/.local/share/bash-completion/completions/cojira
```

## Quick Start

Inspect the current environment:

```bash
cojira describe --with-context --output-mode json
```

Read a Jira issue:

```bash
cojira jira info RAPTOR-3223 --output-mode summary
```

Read a Confluence page:

```bash
cojira confluence info 6573916430 --output-mode summary
```

Preview a Jira update safely:

```bash
cojira plan jira update RAPTOR-3223 --set priority=High
```

Render a board in the terminal:

```bash
cojira jira board-view 18612 --all
```

Convert Markdown for Jira or Confluence:

```bash
cojira convert --from markdown --to jira-wiki -f note.md
cojira convert --from markdown --to jira-adf -f note.md
cojira convert --from markdown --to confluence-storage -f page.md
```

Create a branch from an issue and inspect saved queries:

```bash
cojira jira branch RAPTOR-3223 --plan
cojira jira query list
cojira jira mine --output-mode summary
```

Use guided prompts for common Jira actions:

```bash
cojira jira create --interactive --dry-run
cojira jira transition RAPTOR-3223 --interactive --dry-run
```

Sprint analytics:

```bash
cojira jira report burndown 18612 --output-mode summary
cojira jira report cycle-time 18612 --output-mode summary
cojira jira report blocker-aging 18612 --output-mode summary
cojira jira report workload 18612 --output-mode summary
```

## Output Modes

- `human`: operator-friendly output
- `summary`: short user-facing summaries
- `json`: structured output for chaining
- `auto`: `human` on TTY, `json` when piped

## Safety Model

- Use `cojira plan ...` or `--dry-run` before high-blast-radius changes.
- Bulk and batch commands support resumability and idempotency keys.
- Destructive flows require explicit confirmation flags.
- `jira` and `confluence` GET or HEAD requests use a short-lived shared HTTP cache by default.
- Use `--no-cache` to bypass cache reads and `--cache-ttl` to tune cache freshness.

## Jira Highlights

Read and inspect:

- `info`
- `get`
- `dashboard`
- `dashboards`
- `search`
- `fields`
- `projects`
- `users`
- `history`
- `diff`
- `boards`
- `board-view`
- `board-issues`
- `report`
- `report burndown`
- `report cycle-time`
- `report blocker-aging`
- `report workload`
- `graph`
- `blocked`
- `critical-path`
- `query`
- `mine`
- `recent`
- `template`
- `field-values`
- `poll`
- `offline`
- `undo list`

Write and automate:

- `current`
- `branch`
- `commit-template`
- `pr-title`
- `finish-branch`
- `create`
- `clone`
- `assign`
- `comment`
- `attachment`
- `link`
- `watchers`
- `worklog`
- `update`
- `transition`
- `undo apply`
- `sprint`
- `batch`
- `bulk-update`
- `bulk-transition`
- `bulk-update-summaries`
- `sync`
- `sync-from-dir`

Operator helpers:

- `cojira completion`
- `cojira jira current`
- `cojira jira branch`
- `cojira jira template list`
- `cojira jira field-values <ISSUE> <FIELD>`
- `cojira jira poll issue <ISSUE>`
- `cojira jira offline search <TEXT>`

## Confluence Highlights

Read and inspect:

- `info`
- `get`
- `find`
- `history`
- `diff`
- `spaces`
- `labels`
- `tree`
- `attachment`
- `comment`
- `blog list`

Write and automate:

- `create`
- `update`
- `rename`
- `move`
- `archive`
- `copy-tree`
- `restore`
- `restrictions`
- `attachment`
- `comment`
- `blog create`
- `blog update`
- `blog delete`
- `batch`

## Markdown Support

`cojira` can convert Markdown into Confluence storage XHTML, Jira wiki text, or Jira ADF JSON.

Supported surfaces include:

- `cojira convert`
- `cojira jira comment --format markdown`
- `cojira confluence comment --format markdown`
- `cojira confluence create --format markdown`
- `cojira confluence update --format markdown`
- `cojira confluence blog create --format markdown`
- `cojira confluence blog update --format markdown`

Use `storage` or `raw` formats when you already have exact Confluence XHTML or Jira-native markup.

## OAuth And API Versions

`cojira` now supports Atlassian OAuth 2.0 token flows through env-backed access tokens or refresh-token refresh.
Refreshed OAuth tokens are persisted back to the configured credentials file so later commands can reuse the rotated access or refresh token.

Useful env knobs include:

- `JIRA_AUTH_MODE=oauth2`
- `JIRA_OAUTH_ACCESS_TOKEN`, `JIRA_OAUTH_REFRESH_TOKEN`, `JIRA_OAUTH_CLIENT_ID`, `JIRA_OAUTH_CLIENT_SECRET`
- `CONFLUENCE_AUTH_MODE=oauth2`
- `CONFLUENCE_OAUTH_ACCESS_TOKEN`, `CONFLUENCE_OAUTH_REFRESH_TOKEN`, `CONFLUENCE_OAUTH_CLIENT_ID`, `CONFLUENCE_OAUTH_CLIENT_SECRET`
- `JIRA_API_VERSION=3` to send Jira descriptions and comments as ADF
- `CONFLUENCE_API_VERSION=2` to use the Confluence v2 page CRUD path on Cloud

## Development

Run the full test suite:

```bash
go test ./...
```

Inspect the current command surface:

```bash
cojira --help
cojira jira --help
cojira confluence --help
```
