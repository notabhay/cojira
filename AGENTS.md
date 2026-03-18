# Agent Instructions (cojira)

This is the canonical agent guide for `cojira`.

Inside the repo, this file is the source of truth.
Outside the repo, `COJIRA-BOOTSTRAP.md` and `internal/assets/COJIRA-BOOTSTRAP.md` must contain
the same guidance so a coding agent can bootstrap from a clean workspace without reading anything else.

To regenerate the bootstrap guide and example templates after making changes here:

```bash
cojira bootstrap --output /tmp/cojira/COJIRA-BOOTSTRAP.md --force
```

## Canonical setup prompt

The exact prompt a user gives their coding agent to install and configure cojira:

> hey, run `curl -fsSL https://cojira.notabhay.xyz/install.sh | bash` and then follow the instructions in `/tmp/cojira/COJIRA-BOOTSTRAP.md`

---

<!-- Everything below is kept in sync with internal/assets/COJIRA-BOOTSTRAP.md and root COJIRA-BOOTSTRAP.md -->
# cojira Bootstrap Guide

> **For the coding agent:** This file is your complete guide for getting cojira set up and using it
> on behalf of a non-technical user. Part 1 is the onboarding playbook — follow it step-by-step
> if this is a fresh install. Part 2 is the full technical reference for ongoing use.
> The user does not need to read this file. You do.

---

## Quick orientation

If `cojira` is **already set up** (credentials are configured), skip straight to Part 2.

If you just ran the installer for the first time, start at Part 1 right now.

To check whether setup is needed:

```bash
cojira describe --with-context --output-mode json
```

If `setup_needed` is `false`, skip to Part 2. If it is `true` or if the command fails, follow Part 1.

---

# PART 1 — ONBOARDING PLAYBOOK

## What just happened

The install script ran and did three things:

1. Ensured a Go toolchain was available (downloaded one if needed).
2. Downloaded the cojira source from GitHub, built a native binary, and installed it to
   `${GOBIN:-$HOME/.local/bin}/cojira`.
3. Wrote this guide and a set of example templates to `/tmp/cojira/`.

`cojira` is now installed. It does not yet know your Jira or Confluence URLs or credentials.
That is what Part 1 will fix.

---

## Your role

You are setting up cojira on behalf of a non-technical user. They do not need to run any commands
or understand how any of this works. You will:

1. Have a brief, friendly conversation with the user to collect the information you need.
2. Write their credentials to a `.env` file yourself.
3. Verify the connection is working.
4. Tell the user what you can now do for them.

**Rules during onboarding:**

- Never show the user raw tokens, `.env` file contents, CLI commands, JSON, error stack traces,
  or any other technical output.
- If something goes wrong, handle it yourself or ask a clear, non-technical question.
- Keep the conversation short and cheerful. The user just wants it to work.

---

## Step 1 — Greet the user

Send this message (adapt wording to your own voice):

> "cojira installed successfully! It's a tool that lets me talk directly to your Jira and
> Confluence on your behalf — reading tickets, updating them, searching, creating pages,
> the whole thing.
>
> Before I can connect, I need a couple of things from you. It takes about 2-3 minutes and
> you won't need to do it again.
>
> First: **what's your company's Jira URL?**
> It usually looks like one of these:
> - `https://yourcompany.atlassian.net` — if you're on Atlassian Cloud
> - `https://jira.yourcompany.com` — if it's self-hosted
>
> Just paste or type it in."

---

## Step 2 — Determine their Jira hosting type

Look at the URL they give you:

| What you see in the URL | Hosting type | Auth approach |
|---|---|---|
| `.atlassian.net` | **Atlassian Cloud** | email + API token (basic auth) |
| Custom domain (`.com`, `.net`, etc.) | **Self-hosted Server/Data Center** | Personal Access Token only (bearer auth) |

This determines which questions to ask next and which auth mode to configure.

---

## Step 3 — Collect Jira credentials

### If Atlassian Cloud

Ask the user:

