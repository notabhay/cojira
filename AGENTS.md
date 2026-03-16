# Agent Instructions (cojira)

This is the canonical agent guide for `cojira`.

Inside the repo, this file is the source of truth.
Outside the repo, `COJIRA-BOOTSTRAP.md` and `internal/assets/COJIRA-BOOTSTRAP.md` must contain the same guidance so a coding agent can bootstrap from a clean workspace without reading anything else.

If you already have `cojira` installed, regenerate this guide plus the example templates with:

```bash
cojira bootstrap --output /tmp/cojira/COJIRA-BOOTSTRAP.md --force
```

## Canonical setup prompt

For a clean agent session, use this exact one-line prompt shape:

```bash
curl -fsSL https://raw.githubusercontent.com/notabhay/cojira/beta/install.sh | bash && git clone --branch beta https://github.com/notabhay/cojira.git /tmp/cojira 2>/dev/null || git -C /tmp/cojira pull && follow /tmp/cojira/COJIRA-BOOTSTRAP.md
```

## What cojira is for

`cojira` is an agent-first CLI for Jira and Confluence work.
It exists for workflows where:

- a human gives plain-language intent,
- an agent performs the operational work,
- and the tool provides structured output, safe previews, stable identifiers, and resumable mutation flows.

## First call

If you have not used `cojira` in this session yet, start with:

```bash
cojira describe --with-context --output-mode json
```

If `setup_needed` is `true`, either:

- run `cojira init` for an interactive setup flow, or
- write credentials manually using the `.env` format documented below.

## Installation and bootstrap behavior

### Curl install

The canonical prompt above uses the beta-branch installer.
By default it:

- downloads source for the `beta` branch,
- ensures a local Go toolchain is available if `go` is missing,
- builds `cojira` into `${COJIRA_INSTALL_DIR:-${GOBIN:-$HOME/.local/bin}}/cojira`,
- writes `COJIRA-BOOTSTRAP.md` to `/tmp/cojira/COJIRA-BOOTSTRAP.md`.

### Optional installer overrides

These are advanced overrides for the installer itself:

- `COJIRA_VERSION`: version label to embed in the built binary.
- `COJIRA_REF`: Git ref to download instead of the default `refs/heads/beta`.
- `COJIRA_GITHUB_REPO`: alternate `owner/repo`.
- `COJIRA_INSTALL_DIR`: install destination for the binary.
- `COJIRA_BOOTSTRAP_OUT`: where the installer should write the bootstrap markdown.
- `COJIRA_GO_VERSION`: Go version to auto-download if `go` is unavailable.
- `COJIRA_GO_BASE_URL`: alternate base URL for Go downloads.
- `COJIRA_GO_INSTALL_ROOT`: alternate location for the downloaded Go toolchain.

### Source build

If you already have the repo and Go 1.22+:

```bash
go build -o "${GOBIN:-$HOME/.local/bin}/cojira" .
```

### Verify

```bash
cojira --version
cojira --help
cojira doctor
```

## Credentials and environment

### Where credentials can live

`cojira` merges credentials in this order:

1. inherited shell environment variables win,
2. `./.env` fills any missing keys,
3. `${XDG_CONFIG_HOME:-$HOME/.config}/cojira/credentials` fills any remaining missing keys.

### Required environment variables

- `CONFLUENCE_BASE_URL`: Confluence base URL.
- `CONFLUENCE_API_TOKEN`: Confluence Personal Access Token.
- `JIRA_BASE_URL`: Jira base URL. Include any context path such as `/jira` if your instance uses one.
- `JIRA_API_TOKEN`: Jira Personal Access Token or API token.

### Optional environment variables

- `JIRA_EMAIL`: enable basic auth with email + token instead of bearer/PAT mode.
- `JIRA_PROJECT`: default project for `jira sync` and related project-default behavior.
- `JIRA_API_VERSION`: Jira REST API version override. Default is `2`.
- `JIRA_AUTH_MODE`: force `basic` or `bearer`.
- `JIRA_VERIFY_SSL`: set to `false` only when you intentionally need insecure TLS.
- `JIRA_USER_AGENT`: custom HTTP user agent.
- `COJIRA_OUTPUT_MODE`: default output mode when a command does not set one explicitly.
- `COJIRA_IDEMPOTENCY_DIR`: override the local idempotency/resume checkpoint directory.
- `XDG_CONFIG_HOME`: changes the global credentials location.
- `XDG_CACHE_HOME`: changes the default idempotency cache location.

### Interactive setup

For a guided setup:

```bash
cojira init
```

To write the global credentials file directly:

