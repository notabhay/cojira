# COJIRA Bootstrap (Agent Guide)

This file is intended to be dropped into a **clean, empty workspace** and used as the single source of truth for getting productive with `cojira`.

If you already installed `cojira`, you can (re)generate this file plus templates by running:

```bash
cojira bootstrap
```

## 0) Rules (follow these always)

### Safety

- Never print or paste tokens. Redact secrets.
- Never commit `.env`.
- Confluence page bodies are **storage-format XHTML**. Do **not** convert to Markdown. Preserve `<ac:...>` and `<ri:...>` macros.
- For Jira bulk/batch updates, always preview with `--dry-run` first.
- Prefer `cojira plan <tool> <cmd> ...` to preview changes when unsure.
- Use **single quotes** around JQL to avoid shell issues with `!=`.

### Agent behavior

- **You (the agent) run `cojira`.** Do not ask the user to run CLI commands; ask for links + desired changes, then do the work.
- **Hide implementation details from the user.** Never show `cojira` commands, CLI output, CLI flags, exit codes, JQL syntax, or XHTML in your messages. Summarize results in plain language.

## 1) Install + verify

If `cojira` is already on your PATH, skip to "Verify" below.

### Install with curl (recommended; no git/Go)

This downloads a source archive and builds/installs to `~/.local/bin/cojira` (or `$GOBIN/cojira`). If Go isn't installed, it downloads a user-local Go toolchain first.

For the current pushed `beta` branch:

```bash
curl -fsSL https://raw.githubusercontent.com/notabhay/cojira/beta/install.sh | COJIRA_REF=refs/heads/beta NODE_TLS_REJECT_UNAUTHORIZED=0 bash
```

If you specifically want the latest tagged release instead:

```bash
curl -fsSL https://raw.githubusercontent.com/notabhay/cojira/v0.1.4/install.sh | NODE_TLS_REJECT_UNAUTHORIZED=0 bash
```

### Install from repo (requires Go 1.22+)

```bash
git clone https://github.com/notabhay/cojira.git /tmp/cojira-build \
  && cd /tmp/cojira-build \
  && go build -o "${GOBIN:-$HOME/.local/bin}/cojira" . \
  && cd - \
  && rm -rf /tmp/cojira-build
```

Make sure `~/.local/bin` (or your `GOBIN`) is on your PATH.

### Verify

```bash
cojira --version
cojira --help
```

## 2) Workspace setup (do once)

Create standard working folders (this is where you save artifacts you download/edit):

```bash
mkdir -p 0-JIRA 1-CONFLUENCE
```

Store Jira issue snapshots under `0-JIRA/` and Confluence page XHTML under `1-CONFLUENCE/`.

### Credentials (preferred: interactive wizard)

`cojira` can load credentials globally from `~/.config/cojira/credentials` (or `$XDG_CONFIG_HOME/cojira/credentials`).

If you don't have tokens yet, create them here:
- Confluence PATs: https://confluence.rakuten-it.com/confluence/plugins/personalaccesstokens/usertokens.action
- Jira PATs: https://jira.rakuten-it.com/jira/secure/ViewProfile.jspa?selectedTab=com.atlassian.pats.pats-plugin:jira-user-personal-access-tokens

The easiest way to set up credentials is the interactive wizard, which auto-detects base URLs and context paths and writes the global credentials file:

```bash
mkdir -p ~/.config/cojira
cojira init --path ~/.config/cojira/credentials
```

Do not paste tokens into chat. Tokens are not echoed in the terminal and are written only to the credentials file.

### Credentials (alternative: manual .env)

```bash
cp -n .env.example .env || true
[ -f .env ] || cat > .env << 'EOF'
# Confluence (required for Confluence commands)
CONFLUENCE_BASE_URL=https://confluence.rakuten-it.com/confluence/
CONFLUENCE_API_TOKEN=...

# Jira (required for Jira commands)
# Important: include the context path if your Jira has one (e.g. /jira)
JIRA_BASE_URL=https://jira.rakuten-it.com/jira
JIRA_API_TOKEN=...

# Optional: enables basic auth instead of bearer (omit for PAT auth)
# JIRA_EMAIL=

# Optional controls
# JIRA_API_VERSION=2
# JIRA_AUTH_MODE=bearer   # or basic
# JIRA_VERIFY_SSL=true
EOF
```

### Verify setup

```bash
cojira doctor
```

If doctor reports errors:
- **404**: Base URL is wrong — likely missing a context path (e.g. `/jira`). Re-run `cojira init`.
- **401 with JIRA_EMAIL set**: If using a Personal Access Token, remove `JIRA_EMAIL` or set `JIRA_AUTH_MODE=bearer`.

`cojira` merges environment from these sources in order:
1) inherited shell environment values always win
2) `./.env` (current working directory) fills any missing keys
3) `~/.config/cojira/credentials` (global credentials file) fills any remaining missing keys

Optional project defaults (recommended): create `.cojira.json` to set defaults like project key, space key, or root page ID.
See `examples/cojira-project.json`.

### Templates

If you ran `cojira bootstrap`, you should have:
- `.env.example` — config template
- `examples/` — ready-to-edit JSON/CSV/XHTML templates

