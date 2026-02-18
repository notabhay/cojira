# Claude Code Instructions (cojira)

Use `cojira` to fulfill user requests about Jira issues and Confluence pages. The user does **not** need to know how `cojira` works — hide all implementation details. Never show CLI commands, JQL, XHTML, exit codes, or raw JSON to the user. Summarize results in plain language.

## First call

If you haven't used cojira this session, start with:
```
cojira describe --with-context --output-mode json
```
If `setup_needed` is true in the response, guide the user through `cojira init` (interactive wizard that auto-detects base URLs and context paths).

## Install

Preferred (no git/Go): follow `COJIRA-BOOTSTRAP.md` → "Install with curl".

Alternative (requires Go 1.22+):

```bash
go build -o cojira .
go install github.com/cojira/cojira@latest
```

## Setup

**Preferred**: Use the interactive setup wizard (auto-detects base URLs and context paths):

```bash
cojira init
```

**Alternative**: Copy `.env.example` to `.env` and fill in your credentials manually:

```bash
cp .env.example .env
# Edit .env with your values
```

Required environment variables:
- **Confluence**: `CONFLUENCE_BASE_URL`, `CONFLUENCE_API_TOKEN`
- **Jira**: `JIRA_BASE_URL`, `JIRA_API_TOKEN`
- **Optional**: `JIRA_EMAIL` enables basic auth (email + token). Omit for bearer/PAT auth.

**Important**: If your Jira instance has a context path (e.g. `/jira`), include it in `JIRA_BASE_URL`:
`JIRA_BASE_URL=https://jira.rakuten-it.com/jira`

Verify setup:

```bash
cojira doctor
```

Optional project defaults (recommended): create a `.cojira.json` to set defaults:

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

All fields are optional. `aliases` map shortcut names to full cojira commands (max 3 levels of alias expansion).

## Safety rules

- Confluence page bodies are **storage-format XHTML**; never convert to Markdown.
- Preserve all `<ac:...>` and `<ri:...>` macros.
- Use `--dry-run` on bulk/batch operations before applying.
- Use `cojira plan <tool> <cmd> ...` for previews when unsure.
- Never print or paste tokens.
- Use single quotes around JQL to avoid shell issues with `!=`.

## Agent behavior

- **You (the agent) run cojira.** Do not ask the user to run CLI commands; ask for links + desired changes, then do the work.
- **Hide implementation details from the user.** Never show `cojira` commands, Python output, CLI flags, exit codes, JQL syntax, or XHTML in your messages.
- Summarize results in plain language.
- Default to `--output-mode summary` for read operations (info, search, whoami, find).
- Use `--output-mode json` only when you need structured data for a follow-up operation.
- If setup is missing, guide the user through `cojira init` (user-friendly prompts).
- If `cojira doctor` reports errors, translate them into plain language for the user.

## Intent → Action (phrasebook)

### Jira