```bash
mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}/cojira"
cojira init --path "${XDG_CONFIG_HOME:-$HOME/.config}/cojira/credentials"
```

### Manual `.env` format

Use this exact shape when writing credentials manually:

```dotenv
# Confluence
CONFLUENCE_BASE_URL=https://confluence.example.com/confluence/
CONFLUENCE_API_TOKEN=your-confluence-token

# Jira
# Include the context path if your Jira uses one.
JIRA_BASE_URL=https://jira.example.com/jira
JIRA_API_TOKEN=your-jira-token

# Optional Jira auth and behavior
# JIRA_EMAIL=you@example.com
# JIRA_PROJECT=PROJ
# JIRA_API_VERSION=2
# JIRA_AUTH_MODE=bearer
# JIRA_VERIFY_SSL=true
# JIRA_USER_AGENT=cojira/0.1
```

### Project defaults with `.cojira.json`

Optional file in the working directory or repo root:

```json
{
  "jira": {
    "default_project": "PROJ",
    "default_jql_scope": "project = PROJ"
  },
  "confluence": {
    "default_space": "TEAM",
    "root_page_id": "12345"
  },
  "aliases": {
    "my-board": "jira board-issues 45434 --all"
  }
}
```

## Safety rules

Always follow these rules:

- Never print or paste tokens.
- Never commit `.env`.
- Confluence content is storage-format XHTML. Do not convert it through Markdown or strip `<ac:...>` / `<ri:...>` macros.
- Preview multi-item or destructive work with `--dry-run` first.
- Use `cojira plan <tool> <command> ...` when you want preview-first behavior from outside the command.
- Use single quotes around JQL to avoid shell mangling of operators such as `!=`.
- Treat Jira board configuration commands as experimental and potentially brittle across Jira upgrades.
- Prefer full URLs, numeric IDs, or explicit keys when identifier resolution is ambiguous.

## Agent behavior rules

- You run `cojira`. Do not ask the user to run CLI commands for you.
- Keep user-facing replies non-technical.
- Do not show raw CLI commands, flags, JQL, XHTML, JSON envelopes, or exit codes in normal user replies.
- Summarize results in plain language.
- Use `--output-mode summary` or `--output-mode human` for user-facing read operations unless you need machine-readable follow-up data.
- Use `--output-mode json` when you need structured data for another step.
- Be honest about unsupported features. Do not imply the CLI can do something it cannot do.

## Command surface

### Top-level commands

| Command | Purpose | Example |
| --- | --- | --- |
| `bootstrap` | Write the bootstrap guide and example templates | `cojira bootstrap --output /tmp/cojira/COJIRA-BOOTSTRAP.md --force` |
| `completion` | Generate shell completion | `cojira completion zsh` |
| `confluence` | Confluence page management | `cojira confluence --help` |
| `describe` | Machine-readable capability and context report | `cojira describe --with-context --output-mode json` |
| `do` | Intent parsing into a concrete command | `cojira do "move PROJ-123 to Done"` |
| `doctor` | Setup and connectivity diagnostics | `cojira doctor` |
| `init` | Interactive setup wizard | `cojira init` |
| `jira` | Jira issue and board automation | `cojira jira --help` |
| `plan` | Preview any command without applying it | `cojira plan jira update PROJ-123 --set labels+=urgent` |

### Jira commands

| Command | Purpose | Example |
| --- | --- | --- |
| `batch` | Run batch Jira operations from a file or stdin | `cojira jira batch ops.json --dry-run` |
| `board-detail-view` | Experimental Issue Detail View management | `cojira jira --experimental board-detail-view get 45434 --output-mode json` |
| `board-issues` | List issues on a board | `cojira jira board-issues 45434 --output-mode summary` |
| `board-swimlanes` | Experimental swimlane configuration | `cojira jira --experimental board-swimlanes get 45434 --output-mode json` |
| `bulk-transition` | Transition many issues matched by JQL | `cojira jira bulk-transition --jql 'project = PROJ AND status != Done' --to "Done" --dry-run` |
| `bulk-update` | Apply one JSON payload to many issues | `cojira jira bulk-update --jql 'project = PROJ' --payload update.json --dry-run` |
| `bulk-update-summaries` | Rename many issues from CSV or JSON | `cojira jira bulk-update-summaries --file map.csv --dry-run` |
| `create` | Create an issue from JSON | `cojira jira create payload.json` |
| `delete` | Delete an issue | `cojira jira delete PROJ-123 --dry-run` |
| `fields` | Search available fields | `cojira jira fields --query priority --output-mode json` |
| `get` | Fetch full issue JSON | `cojira jira get PROJ-123 -o issue.json` |
| `info` | Show issue metadata | `cojira jira info PROJ-123 --output-mode summary` |
| `raw` | Send an allowlisted Jira REST request | `cojira jira raw GET /rest/api/2/issue/PROJ-123` |
| `search` | Search with JQL | `cojira jira search 'project = PROJ AND status != Done' --output-mode summary` |
| `sync` | Sync reporter issues to local folders | `cojira jira sync --project PROJ` |
| `sync-from-dir` | Apply updates from ticket folders | `cojira jira sync-from-dir --root ./tickets --dry-run` |
| `transition` | Transition one issue | `cojira jira transition PROJ-123 --to "Done" --dry-run` |
| `transitions` | List transitions for an issue | `cojira jira transitions PROJ-123` |
| `update` | Update issue fields | `cojira jira update PROJ-123 --set labels+=urgent --dry-run` |
| `validate` | Validate Jira JSON payload shape | `cojira jira validate payload.json` |
| `whoami` | Show the current Jira identity | `cojira jira whoami --output-mode summary` |