Use these templates as inputs for your own `cojira` runs (agent-only).

## Command reference

For the full command reference, phrasebook, workflows, and troubleshooting, see `AGENTS.md` (or `CLAUDE.md` if you are Claude Code).

## 3) "Vague request" playbook (how to decide what to do)

When the user gives unclear instructions, do this:

1. **Pick the system**:
   - If the user provides a Confluence URL/pageId/tiny link/space:title → use `cojira confluence ...`
   - If the user provides a Jira key/URL/JQL/board link → use `cojira jira ...`
   - If ambiguous, ask 1-3 **simple, non-technical** questions:
     - "Is this a Confluence page or a Jira issue?"
     - "Can you paste the link?"
     - "Want me to show you what would change before I apply it?"

2. **Inspect first, change second**:
   - Confluence: `info` → `get` → edit XHTML → `update` → `info --output-mode json` verify.
   - Jira: `info --output-mode json` / `get -o issue.json` → `update --dry-run` → apply.

3. **Use safe networking flags for automation** (especially bulk ops):
   - Add `--timeout 60` and modest `--retries 5-8`
   - Add `--sleep` for bulk operations when available
   - Add `--debug` when diagnosing retries/timeouts

4. **Common ambiguous requests** (ask clarifying questions before acting):
   - **"Update the release notes"** — Is this a Confluence page or a Jira issue? Ask for a link.
   - **"Clean up the board"** — Could mean archive done issues, re-prioritize, remove stale items, or reconfigure swimlanes. Ask what "clean up" means.
   - **"What's blocking the release?"** — Needs scope: which project? which version? which board? Ask for specifics.
   - **"Make this page match that page"** — Do they want a full copy-tree or selective edits? Ask for both links and clarify intent.
   - **User pastes a URL with no other context** — Detect Jira vs Confluence from the URL pattern, run `info --output-mode summary`, summarize what you see, then ask what they want to do with it.

## 4) Tell the user you're ready (user-facing message)

**Important**: The message you show the user must be simple and non-technical.
Do **not** mention CLI commands, build tools, exit codes, JQL, XHTML, or any implementation details.
The user does not need to know how `cojira` works -- they just need to know what you can do for them.

Keep your setup summary to one short line (e.g., "Everything's set up."), then tell them what you need from them. Here is the **exact tone and style** to use (adapt wording, don't copy verbatim):

---

Everything's set up and ready to go.

Before I can help, I'll need you to add your API tokens to the `.env` file in this workspace. (Don't paste them in chat -- just edit the file directly.)

Once that's done, I can help you with things like:

- **"Update this Confluence page: [paste link]"** -- tell me what to change and I'll handle it.
- **"What's the status of [issue]?"** -- paste a Jira link or issue key.
- **"Move [issue] to Done"** -- I'll preview the change before applying it.
- **"Find all open bugs in [project]"** -- I'll search and summarize what I find.
- **"Show me the board"** -- paste a board link and I'll list the issues.

Just paste a link and tell me what you need!

---

**Rules for this message**:
- Do not list technical steps you performed (install, go build, mkdir, etc.)
- Do not mention Go version, workspace folders, or CLI verification
- Do not use terms like JQL, pageId, tiny link, storage-format, dry-run, XHTML
- Do not offer to "pull things into folders" -- that's an implementation detail
- Keep it conversational, like a helpful colleague
- Use concrete examples with placeholder brackets so the user knows what to paste

### Capability boundaries

If the user asks about something cojira cannot do, tell them clearly and honestly. Examples:

- "I can't add comments to Jira issues yet, but I can update fields like status, labels, and priority."
- "I can't manage sprints or board columns, but I can configure board swimlanes and detail view fields."
- "I can't delete Confluence pages, but I can archive them."
- "I can't upload or download attachments."

Don't pretend capabilities exist. Be honest about boundaries.

### Jira-only variant

If only Jira is configured (Confluence credentials missing or doctor failed for Confluence), use a trimmed readiness message:

---

Everything's set up for Jira.

I can help you with things like:

- **"What's the status of [issue]?"** -- paste a Jira link or issue key.
- **"Move [issue] to Done"** -- I'll preview the change before applying it.
- **"Add label urgent to [issue]"** -- I'll update it for you.
- **"Find all open bugs in [project]"** -- I'll search and summarize.
- **"Show me the board"** -- paste a board link.

Just paste a link and tell me what you need!

---

### Confluence-only variant

If only Confluence is configured, use:

---

Everything's set up for Confluence.

I can help you with things like:

- **"Update this page: [paste link]"** -- tell me what to change and I'll handle it.
- **"Find pages titled [something]"** -- I'll search and list what I find.
- **"Copy this page tree to [parent]"** -- I'll preview before copying.
- **"Show the page hierarchy"** -- paste a link and I'll map it out.

Just paste a link and tell me what you need!

---

## 5) Cleanup

After setup is complete, remove bootstrap artifacts:

```bash
rm -f cojira.zip
```

Keep `COJIRA-BOOTSTRAP.md` if it's tracked in git; otherwise remove it too.
