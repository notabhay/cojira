# cojira

`cojira` is an agent-first CLI for Jira and Confluence.

It is designed to work well for both humans and coding agents:

- safe previews with `cojira plan ...` and `--dry-run`
- structured JSON output for follow-up automation
- human and summary output for fast operator use
- flexible Jira and Confluence identifier resolution
- local install and workspace bootstrap flows
- named profiles from `.cojira.json`
- NDJSON streaming and lightweight client-side projection for agent consumers

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

If you work across multiple tenants, add named profiles to `.cojira.json` and select one with `--profile <name>` or `COJIRA_PROFILE`.

Generate shell completions:

```bash
cojira completion zsh > "${fpath[1]}/_cojira"
cojira completion bash > ~/.local/share/bash-completion/completions/cojira
cojira completion man --dir ./man
```

Inspect or migrate the active credential store:

```bash
cojira auth status
cojira auth migrate --to keyring --set-default
```

## Quick Start

Inspect the current environment:

```bash
cojira describe --with-context --output-mode json
cojira doctor --ci --output-mode json
```

Inspect the shared HTTP cache:

```bash
cojira cache stats
cojira cache inspect --output-mode json
```

Read a Jira issue:

```bash
cojira jira info RAPTOR-3223 --output-mode summary
```

Read a Confluence page:

```bash
cojira confluence info 6573916430 --output-mode summary
cojira confluence get 6573916430 --format markdown --output-mode summary
cojira confluence export 6573916430 --format markdown -o page.md
```

Preview a Jira update safely:

```bash
cojira plan jira update RAPTOR-3223 --set priority=High
```

Record a preview for later apply and tail structured events:

```bash
cojira dry-run record jira transition RAPTOR-3223 --to Done
cojira apply <plan-id> --yes
cojira events tail --latest
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

JQL helpers and enum-friendly field values:

```bash
cojira jira jql build --project RAPTOR --status "In Progress" --unresolved
cojira jira jql validate 'project = RAPTOR AND assignee = currentUser()' --output-mode json
cojira jira jql suggest status --output-mode json
cojira jira customfields resolve "Story Points" --output-mode json
cojira jira field-values RAPTOR-3223 priority --format enum --output-mode json
```

Bulk Jira helpers:

```bash
cojira jira rank RAPTOR-3223 --after RAPTOR-2711 --dry-run
cojira jira epic list --project RAPTOR --limit 10 --output-mode summary
cojira jira epic children RAPTOR-100 --limit 20 --output-mode summary
cojira jira epic add RAPTOR-100 RAPTOR-3223 --dry-run
cojira jira backlog list 43880 --limit 20 --output-mode summary
cojira jira backlog remove RAPTOR-3223 --board 43880 --dry-run
cojira jira backlog move-to 43880 RAPTOR-3223 --after RAPTOR-2711 --dry-run
cojira jira bulk-assign me --jql 'project = RAPTOR AND status = "To Do"' --dry-run
cojira jira bulk-attachment --jql 'project = RAPTOR AND issuekey = RAPTOR-3223' --upload README.md --dry-run
cojira jira bulk-comment --jql 'project = RAPTOR AND labels = triage' --add 'Reviewed by agent' --dry-run
cojira jira bulk-delete --jql 'project = RAPTOR AND status = Closed' --dry-run
cojira jira bulk-label --jql 'project = RAPTOR AND labels = triage' --add reviewed --dry-run
cojira jira bulk-link RAPTOR-3223 --jql 'project = RAPTOR AND labels = blocker' --type Blocks --dry-run
cojira jira bulk-watch me --jql 'project = RAPTOR AND reporter = currentUser()' --dry-run
cojira jira bulk-worklog --jql 'project = RAPTOR AND assignee = currentUser()' --time-spent '30m' --comment 'Triage pass' --dry-run
```

Watch Jira or Confluence for changes:

```bash
cojira jira watch issue RAPTOR-3223 --interval 30s --output-mode ndjson
cojira jira watch jql 'project = RAPTOR AND statusCategory != Done' --interval 60s --output-mode ndjson
cojira confluence watch page 6597705735 --interval 60s --output-mode ndjson
cojira confluence watch cql 'space = CXD AND type = page' --interval 120s --output-mode ndjson
```

Jira Service Management helpers:

```bash
cojira jira jsm desks --output-mode summary
cojira jira jsm queues <DESK-ID> --output-mode summary
cojira jira jsm requests <DESK-ID> --limit 20 --output-mode summary
cojira jira jsm approvals <REQUEST-ID> --output-mode json
cojira jira jsm sla <REQUEST-ID> --output-mode json
```

Attachment helpers:

```bash
printf 'agent note' | cojira jira attachment RAPTOR-3223 --stdin --filename note.txt --dry-run --output-mode json
cojira jira attachment RAPTOR-3223 --download-all --output-dir ./attachments --output-mode summary
cojira jira attachment RAPTOR-3223 --sync-dir ./attachments --dry-run --output-mode json
cojira jira attachment RAPTOR-3223 --sync-dir ./attachments --replace-existing --delete-missing --dry-run
printf 'page note' | cojira confluence attachment 6597705735 --stdin --filename note.txt --dry-run --output-mode json
cojira confluence attachment 6597705735 --download-all --output-dir ./page-files --output-mode summary
cojira confluence attachment 6597705735 --sync-dir ./page-files --dry-run --output-mode json
```

## Output Modes

- `human`: operator-friendly output
- `summary`: short user-facing summaries
- `json`: structured output for chaining
- `ndjson`: one JSON object per line for streaming consumers
- `auto`: `human` on TTY, `json` when piped

Helpful companions:

- `--stream`: shorthand for `--output-mode ndjson`
- `--select result.issues`: lightweight client-side projection for JSON or NDJSON output
- `--profile work`: apply a named profile from `.cojira.json`

## Safety Model

- Use `cojira plan ...` or `--dry-run` before high-blast-radius changes.
- Bulk and batch commands support resumability and idempotency keys.
- Destructive flows require explicit confirmation flags.
- `jira` and `confluence` GET or HEAD requests use a short-lived shared HTTP cache by default.
- Use `--no-cache` to bypass cache reads and `--cache-ttl` to tune cache freshness.
- Successful write requests invalidate the shared cache for the active auth context.

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
- `rank`
- `epic`
- `backlog`
- `customfields`
- `query`
- `jql`
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
- `bulk-assign`
- `bulk-attachment`
- `bulk-comment`
- `bulk-delete`
- `bulk-label`
- `bulk-link`
- `bulk-watch`
- `bulk-worklog`
- `bulk-update`
- `bulk-transition`
- `bulk-update-summaries`
- `sync`
- `sync-from-dir`

Operator helpers:

- `cojira cache inspect|stats|clear`
- `cojira completion`
- `cojira doctor --ci --output-mode json`
- `cojira jira current`
- `cojira jira branch`
- `cojira jira template list`
- `cojira jira jql build|validate|suggest`
- `cojira jira customfields map|resolve`
- `cojira jira field-values <ISSUE> <FIELD>`
- `cojira jira poll issue <ISSUE>`
- `cojira jira offline search <TEXT>`

## Confluence Highlights

Read and inspect:

- `info`
- `get`
- `export`
- `find`
- `history`
- `diff`
- `spaces`
- `labels`
- `tree`
- `attachment`
- `comment`
- `inline-comment`
- `page-properties`
- `templates`
- `trash`
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
- `inline-comment`
- `macros`
- `page-properties`
- `templates`
- `trash restore`
- `blog create`
- `blog update`
- `blog delete`
- `batch`

Examples:

```bash
cojira confluence inline-comment list 6597705735 --output-mode summary
cojira confluence macros render info --title "Heads up" --body "Agent-authored panel"
cojira confluence page-properties report --space CXD --label report --output-mode json
cojira confluence templates list --space CXD --output-mode summary
cojira confluence trash list --space CXD --output-mode summary
```

## Markdown Support

`cojira` can convert Markdown into Confluence storage XHTML, Jira wiki text, or Jira ADF JSON.

Supported surfaces include:

- `cojira convert`
- `cojira jira comment --format markdown`
- `cojira confluence comment --format markdown`
- `cojira confluence get --format markdown`
- `cojira confluence export --format markdown`
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

Credential storage knobs:

- `COJIRA_CRED_STORE=plain` to use `${XDG_CONFIG_HOME:-$HOME/.config}/cojira/credentials`
- `COJIRA_CRED_STORE=keyring` to load from the OS keychain via `cojira auth migrate`

## Release And Publication

Release builds now include:

- `deb` and `rpm` packages from GoReleaser nfpm config
- keyless Cosign signatures for `checksums.txt`
- SPDX SBOM generation
- GitHub artifact attestations for built release assets

The repo also includes publication scaffolding for ecosystems that still require an external registry submission:

- `Formula/cojira.rb` for Homebrew
- `packaging/` manifest templates for Winget, Scoop, and Chocolatey
- `scripts/render_release_manifests.sh` to render those manifests for a tagged release

Example:

```bash
./scripts/render_release_manifests.sh --version v0.4.2 --windows-amd64-sha256 <sha256>
```

That generates ready-to-review manifests under `packaging/generated/<version>/`. Publishing them to a real Homebrew tap, Winget, Scoop bucket, or Chocolatey feed is still an external release step.

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