### Confluence commands

| Command | Purpose | Example |
| --- | --- | --- |
| `archive` | Archive a page under an archive parent | `cojira confluence archive 12345 --to-parent 67890 --dry-run` |
| `batch` | Run batch Confluence operations | `cojira confluence batch ops.json --dry-run` |
| `comments` | List page comments with inline context | `cojira confluence comments 12345 --output-mode summary` |
| `copy-tree` | Copy a page tree under a new parent | `cojira confluence copy-tree 12345 67890 --dry-run` |
| `create` | Create a page from XHTML | `cojira confluence create "Title" -s TEAM -f page.html` |
| `find` | Search by title or CQL | `cojira confluence find "Release Notes" --output-mode summary` |
| `get` | Download storage-format XHTML | `cojira confluence get 12345 -o page.html` |
| `info` | Show page metadata | `cojira confluence info 12345 --output-mode summary` |
| `move` | Move a page to a new parent | `cojira confluence move 12345 67890 --dry-run` |
| `raw` | Send a read-only Confluence REST request | `cojira confluence raw GET /rest/api/content/12345` |
| `rename` | Rename a page | `cojira confluence rename 12345 "New Title" --dry-run` |
| `tree` | Show page hierarchy | `cojira confluence tree 12345 -d 5 --output-mode summary` |
| `update` | Update a page from XHTML | `cojira confluence update 12345 page.html --diff` |
| `validate` | Validate storage-format XHTML | `cojira confluence validate page.html` |
| `view` | Fetch rendered HTML for reading | `cojira confluence view 12345 --output-mode summary` |

## Intent phrasebook

### Jira intent mapping

- "What's the status of PROJ-123?" -> `cojira jira info PROJ-123 --output-mode summary`
- "Show me details for PROJ-123" -> `cojira jira info PROJ-123 --output-mode summary` or `cojira jira get PROJ-123`
- "Add label urgent to PROJ-123" -> `cojira jira update PROJ-123 --set labels+=urgent --dry-run`
- "Change priority to High" -> `cojira jira update PROJ-123 --set priority:=High --dry-run`
- "Move PROJ-123 to Done" -> `cojira jira transition PROJ-123 --to "Done" --dry-run`
- "Move all open bugs to Done" -> `cojira jira bulk-transition --jql 'project = FOO AND type = Bug AND status != Done' --to "Done" --dry-run`
- "Find all open bugs in FOO" -> `cojira jira search 'project = FOO AND type = Bug AND status != Done' --output-mode summary`
- "Save search results to a file" -> `cojira jira search 'project = FOO' -o results.json`
- "Show me the board" -> `cojira jira board-issues <board-id-or-url> --output-mode summary`
- "Show me all issues on the board" -> `cojira jira board-issues <board-id-or-url> --all --output-mode summary`
- "Create a new issue in FOO" -> write a JSON payload, then run `cojira jira create payload.json`
- "List available transitions" -> `cojira jira transitions PROJ-123`
- "What fields are available?" -> `cojira jira fields --query <term>`
- "Validate this payload" -> `cojira jira validate payload.json`
- "Rename issues in bulk" -> `cojira jira bulk-update-summaries --file map.csv --dry-run`
- "Bulk update issues" -> `cojira jira bulk-update --jql '...' --payload payload.json --dry-run`
- "Run a batch of operations" -> `cojira jira batch config.json --dry-run`
- "Sync issues to disk" -> `cojira jira sync --project PROJ`
- "Sync from local folders" -> `cojira jira sync-from-dir --root ./tickets --dry-run`
- "Parse this intent" -> `cojira do "move PROJ-123 to Done"`
- "What fields are on the board detail view?" -> `cojira jira --experimental board-detail-view get <board> --output-mode json`
- "Find a board detail view field ID" -> `cojira jira --experimental board-detail-view search-fields <board> --query "epic" --output-mode json`
- "Configure the board detail view" -> export -> edit -> `cojira jira --experimental board-detail-view apply <board> --file fields.json --dry-run`
- "Show me the board swimlanes" -> `cojira jira --experimental board-swimlanes get <board> --output-mode json`
- "Validate swimlane queries" -> `cojira jira --experimental board-swimlanes validate <board> --file swimlanes.json --output-mode summary`
- "Simulate swimlane routing" -> `cojira jira --experimental board-swimlanes simulate <board> --output-mode summary`
- "Who am I logged in as?" -> `cojira jira whoami --output-mode summary`
- "Delete PROJ-123" -> `cojira jira delete PROJ-123 --dry-run`
- "Add a comment to PROJ-123" -> unsupported; say so clearly