- **"What's the status of PROJ-123?"** → `cojira jira info PROJ-123 --output-mode summary`
- **"Show me details for PROJ-123"** → `cojira jira info PROJ-123 --output-mode summary` or `cojira jira get PROJ-123`
- **"Add label urgent to PROJ-123"** → `cojira jira update PROJ-123 --set labels+=urgent --dry-run`
- **"Change priority to High"** → `cojira jira update PROJ-123 --set priority:=High --dry-run`
- **"Move PROJ-123 to Done"** → `cojira jira transition PROJ-123 --to "Done" --dry-run`
- **"Move all open bugs to Done"** → `cojira jira bulk-transition --jql 'project = FOO AND type = Bug AND status != Done' --to "Done" --dry-run`
- **"Find all open bugs in FOO"** → `cojira jira search 'project = FOO AND type = Bug AND status != Done' --output-mode summary`
- **"Save search results to a file"** → `cojira jira search 'project = FOO' -o results.json`
- **"Show me the board"** → `cojira jira board-issues <board-id-or-url> --output-mode summary`
- **"Show me all issues on the board"** → `cojira jira board-issues <board-id-or-url> --all --output-mode summary`
- **"Create a new issue in FOO"** → write a JSON payload, then `cojira jira create payload.json`
- **"List available transitions"** → `cojira jira transitions PROJ-123`
- **"What fields are available?"** → `cojira jira fields --query <term>`
- **"Validate this payload"** → `cojira jira validate payload.json`
- **"Rename issues in bulk"** → `cojira jira bulk-update-summaries --file map.csv --dry-run`
- **"Bulk update issues"** → `cojira jira bulk-update --jql '...' --payload p.json --dry-run`
- **"Run a batch of operations"** → `cojira jira batch config.json --dry-run`
- **"Sync issues to disk"** → `cojira jira sync --project PROJ`
- **"Sync from local folders"** → `cojira jira sync-from-dir --root ./tickets --dry-run`
- **"Parse this intent"** → `cojira do "move PROJ-123 to Done"`
- **"What fields are on the board detail view?"** → `cojira jira --experimental board-detail-view get <board> --output-mode json`
- **"Find a board detail view field ID"** → `cojira jira --experimental board-detail-view search-fields <board> --query "epic" --output-mode json`
- **"Configure the board detail view"** → export → edit → `cojira jira --experimental board-detail-view apply <board> --file fields.json --dry-run`
- **"Show me the board swimlanes"** → `cojira jira --experimental board-swimlanes get <board> --output-mode json`
- **"Validate swimlane queries"** → `cojira jira --experimental board-swimlanes validate <board> --output-mode summary`
- **"Simulate swimlane routing"** → `cojira jira --experimental board-swimlanes simulate <board> --output-mode summary`
- **"Who am I logged in as?"** → `cojira jira whoami --output-mode summary`
- **"Add a comment to PROJ-123"** → **Not supported** (say so)

### Confluence

- **"Read this Confluence page"** → `cojira confluence info <page> --output-mode summary` or `cojira confluence get <page>`
- **"Update Confluence page <URL> to include X"** → `get` → edit XHTML → `update`
- **"Find Confluence pages titled X"** → `cojira confluence find "X" --output-mode summary`
- **"Copy this Confluence tree"** → `cojira confluence copy-tree <page> <parent> --dry-run`
- **"Archive this Confluence page"** → `cojira confluence archive <page> --to-parent <parent> --dry-run`
- **"Create a new page"** → `cojira confluence create "Title" -s SPACE -f content.html`
- **"Rename this page"** → `cojira confluence rename <page> "New Title"`
- **"Move this page under another"** → `cojira confluence move <page> <parent>`
- **"Show the page tree"** → `cojira confluence tree <page> -d 5`
- **"Run batch operations"** → `cojira confluence batch config.json --dry-run`
- **"Validate this XHTML"** → `cojira confluence validate page.html`

## Not supported

If the user asks for any of these, tell them clearly that it's not available yet.

### Jira

Comments, watchers, issue links (blocks/relates-to/duplicates), attachments, delete issues, worklogs, sprints, board columns/column mapping, filters, dashboards, project administration, user management (beyond `whoami`), components, versions/releases, clone/duplicate issues.

### Confluence

Comments, delete pages (use `archive` instead), page permissions/restrictions, attachments, labels (as a dedicated command), space administration, page history/version comparison, templates, blog posts, content properties, watchers, export to PDF/Word.

## Presenting results to users

When relaying cojira output to the user, follow these patterns:

- **Single issue**: State key fields in a sentence — "PROJ-123 is In Progress, assigned to John, priority High."
- **Search results**: Summarize count and list the top items — "Found 12 open bugs. Here are the first few: ..."
- **Confluence page**: Title, space, last modified — "Page 'Release Notes' in TEAM space, last updated by Jane on Feb 10."
- **Transitions**: Confirm the change — "Moved PROJ-123 from In Progress to Done."
- **Updates**: Confirm what changed — "Updated PROJ-123: added label 'urgent', changed priority from Medium to High."
- **Bulk operations**: Summarize — "Updated 15 issues" or "Transitioned 8 issues to Done."
- **Board issues**: Group by status column — "Board has 42 issues: 10 To Do, 25 In Progress, 7 Done."
- **Errors**: Use the `user_message` from the error response, never the raw error code or technical message.

