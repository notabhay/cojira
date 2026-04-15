# COJIRA Bootstrap (Agent Guide)

This file is intended to be dropped into a workspace and used by a coding agent as the
single source of truth for bootstrapping `cojira` locally.

The user is non-technical. Keep their experience simple. You do the operational work.

## Core rules

### Local-only bootstrap

- If the workspace already contains a `cojira` release bundle or a local `cojira` binary, use that.
- Do **not** use `curl`, `git clone`, or any network install path during bootstrap when local bundle files are available.
- Prefer the bundled `install.sh` or bundled binary over any remote installer.

### Credentials

- Do **not** ask the user to paste credentials, tokens, or secret URLs into chat.
- Ask the user to manually edit `.env` in the workspace.
- Never print token values or echo `.env` contents back to the user.
- After the user confirms `.env` is ready, copy the `JIRA_*` and `CONFLUENCE_*` entries into `${XDG_CONFIG_HOME:-$HOME/.config}/cojira/credentials`.

### Workspace prompts

- Ensure this workspace has `AGENTS.md` and `CLAUDE.md` guidance for `cojira`.
- If those files already exist, preserve existing content.
- Only add or update the managed `cojira` block. Do not overwrite unrelated instructions.

### Safety

- Preview high-blast-radius changes with `cojira plan ...` or `--dry-run` first.
- Require **double confirmation** before dangerous actions.
- Dangerous actions include:
  - bulk Jira mutations,
  - `delete-missing` board apply flows,
  - board configuration changes,
  - archive / copy-tree operations,
  - editing global credentials,
  - removing files,
  - modifying workspace prompt files.

### User communication

- Hide CLI syntax, raw JSON, JQL, XHTML, exit codes, and stack traces from the user.
- Summarize results in plain language.
- Confluence content is storage-format XHTML. Preserve `<ac:...>` and `<ri:...>` macros.

## Bootstrap flow

### 1. Install locally

If `cojira` is already installed and available on `PATH`, skip to step 2.

If this workspace contains a bundled `install.sh`, run it from this workspace. It should install the
bundled binary locally, create `.env`, merge `AGENTS.md` and `CLAUDE.md`, and then clean up the bundle
artifacts so the workspace is left with only:

- `.env`
- `AGENTS.md`
- `CLAUDE.md`

Verify:

```bash
cojira --version
cojira --help
```

### 2. Ask the user to edit `.env`

Send a message like:

> "Everything is installed locally.
>
> Please open the `.env` file in this workspace and fill in your Jira and Confluence Personal
> Access Tokens. The base URLs are already filled in for you. Please don't paste anything here in
> chat. Tell me when you've finished and I'll verify the setup."

The bundled installer should already have created `.env` for the user. If it somehow does not exist,
create it before continuing.

### 3. Token creation pages

If the user needs to create tokens, send them to:

- Jira: `https://jira.rakuten-it.com/jira/secure/ViewProfile.jspa?selectedTab=com.atlassian.pats.pats-plugin:jira-user-personal-access-tokens`
- Confluence: `https://confluence.rakuten-it.com/confluence/plugins/personalaccesstokens/usertokens.action`

Do not ask them to paste the resulting tokens into chat. They should put them directly into `.env`.

### 4. Validate the workspace `.env`

After the user says they have updated `.env`:

1. Read `.env`.
2. Confirm the required keys exist and do not still contain placeholder values.
3. If something is missing, tell the user exactly which key still needs to be filled in.
4. Do not print the values back.

Expected keys:

- `CONFLUENCE_BASE_URL` should already be prefilled as `https://confluence.rakuten-it.com/confluence/`
- `JIRA_BASE_URL` should already be prefilled as `https://jira.rakuten-it.com/jira`
- The user only needs to fill:
  - `CONFLUENCE_API_TOKEN`
  - `JIRA_API_TOKEN`

Only ask the user to change the base URLs if this workspace is being used against a different Jira or
Confluence instance.

### 5. Sync credentials globally

Once `.env` is valid, copy the `JIRA_*` and `CONFLUENCE_*` keys into the global credentials file:

```bash
mkdir -p "${XDG_CONFIG_HOME:-$HOME/.config}/cojira"
```

Write or update:

```text
${XDG_CONFIG_HOME:-$HOME/.config}/cojira/credentials
```

Rules:

- Preserve unrelated existing keys.
- Update only the `JIRA_*` and `CONFLUENCE_*` entries from the workspace `.env`.
- Write the file with mode `0600`.
- Do not remove the workspace `.env`.

### 6. Verify setup

Run:

```bash
cojira doctor
cojira describe --with-context --output-mode json
```

Success criteria:

- `cojira doctor` passes for the configured systems.
- `cojira describe --with-context --output-mode json` reports `setup_needed: false`.

If Jira or Confluence still fails:

- Check the base URL first.
- Jira Server/Data Center may need a `/jira` context path.
- Confluence Server/Data Center may need a `/confluence` context path.
- If the user uses a PAT, avoid setting `JIRA_EMAIL` unless they intentionally need basic auth.

### 7. Ready message

Once setup passes, send a short non-technical message like:

> "Everything's set up and ready.
>
> I can now help with things like:
> - checking ticket status,
> - searching Jira,
> - updating fields and transitions,
> - reading and updating Confluence pages,
> - showing board issues,
> - and handling bulk changes safely.
>
> Just paste a link or tell me what you want changed."

## Ongoing operating rules

### First call in a new session

```bash
cojira describe --with-context --output-mode json
```

### Preferred setup path

- Workspace `.env` is the human-edit surface.
- Global credentials file is the convenience copy for reuse in other workspaces.
- `cojira init` remains available for real interactive terminals, but it is **not** the preferred
  agent-led onboarding path.

### Output modes

- `summary`: concise user-facing reads
- `json`: structured follow-up work
- `human`: operator-oriented output

### Common safe defaults

- Use `--output-mode summary` for reads when you just need to answer the user.
- Use `--output-mode json` when chaining follow-up work.
- Use `--dry-run` or `cojira plan ...` before mutating many items or touching dangerous surfaces.

### Dangerous action confirmation policy

Before executing a dangerous action:

1. Explain what will change in plain language.
2. Ask for a first confirmation.
3. If the user confirms, restate the blast radius briefly.
4. Ask for a second explicit confirmation.
5. Only then proceed.

### Capability reminders

`cojira` supports:

- Jira issue reads/search/update/create/transition/batch/bulk sync flows
- Jira board issue listing
- experimental Jira board swimlane and detail-view configuration
- Confluence page reads/search/tree/create/update/rename/move/archive/copy-tree/batch

It does **not** support:

- Jira comments, attachments, issue links, sprint admin, dashboards, delete issue
- Confluence comments, attachments, permissions admin, delete page, export to PDF/Word

### Workspace cleanup

For the local bundle flow, the installer should already remove the one-off bundle artifacts from the
workspace. The workspace should be left with `.env`, `AGENTS.md`, and `CLAUDE.md`.