### Confluence intent mapping

- "Read this Confluence page" -> `cojira confluence info <page> --output-mode summary` or `cojira confluence get <page>`
- "Update Confluence page <URL> to include X" -> `get` -> edit XHTML -> `update`
- "Find Confluence pages titled X" -> `cojira confluence find "X" --output-mode summary`
- "Copy this Confluence tree" -> `cojira confluence copy-tree <page> <parent> --dry-run`
- "Archive this Confluence page" -> `cojira confluence archive <page> --to-parent <parent> --dry-run`
- "Create a new page" -> `cojira confluence create "Title" -s SPACE -f content.html`
- "Rename this page" -> `cojira confluence rename <page> "New Title" --dry-run`
- "Move this page under another" -> `cojira confluence move <page> <parent> --dry-run`
- "Show the page tree" -> `cojira confluence tree <page> -d 5 --output-mode summary`
- "Show me the comments on this page" -> `cojira confluence comments <page> --output-mode summary`
- "Run batch operations" -> `cojira confluence batch config.json --dry-run`
- "Validate this XHTML" -> `cojira confluence validate page.html`

## Flexible identifiers

### Confluence

- numeric page id: `12345`
- full URL: `https://confluence.example.com/confluence/pages/viewpage.action?pageId=12345`
- display URL: `https://confluence.example.com/confluence/display/SPACE/Page+Title`
- tiny link code: `APnAVAE`
- `SPACE:"Page Title"`

### Jira

- issue key: `PROJ-123`
- numeric issue id: `10001`
- full URL: `https://jira.example.com/jira/browse/PROJ-123`

### Jira boards

- board id: `45434`
- board URL: `https://jira.example.com/jira/secure/RapidBoard.jspa?rapidView=45434`

## Resumable partial failures

`copy-tree`, `jira batch`, `confluence batch`, `bulk-update`, `bulk-transition`, and `bulk-update-summaries` can now emit machine-readable `resumable_state` on partial failure.

The important contract is:

- the command snapshots the original plan on first execution,
- each successful item gets a checkpoint,
- partial failure returns `resumable_state`,
- rerunning the same command with the emitted `--idempotency-key` resumes from the frozen snapshot instead of replaying completed items.

Expected `resumable_state` fields:

- `version`
- `kind`
- `idempotency_key`
- `request_id`
- `target`
- `snapshot`
- `completed`
- `remaining`
- `resume_hint`
- `notes`

Operational rule:

- if a multi-item mutation fails part-way, read `resumable_state.idempotency_key` from JSON output and rerun the same command with `--idempotency-key <that-key>`.

## Common workflows

### Safe Jira issue update

```bash
cojira jira update PROJ-123 --set summary="New title" --dry-run
cojira jira update PROJ-123 --set summary="New title"
```

### Safe bulk Jira transition

```bash
cojira jira bulk-transition --jql 'project = PROJ AND status != Done' --to "Done" --dry-run
cojira jira bulk-transition --jql 'project = PROJ AND status != Done' --to "Done" --sleep 0.5
```

### Safe Confluence edit

```bash
cojira confluence get 12345 -o page.html
cojira confluence validate page.html
cojira confluence update 12345 page.html --diff
cojira confluence update 12345 page.html
```

### Safe Confluence tree copy

```bash
cojira confluence copy-tree 12345 67890 --dry-run
cojira confluence copy-tree 12345 67890 --idempotency-key copy-12345-67890
```

### Network tuning for large operations

