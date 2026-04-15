# Agent Instructions (cojira)

`cojira` is already installed and configured. Use this file for ongoing Jira and Confluence work after setup.

The user is non-technical. You run `cojira`. The user should only see plain-language summaries.

## Session start

If you have not used `cojira` in this session yet, start with:

```bash
cojira describe --with-context --output-mode json
```

This tells you:

- whether setup is still valid,
- which tools are configured,
- who the current authenticated users are.

## User-facing rules

- Never show CLI commands, flags, JQL, XHTML, exit codes, or raw JSON to the user.
- Never print or paste tokens.
- Never ask the user to paste credentials into chat.
- If setup is unexpectedly missing, ask the user to edit `.env` directly and then sync the confirmed `JIRA_*` and `CONFLUENCE_*` keys to `~/.config/cojira/credentials`.
- Summarize results in plain language.

## Output mode defaults

- Use `--output-mode summary` for read operations when you just need a concise answer.
- Use `--output-mode json` when you need structured data for a follow-up action.
- Human mode is fine for operator work, previews, and local inspection.

## Safety rules

- Confluence page bodies are storage-format XHTML. Never convert them through Markdown.
- Preserve all `<ac:...>` and `<ri:...>` macros.
- Use `cojira plan ...` or `--dry-run` before bulk or high-blast-radius changes.
- Require **double confirmation** before dangerous actions.

Dangerous actions include:

- bulk Jira mutations,
- `delete-missing` apply flows,
- experimental board configuration changes,
- archive or copy-tree operations,
- global credential edits,
- deleting files,
- modifying workspace prompt files.

## Common operating pattern

1. Inspect first.
2. Preview changes.
3. Apply only after confirmation.
4. Verify the final state.

Examples:

- Jira update flow: `info/get` -> `update --dry-run` -> apply -> verify.
- Jira transition flow: `transitions` or `transition --to ... --dry-run` -> apply -> verify.
- Confluence edit flow: `info/get` -> edit storage XHTML -> `update` -> verify.

## Supported areas

### Jira

Primary commands:

- `info`
- `get`
- `search`
- `fields`
- `whoami`
- `create`
- `update`
- `transition`
- `transitions`
- `board-issues`
- `batch`
- `bulk-update`
- `bulk-transition`
- `bulk-update-summaries`
- `sync`
- `sync-from-dir`

Experimental board commands:

- `board-detail-view`
- `board-swimlanes`

### Confluence

Primary commands:

- `info`
- `get`
- `find`
- `tree`
- `create`
- `update`
- `rename`
- `move`
- `archive`
- `copy-tree`
- `validate`
- `batch`

## Not supported

### Jira

- comments,
- watchers,
- issue links,
- attachments,
- delete issue,
- worklogs,
- sprints,
- board columns,
- dashboards,
- project administration.

### Confluence

- comments,
- attachments,
- permissions administration,
- delete page,
- labels as a standalone command,
- page history diffing,
- templates,
- blog posts,
- PDF or Word export.

## Identifier shortcuts

### Jira issues

Accepted forms:

- `PROJ-123`
- numeric issue ID
- Jira browse URL
- Jira REST issue URL

### Jira boards

Accepted forms:

- numeric board ID
- RapidView URL

### Confluence pages

Accepted forms:

- numeric page ID
- page URL
- tiny link code
- `SPACE:"Page Title"`

## Good defaults

- Use full URLs when identifier resolution may be ambiguous.
- For searches, prefer summaries unless you need structured follow-up.
- For Confluence updates, fetch and edit the storage XHTML directly.
- For bulk Jira work, add modest throttling when appropriate.

## If setup breaks

Do this in plain language:

- ask the user to edit `.env` directly,
- do not ask them to paste anything in chat,
- verify the required keys exist,
- sync the confirmed values to `~/.config/cojira/credentials`,
- rerun `cojira doctor`,
- rerun `cojira describe --with-context --output-mode json`.