> "Got it, you're on Atlassian Cloud.
>
> I'll need your **Atlassian API token** — this authenticates me with both Jira and Confluence
> so you only need to create one.
>
> Here's how (takes about 30 seconds):
> 1. Go to: **https://id.atlassian.com/manage-profile/security/api-tokens**
>    (make sure you're logged in to your Atlassian account first)
> 2. Click **'Create API token'**
> 3. Give it a name like `cojira`
> 4. Click **Create** and then **Copy** the token right away
>    (you won't be able to see it again after you close that dialog)
>
> Once you have it, paste it here."

After they paste the token, ask:

> "Perfect. And what's the **email address** you use to log into Jira?
> (It's the same one for your Atlassian account.)"

**What you now have for Jira (Cloud):**
- `JIRA_BASE_URL` = the URL they gave you (e.g., `https://yourcompany.atlassian.net`)
- `JIRA_API_TOKEN` = the token they pasted
- `JIRA_EMAIL` = the email they gave you

### If Self-hosted (Server or Data Center)

Ask the user:

> "Got it, you're on a self-hosted Jira.
>
> I'll need a **Personal Access Token (PAT)** for Jira. Here's how to create one:
> 1. Log into Jira
> 2. Click your **avatar or profile picture** in the top-right corner
> 3. Go to **Profile** → look for **'Personal Access Tokens'** in the left menu
>    (on older versions it might be under **Security**)
> 4. Click **'Create token'**, give it a name like `cojira`
> 5. Copy the token — you won't see it again after closing
>
> Paste it here when you have it."

**What you now have for Jira (self-hosted):**
- `JIRA_BASE_URL` = the URL they gave you (may need `/jira` context path — see Step 6)
- `JIRA_API_TOKEN` = the PAT they pasted
- No `JIRA_EMAIL` — self-hosted uses bearer mode

---

## Step 4 — Collect Confluence credentials

### If Atlassian Cloud

Ask the user:

> "Almost done! What's your Confluence URL?
>
> If you're on Atlassian Cloud it's often the same domain as Jira, something like:
> - `https://yourcompany.atlassian.net/wiki`
>
> (I'll use the same API token you already gave me — no new token needed.)"

**What you now have for Confluence (Cloud):**
- `CONFLUENCE_BASE_URL` = what they give you (often `https://yourcompany.atlassian.net/wiki`)
- `CONFLUENCE_API_TOKEN` = same token as `JIRA_API_TOKEN`

### If Self-hosted

Ask the user:

> "What's your Confluence URL?
> (Usually something like `https://confluence.yourcompany.com`)"

After they answer:

> "And I'll need a **Personal Access Token for Confluence** too — it's a separate token from Jira.
>
> Here's how:
> 1. Log into Confluence
> 2. Click your **avatar or profile picture** (top-right)
> 3. Go to **Settings** or **Profile** → look for **'Personal Access Tokens'**
> 4. Click **'Create token'**, name it `cojira`
> 5. Copy it immediately
>
> Paste it here."

**What you now have for Confluence (self-hosted):**
- `CONFLUENCE_BASE_URL` = what they give you
- `CONFLUENCE_API_TOKEN` = the Confluence PAT they pasted

---

## Step 5 — Optional: default project

Ask this after you have credentials. It saves the user from mentioning their project every time.

> "One optional shortcut: do you have a Jira project you work in most often?
> If so, tell me the **project key** — it's the short code in ticket IDs like `PROJ` in `PROJ-123`.
>
> If you work across several projects or aren't sure, just say skip."

If they give you a key, you will add it to the `.cojira.json` config later.

---

## Step 6 — Write the configuration

Now that you have everything, write the credentials to a `.env` file.

**Where to write it:**
- In the current working directory as `.env` — if this is a project repo, write it there
- Or at `~/.config/cojira/credentials` for global access across all projects
  (create the directory first: `mkdir -p ~/.config/cojira`)

**Format for Atlassian Cloud:**

```dotenv
# Jira
JIRA_BASE_URL=https://yourcompany.atlassian.net
JIRA_API_TOKEN=their-token-here
JIRA_EMAIL=their@email.com

# Confluence
CONFLUENCE_BASE_URL=https://yourcompany.atlassian.net/wiki
CONFLUENCE_API_TOKEN=their-token-here
```

**Format for self-hosted Server/Data Center:**

```dotenv
# Jira
JIRA_BASE_URL=https://jira.yourcompany.com
JIRA_API_TOKEN=their-jira-pat-here

# Confluence
CONFLUENCE_BASE_URL=https://confluence.yourcompany.com
CONFLUENCE_API_TOKEN=their-confluence-pat-here
```

**Auth mode is determined automatically:**
- `JIRA_EMAIL` is present → cojira uses basic auth (email + token) — correct for Cloud
- `JIRA_EMAIL` is absent → cojira uses bearer auth (token only) — correct for Server/DC

**If there is a default project key, also write `.cojira.json`** in the working directory:

```json
{
  "jira": {
    "default_project": "PROJ",
    "default_jql_scope": "project = PROJ"
  }
}
```

**File permissions:** Write `.env` with mode `0600` (readable only by the user). This protects the tokens.

---

## Step 7 — Verify the connection

Run:

```bash
cojira doctor
```

This checks connectivity to both Jira and Confluence and reports who you're authenticated as.
A passing run looks like:

```
[ok] Jira     https://jira.yourcompany.com  (user: Jane Smith)
[ok] Confluence  https://confluence.yourcompany.com  (user: Jane Smith)
```

Then run:

```bash
cojira describe --with-context --output-mode json
```

Confirm `setup_needed` is `false`.

### Troubleshooting common failures

**HTTP 401 — Authentication failed:**
- Cloud: check that `JIRA_EMAIL` is set and matches the Atlassian account exactly
- Server: check that `JIRA_EMAIL` is NOT set (accidentally setting it with a bearer token breaks auth)
- Regenerate the token if it expired or was already used

**HTTP 404 — Not found:**
- The base URL has a wrong context path. Common patterns:
  - Jira Server often needs `/jira`: `https://jira.example.com/jira`
  - Confluence Server often needs `/confluence`: `https://confluence.example.com/confluence`
  - Atlassian Cloud Confluence needs `/wiki`: `https://yourcompany.atlassian.net/wiki`
- Try adding or removing the suffix and rerun `cojira doctor`

**Connection timeout or refused:**
- The URL is unreachable from this machine (VPN might be needed, or the URL is wrong)
- Ask the user to confirm they can open the URL in their browser from this same machine

**"setup_needed" still true:**
- The `.env` file might be in the wrong directory — check with `ls -la .env`
- Re-check variable names and spelling (they are case-sensitive)

---

## Step 8 — Celebrate and show what's possible

Once `cojira doctor` passes, tell the user something like this (adapt to your voice):

> "We're all connected! I can now read and update your Jira and Confluence directly.
>
> Here's a taste of what you can ask me to do:
>
> **Jira:**
> - "What's the status of PROJ-123?" — I'll read it and tell you in plain English
> - "Find all open bugs in our project" — I'll search and summarize
> - "Move PROJ-123 to Done" — I'll handle the transition
> - "Change the priority on PROJ-456 to High" — done in one step
> - "Move everything that's In Review to Done" — bulk transition across dozens of tickets
> - "Add the label 'urgent' to all open bugs" — I'll update every matching ticket
> - "Show me everything on the board" — full board view, summarized
> - "Create a ticket for [description]" — I'll draft and create it
> - "Rename all 40 tickets in this sprint using this CSV" — bulk rename from a file
>
> **Confluence:**
> - "Find the page called Release Notes" — instant search
> - "Summarize this Confluence page: [URL]" — I'll read and distill it
> - "Update the Team Handbook page to add [content]" — I'll edit it safely
> - "Copy the entire Q1 Planning tree to the Archive space" — full tree copy, resumable
> - "Show me the structure of this space" — visual hierarchy
> - "Rename this page to [new title]" — quick and safe
> - "Archive the old Project X docs" — moves without deleting
>
> Just ask in plain English and I'll take care of the rest."

---

# PART 2 — TECHNICAL REFERENCE

Everything below is the complete technical guide for using `cojira` once it is set up.
All ongoing operations should refer to this section.

---

## What cojira is for

`cojira` is an agent-first CLI for Jira and Confluence work.
It exists for workflows where:

- a human gives plain-language intent,
- an agent performs the operational work,
- and the tool provides structured output, safe previews, stable identifiers, and resumable mutation flows.

---

## First call in a session

If you have not used `cojira` in this session yet, start with:

```bash
cojira describe --with-context --output-mode json
```

If `setup_needed` is `true`, follow Part 1 above.

---

## Installation and bootstrap behavior

### Curl install

```bash
curl -fsSL https://cojira.notabhay.xyz/install.sh | bash
```

By default it:

- redirects to the `beta` branch installer,
- resolves the latest tagged GitHub release,
- ensures a local Go toolchain is available if `go` is missing,
- builds `cojira` into `${COJIRA_INSTALL_DIR:-${GOBIN:-$HOME/.local/bin}}/cojira`,
- writes `COJIRA-BOOTSTRAP.md` to `/tmp/cojira/COJIRA-BOOTSTRAP.md`.

### Optional installer overrides

- `COJIRA_VERSION`: version label to embed in the built binary.
- `COJIRA_REF`: Git ref to download instead of the default latest release tag.
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

---

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

### Interactive setup (requires a real TTY)

If running in an interactive terminal that supports stdin prompts:

```bash
cojira init
```

To write the global credentials file directly:

```bash
mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}/cojira"
cojira init --path "${XDG_CONFIG_HOME:-$HOME/.config}/cojira/credentials"
```

Note: `cojira init` requires an interactive TTY. Most coding agents run commands in a subprocess
without a TTY and should write `.env` manually instead (see Part 1, Step 6).

---

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

---

## Agent behavior rules

- You run `cojira`. Do not ask the user to run CLI commands for you.
- Keep user-facing replies non-technical.
- Do not show raw CLI commands, flags, JQL, XHTML, JSON envelopes, or exit codes in normal user replies.
- Summarize results in plain language.
- Use `--output-mode summary` or `--output-mode human` for user-facing read operations unless you need machine-readable follow-up data.
- Use `--output-mode json` when you need structured data for another step.
- Be honest about unsupported features. Do not imply the CLI can do something it cannot do.

---

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
| `init` | Interactive setup wizard (requires TTY) | `cojira init` |
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
| `clone` | Create a new issue by cloning an existing one | `cojira jira clone PROJ-123 --dry-run` |
| `create` | Create an issue from JSON, quick flags, template, or a cloned source issue | `cojira jira create --project PROJ --type Task --summary "Example issue"` |
| `development` | Experimental Jira Development-tab data reads | `cojira jira --experimental development summary PROJ-123 --output-mode json` |
| `delete` | Delete an issue | `cojira jira delete PROJ-123 --dry-run` |
| `fields` | Search available fields | `cojira jira fields --query priority --output-mode json` |
| `get` | Fetch full issue JSON | `cojira jira get PROJ-123 -o issue.json` |
| `info` | Show issue metadata, optionally with development summary | `cojira jira info PROJ-123 --output-mode summary` |
| `raw` | Send an allowlisted Jira REST request | `cojira jira raw GET /issue/PROJ-123` |
| `raw-internal` | Experimental Jira internal/API-adjacent passthrough | `cojira jira --experimental raw-internal dev-status GET /issue/summary?issueId=10001 --output-mode json` |
| `search` | Search with JQL | `cojira jira search 'project = PROJ AND status != Done' --output-mode summary` |
| `sync` | Sync reporter issues to local folders | `cojira jira sync --project PROJ` |
| `sync-from-dir` | Apply updates from ticket folders | `cojira jira sync-from-dir --root ./tickets --dry-run` |
| `transition` | Transition one issue | `cojira jira transition PROJ-123 --to "Done" --dry-run` |
| `transitions` | List transitions for an issue | `cojira jira transitions PROJ-123` |
| `update` | Update issue fields | `cojira jira update PROJ-123 --set labels+=urgent --dry-run` |
| `validate` | Validate Jira JSON payload shape | `cojira jira validate payload.json` |
| `whoami` | Show the current Jira identity | `cojira jira whoami --output-mode summary` |

### Jira `--set` syntax for `update`

| Syntax | Effect |
| --- | --- |
| `--set field=value` | Set a string field |
| `--set field:=<json>` | Set a field using a JSON literal |
| `--set labels+=value` | Append a value to an array field |
| `--set labels-=value` | Remove a value from an array field |
| `--set priority=High` | Shorthand: wraps in `{"name": value}` |
| `--set assignee=accountId:xxx` | Assignee by account ID |
| `--set assignee=null` | Unassign the ticket |

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
| `view` | Fetch rendered HTML, text, or markdown for reading | `cojira confluence view 12345 --format text --output-mode json` |

---

## Intent phrasebook

### Jira

| What the user says | What you run |
| --- | --- |
| "What's the status of PROJ-123?" | `cojira jira info PROJ-123 --output-mode summary` |
| "Show me details for PROJ-123" | `cojira jira get PROJ-123` |
| "Add label urgent to PROJ-123" | `cojira jira update PROJ-123 --set labels+=urgent --dry-run` then apply |
| "Change priority to High" | `cojira jira update PROJ-123 --set priority=High --dry-run` then apply |
| "Move PROJ-123 to Done" | `cojira jira transition PROJ-123 --to "Done" --dry-run` then apply |
| "Move all open bugs to Done" | `cojira jira bulk-transition --jql 'project = FOO AND type = Bug AND status != Done' --to "Done" --dry-run` then apply |
| "Find all open bugs in FOO" | `cojira jira search 'project = FOO AND type = Bug AND status != Done' --output-mode summary` |
| "Save search results to a file" | `cojira jira search 'project = FOO' -o results.json` |
| "Show me the board" | `cojira jira board-issues <board-id-or-url> --output-mode summary` |
| "Show me all issues on the board" | `cojira jira board-issues <board-id-or-url> --all --output-mode summary` |
| "Create a new issue in FOO titled X" | `cojira jira create --project FOO --type Task --summary "X" --dry-run` then apply |
| "Clone PROJ-123" | `cojira jira clone PROJ-123 --dry-run` then apply |
| "Show the issue plus development summary for PROJ-123" | `cojira jira info PROJ-123 --with-development --output-mode json` |
| "Show the development summary for PROJ-123" | `cojira jira --experimental development summary PROJ-123 --output-mode json` |
| "Show the pull requests for PROJ-123" | `cojira jira --experimental development pull-requests PROJ-123 --output-mode json` |
| "List available transitions" | `cojira jira transitions PROJ-123` |
| "What fields are available?" | `cojira jira fields --query <term>` |
| "Validate this payload" | `cojira jira validate payload.json` |
| "Rename issues in bulk" | `cojira jira bulk-update-summaries --file map.csv --dry-run` then apply |
| "Bulk update issues" | `cojira jira bulk-update --jql '...' --payload payload.json --dry-run` then apply |
| "Run a batch of operations" | `cojira jira batch config.json --dry-run` then apply |
| "Sync issues to disk" | `cojira jira sync --project PROJ` |
| "Sync from local folders" | `cojira jira sync-from-dir --root ./tickets --dry-run` |
| "Parse this intent" | `cojira do "move PROJ-123 to Done"` |
| "What fields are on the board detail view?" | `cojira jira --experimental board-detail-view get <board> --output-mode json` |
| "Find a board detail view field ID" | `cojira jira --experimental board-detail-view search-fields <board> --query "epic" --output-mode json` |
| "Configure the board detail view" | export -> edit -> `cojira jira --experimental board-detail-view apply <board> --file fields.json --dry-run` |
| "Show me the board swimlanes" | `cojira jira --experimental board-swimlanes get <board> --output-mode json` |
| "Validate swimlane queries" | `cojira jira --experimental board-swimlanes validate <board> --file swimlanes.json --output-mode summary` |
| "Simulate swimlane routing" | `cojira jira --experimental board-swimlanes simulate <board> --output-mode summary` |
| "Who am I logged in as?" | `cojira jira whoami --output-mode summary` |
| "Delete PROJ-123" | `cojira jira delete PROJ-123 --dry-run` then apply |
| "Add a comment to PROJ-123" | Unsupported — say so clearly |

### Confluence

| What the user says | What you run |
| --- | --- |
| "Read this Confluence page" | `cojira confluence view <page> --format text --output-mode json` |
| "Update Confluence page X to include Y" | `get` -> edit XHTML -> `update` |
| "Find Confluence pages titled X" | `cojira confluence find "X" --output-mode summary` |
| "Copy this Confluence tree" | `cojira confluence copy-tree <page> <parent> --dry-run` then apply |
| "Archive this Confluence page" | `cojira confluence archive <page> --to-parent <parent> --dry-run` then apply |
| "Create a new page" | `cojira confluence create "Title" -s SPACE -f content.html` |
| "Rename this page" | `cojira confluence rename <page> "New Title" --dry-run` then apply |
| "Move this page under another" | `cojira confluence move <page> <parent> --dry-run` then apply |
| "Show the page tree" | `cojira confluence tree <page> -d 5 --output-mode summary` |
| "Show me the comments on this page" | `cojira confluence comments <page> --output-mode summary` |
| "Run batch operations" | `cojira confluence batch config.json --dry-run` then apply |
| "Validate this XHTML" | `cojira confluence validate page.html` |

---

## Flexible identifiers

### Confluence pages

- numeric page id: `12345`
- full URL: `https://confluence.example.com/confluence/pages/viewpage.action?pageId=12345`
- display URL: `https://confluence.example.com/confluence/display/SPACE/Page+Title`
- tiny link code: `APnAVAE`
- space and title: `SPACE:"Page Title"`

### Jira issues

- issue key: `PROJ-123`
- numeric issue id: `10001`
- full URL: `https://jira.example.com/jira/browse/PROJ-123`

### Jira boards

- board id: `45434`
- board URL: `https://jira.example.com/jira/secure/RapidBoard.jspa?rapidView=45434`

---

## Resumable partial failures

`copy-tree`, `jira batch`, `confluence batch`, `bulk-update`, `bulk-transition`, and `bulk-update-summaries`
can emit machine-readable `resumable_state` on partial failure.

The contract is:

- the command snapshots the original plan on first execution,
- each successful item gets a checkpoint,
- partial failure returns `resumable_state`,
- rerunning the same command with the emitted `--idempotency-key` resumes from the frozen snapshot
  instead of replaying completed items.

Expected `resumable_state` fields: `version`, `kind`, `idempotency_key`, `request_id`, `target`,
`snapshot`, `completed`, `remaining`, `resume_hint`, `notes`.

Operational rule: if a multi-item mutation fails part-way, read `resumable_state.idempotency_key`
from JSON output and rerun the same command with `--idempotency-key <that-key>`.

---

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

---

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

Do instead:

- use field updates, transitions, clone, development reads, board reads, or raw allowlisted/internal reads when those meet the need,
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

---

## Error codes and recovery

| Code | Meaning | Recovery |
| --- | --- | --- |
| `CONFIG_MISSING_ENV` | Required setup is missing | follow Part 1, or write `.env` / global credentials |
| `CONFIG_INVALID` | `.env`, `.cojira.json`, or input config is invalid | correct the file and rerun; `cojira doctor` helps |
| `HTTP_ERROR` | Generic HTTP failure | retry after checking connectivity and base URLs |
| `HTTP_401` | Authentication failed | replace token or remove mismatched `JIRA_EMAIL` when using bearer/PAT |
| `HTTP_403` | Permission denied | verify the token and resource permissions |
| `HTTP_404` | Not found | confirm identifiers and base URL, including any Jira or Confluence context path |
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

---

## User-facing response rules

When you report back to the user:

- summarize the outcome in plain language,
- mention the affected Jira issues or Confluence pages,
- describe what changed or what blocked the change,
- never paste tokens, raw JQL, raw JSON envelopes, or storage XHTML unless the user explicitly asks.

Examples:

- single Jira issue: "PROJ-123 is In Progress, assigned to Alex, priority High."
- Jira search: "Found 12 open bugs. The most urgent are PROJ-1, PROJ-7, and PROJ-11."
- Confluence page: "The page is in TEAM space and was last updated yesterday by Priya."
- mutation: "Done — I added the label and left everything else unchanged."
- partial failure: "Some items were updated and some were not. I can resume safely from the saved checkpoint — just say the word."