## Command cheatsheet

### Meta Commands

| Command | Description | Example |
|---------|-------------|---------|
| `describe` | Agent capabilities and live checks | `cojira describe --with-context --output-mode json` |
| `describe --agent-prompt` | Compact text prompt for system prompts | `cojira describe --agent-prompt` |
| `doctor` | Diagnose connection/auth issues | `cojira doctor` |
| `init` | Interactive setup wizard | `cojira init` |
| `bootstrap` | Generate workspace templates | `cojira bootstrap` |
| `plan` | Preview any command without applying | `cojira plan jira update PROJ-123 --set labels+=urgent` |
| `do` | Natural-language intent parser | `cojira do "move PROJ-123 to Done"` |

### Confluence Commands

| Command | Description | Example |
|---------|-------------|---------|
| `info` | Show page metadata | `cojira confluence info 12345 --output-mode json` |
| `get` | Download page content (XHTML) | `cojira confluence get 12345 -o page.html` |
| `update` | Update page from XHTML file | `cojira confluence update 12345 page.html` |
| `create` | Create new page | `cojira confluence create "Title" -s SPACE -f content.html` |
| `rename` | Rename a page | `cojira confluence rename 12345 "New Title"` |
| `move` | Move page to new parent | `cojira confluence move 12345 67890` |
| `tree` | Show page hierarchy | `cojira confluence tree 12345 -d 5` |
| `find` | Search pages by title/CQL | `cojira confluence find "search term" -s SPACE` |
| `copy-tree` | Duplicate a page tree | `cojira confluence copy-tree <page> <parent> --dry-run` |
| `archive` | Archive a page (move + label) | `cojira confluence archive <page> --to-parent <parent> --dry-run` |
| `validate` | Sanity-check XHTML file | `cojira confluence validate page.html` |
| `batch` | Run batch operations | `cojira confluence batch config.json --dry-run` |

### Jira Commands

| Command | Description | Example |
|---------|-------------|---------|
| `info` | Show issue metadata | `cojira jira info PROJ-123 --output-mode json` |
| `get` | Fetch full issue JSON | `cojira jira get PROJ-123 -o issue.json` |
| `update` | Update issue fields | `cojira jira update PROJ-123 --set summary="New" --dry-run` |
| `create` | Create issue from JSON | `cojira jira create payload.json` |
| `transition` | Change issue status | `cojira jira transition PROJ-123 --to "Done" --dry-run` |
| `transitions` | List available transitions | `cojira jira transitions PROJ-123 --to "Done"` |
| `search` | Query issues with JQL | `cojira jira search 'project=PROJ' -o results.json` |
| `board-issues` | List issues on a board | `cojira jira board-issues <board-id-or-url>` |
| `fields` | List available fields | `cojira jira fields --query priority` |
| `whoami` | Show current user | `cojira jira whoami` |
| `validate` | Sanity-check JSON payload | `cojira jira validate payload.json` |
| `batch` | Run batch operations | `cojira jira batch config.json --dry-run` |
| `bulk-update` | Update multiple issues | `cojira jira bulk-update --jql '...' --payload p.json --dry-run` |
| `bulk-transition` | Transition multiple issues | `cojira jira bulk-transition --jql '...' --to "Done" --dry-run` |
| `bulk-update-summaries` | Bulk rename from CSV/JSON | `cojira jira bulk-update-summaries --file map.csv` |
| `sync` | Download issues to disk | `cojira jira sync --project PROJ` |
| `sync-from-dir` | Update issues from local folders | `cojira jira sync-from-dir --root ./tickets` |
| `board-swimlanes` | (EXP) Manage board swimlanes | `cojira jira --experimental board-swimlanes get 45434` |
| `board-detail-view` | (EXP) Manage board detail view fields | `cojira jira --experimental board-detail-view get 45434` |