```bash
cojira jira --timeout 60 --retries 8 bulk-update --jql 'project = PROJ' --payload update.json --sleep 0.5
cojira confluence --timeout 60 tree 12345 -d 5
```

## Unsupported features and what to do instead

### Jira

Unsupported:

- comments
- watchers
- issue links
- attachments
- worklogs
- sprints
- board columns or column mapping
- filters
- dashboards
- project administration
- user management beyond `whoami`
- components
- versions or releases
- clone or duplicate issue flows

Do instead:

- use field updates, transitions, board reads, or raw allowlisted reads when those meet the need,
- say clearly when the requested action is unsupported.

### Confluence

Unsupported:

- permanent delete
- page restrictions or permissions
- attachments
- dedicated label-management commands
- space administration
- page history or version diff commands
- templates
- blog posts
- content properties
- watchers
- export to PDF or Word

Do instead:

- use `archive` instead of delete,
- use `comments`, `tree`, `find`, `get`, `view`, `move`, `rename`, `update`, and `copy-tree` where applicable,
- say clearly when the requested action is unsupported.

## Error codes and recovery

| Code | Meaning | Recovery |
| --- | --- | --- |
| `CONFIG_MISSING_ENV` | Required setup is missing | run `cojira init` or write `.env` / global credentials |
| `CONFIG_INVALID` | `.env`, `.cojira.json`, or input config is invalid | correct the file and rerun; `cojira doctor` helps |
| `HTTP_ERROR` | Generic HTTP failure | retry after checking connectivity and base URLs |
| `HTTP_401` | Authentication failed | replace token or remove mismatched `JIRA_EMAIL` when using bearer/PAT |
| `HTTP_403` | Permission denied | verify the token and resource permissions |
| `HTTP_404` | Not found | confirm identifiers and base URL, including any Jira context path |
| `HTTP_429` | Rate limited | retry with backoff or lower concurrency |
| `TIMEOUT` | Request timed out | retry with a larger `--timeout` |
| `IDENT_UNRESOLVED` | Identifier could not be resolved | use a full URL, numeric id, or explicit key |
| `FETCH_FAILED` | Read failed | retry after confirming the target exists |
| `UPDATE_FAILED` | Update failed | fetch latest state, preview again, and retry |
| `CREATE_FAILED` | Create failed | validate payload or permissions, then retry |
| `TRANSITION_FAILED` | Transition failed | inspect available transitions and retry |
| `FILE_NOT_FOUND` | Referenced file is missing | fix the path and rerun |
| `INVALID_JSON` | JSON payload is invalid | fix the JSON or run a validate command first |
| `OP_FAILED` | Generic operation failure | read the message and retry with the recommended hint |
| `UNSUPPORTED` | Requested operation is unsupported | use a supported alternative or say so explicitly |
| `CONFIG_ERROR` | General configuration failure | run `cojira doctor` |
| `ERROR` | Unexpected failure | retry or inspect `cojira doctor` output |
| `EMPTY_CONTENT` | Refusing to write empty Confluence content | provide non-empty storage XHTML |
| `MOVE_FAILED` | Confluence move failed | verify permissions and target parent |
| `RENAME_FAILED` | Rename failed | retry with a valid, unique title |
| `SEARCH_FAILED` | Search failed | correct the query and retry |
| `LABEL_FAILED` | Label change failed | verify permissions and retry |
| `COPY_FAILED` | Copy operation failed | inspect `resumable_state` and rerun with the emitted idempotency key |
| `INVALID_TITLE` | Page title is invalid | fix the title and retry |
| `COPY_LIMITATION` | Confluence refused part of a copy flow | inspect warnings, then resume or repair manually |
| `AMBIGUOUS_TRANSITION` | More than one transition matched | choose a specific transition id |
| `TRANSITION_NOT_FOUND` | Requested transition is unavailable | run `cojira jira transitions <issue>` |
| `MISSING_DEP` | Required local dependency is missing | install the missing dependency or rebuild `cojira` |

## User-facing response rules

When you report back to the user:

- summarize the outcome in plain language,
- mention the affected Jira issues or Confluence pages,
- describe what changed or what blocked the change,
- never paste tokens, raw JQL, raw JSON envelopes, or storage XHTML unless the user explicitly asks for that technical detail.

Examples:

- single Jira issue: "PROJ-123 is In Progress, assigned to Alex, priority High."
- Jira search: "Found 12 open bugs. The most urgent are PROJ-1, PROJ-7, and PROJ-11."
- Confluence page: "The page is in TEAM space and was last updated yesterday by Priya."
- mutation: "I added the label and left everything else unchanged."
- partial failure: "Some items were updated and some were not. I can resume safely from the saved checkpoint."