## Quick field updates (--set flag)

The `update` command supports inline field changes without a JSON payload:

```bash
cojira jira update PROJ-123 --set summary="New title" --dry-run
cojira jira update PROJ-123 --set labels+=urgent --dry-run       # append to list
cojira jira update PROJ-123 --set labels-=stale --dry-run        # remove from list
cojira jira update PROJ-123 --set priority:=High --dry-run       # set object field
```

## Flexible identifiers

Both tools accept multiple identifier formats:

**Confluence pages:**
- Numeric ID: `12345`
- URL: `https://confluence.rakuten-it.com/confluence/pages/viewpage.action?pageId=12345`
- URL: `https://confluence.rakuten-it.com/confluence/display/SPACE/Page+Title`
- Tiny link code: `APnAVAE`
- Space:Title: `SPACE:"My Page Title"`

**Jira issues:**
- Issue key: `PROJ-123`
- Numeric ID: `10001`
- URL: `https://jira.rakuten-it.com/jira/browse/PROJ-123`

**Jira boards:**
- Board ID: `12345`
- URL: `https://jira.rakuten-it.com/jira/secure/RapidBoard.jspa?rapidView=12345`

## Common workflows

### Edit a Confluence page (lossless)

```bash
# 1. Download the page
cojira confluence get 12345 -o page.html

# 2. Edit page.html (preserve all <ac:...> and <ri:...> macros!)

# 3. Upload changes
cojira confluence update 12345 page.html

# 4. Verify
cojira confluence info 12345 --output-mode json
```

### Update a Jira issue safely

```bash
# Preview changes first
cojira jira update PROJ-123 --set summary="New title" --diff
cojira jira update PROJ-123 --set summary="New title" --dry-run

# Apply if preview looks good
cojira jira update PROJ-123 --set summary="New title"
```

### Bulk update Jira issues

```bash
# Create payload file (see examples/jira-update-payload.json)
cat > update.json << 'EOF'
{"fields": {"labels": ["bulk-updated"]}}
EOF

# Preview
cojira jira bulk-update --jql 'project = PROJ AND status = "Open"' --payload update.json --dry-run

# Apply with rate limiting
cojira jira bulk-update --jql 'project = PROJ AND status = "Open"' --payload update.json --sleep 0.5
```

### Bulk transition issues

```bash
# Preview
cojira jira bulk-transition --jql 'project = PROJ AND status = "Open"' --to "In Progress" --dry-run

# Apply
cojira jira bulk-transition --jql 'project = PROJ AND status = "Open"' --to "In Progress" --sleep 0.5
```

### Transition an issue to a new status

```bash
# Shorthand: transition by status name (finds the right transition ID automatically)
cojira jira transition PROJ-123 --to "Done" --dry-run
cojira jira transition PROJ-123 --to "Done"

# Or find the transition ID manually
cojira jira transitions PROJ-123 --to "Done"
cojira jira transition PROJ-123 31
```

### List issues on a board

```bash
cojira jira board-issues 45434 --output-mode summary
cojira jira board-issues 45434 --all --output-mode summary
cojira jira board-issues 45434 --all --max-issues 5000 --output-mode summary
cojira jira board-issues 'https://jira.rakuten-it.com/jira/secure/RapidBoard.jspa?rapidView=45434' -o board.json
```

### Experimental board configuration (requires `--experimental`)

These commands use internal GreenHopper REST APIs and require board administration permission.

#### Board Issue Detail View fields

```bash
# See current fields
cojira jira --experimental board-detail-view get <board> --output-mode json

# Search available fields (avoids dumping the full list)
cojira jira --experimental board-detail-view search-fields <board> --query "epic" --output-mode json

# See current + available fields
cojira jira --experimental board-detail-view get <board> --include-available --output-mode json

# Export current config to file
cojira jira --experimental board-detail-view export <board> -o detail-view.json

# Apply desired config (preview first)
cojira jira --experimental board-detail-view apply <board> --file detail-view.json --dry-run
cojira jira --experimental board-detail-view apply <board> --file detail-view.json

# Apply and remove fields not in file
cojira jira --experimental board-detail-view apply <board> --file detail-view.json --delete-missing --dry-run
```

**Config file format** (output of `export`, input for `apply`):
```json
{"fields": [{"fieldId": "priority", "name": "Priority", "category": "System"}]}
```

Or simpler:
```json
{"fieldIds": ["priority", "status", "assignee"]}
```

#### Board swimlanes

```bash
cojira jira --experimental board-swimlanes get <board> --output-mode json
cojira jira --experimental board-swimlanes export <board> -o swimlanes.json
cojira jira --experimental board-swimlanes validate <board> --file swimlanes.json --sleep 0.5 --output-mode json
cojira jira --experimental board-swimlanes simulate <board> --sleep 0.5 --output-mode json
cojira jira --experimental board-swimlanes apply <board> --file swimlanes.json --dry-run
cojira jira --experimental board-swimlanes set-strategy <board> --strategy custom
cojira jira --experimental board-swimlanes add <board> --name "P0" --query "priority = Highest"
cojira jira --experimental board-swimlanes delete <board> <id>
cojira jira --experimental board-swimlanes move <board> <id> --first
```

## JQL tips

- Always use **single quotes** around JQL to avoid shell issues with `!=`:
  ```bash
  cojira jira search 'statusCategory != Done'
  ```
- cojira auto-fixes common shell mangling (e.g. `\!` → `!`), but single quotes prevent it entirely.

## Networking flags

For large operations or flaky networks:

```bash
cojira confluence --timeout 60 --retries 8 --debug tree 12345 -d 5
cojira jira --timeout 60 --retries 8 bulk-update --jql '...' --payload p.json --sleep 1.0
```

| Flag | Default | Description |
|------|---------|-------------|
| `--timeout` | 30 | HTTP timeout in seconds |
| `--retries` | 5 | Retry count for 429/5xx errors |
| `--retry-base-delay` | 0.5 | Initial backoff delay (seconds) |
| `--retry-max-delay` | 8.0 | Maximum backoff delay (seconds) |
| `--debug` | off | Print retry attempts to stderr |

## Payload examples

See the `examples/` directory for ready-to-use templates:

| File | Purpose |
|------|---------|
| `jira-create-payload.json` | Create a new Jira issue |
| `jira-update-payload.json` | Update Jira issue fields |
| `jira-batch-config.json` | Batch create/update/transition operations |
| `jira-bulk-summaries.json` | JSON mapping for bulk summary updates |
| `jira-bulk-summaries.csv` | CSV mapping for bulk summary updates |
| `confluence-batch-config.json` | Batch Confluence operations |
| `confluence-page-content.html` | Sample Confluence XHTML with macros |

## Troubleshooting

| Error | Cause | Fix |
|-------|-------|-----|
| `401 Unauthorized` | Bad token, or `JIRA_EMAIL` set with PAT | Regenerate token; if using PAT, remove `JIRA_EMAIL` or set `JIRA_AUTH_MODE=bearer` |
| `403 Forbidden` | No permission | Verify account has access to the resource |
| `404 Not Found` | Wrong base URL (missing context path) | Ensure `JIRA_BASE_URL` includes context path (e.g. `/jira`). Re-run `cojira init`. |
| `409 Conflict` | Version conflict | Retry (built-in for move/batch); fetch latest version first |
| Macros lost after update | Converted to Markdown | Always edit as XHTML; never convert Confluence storage format |

Run `cojira doctor` for automated diagnostics with actionable hints.

## Exit codes

| Code | Meaning |
|------|---------|
| 0 | Success |
| 1 | Operation failed (API error, file not found, etc.) |
| 2 | Usage error (missing args, bad config) |
| 3 | Needs user interaction (TTY required, interactive mode required) |
