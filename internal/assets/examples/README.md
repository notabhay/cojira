# Example Files

Ready-to-use templates for cojira operations.

## Jira

| File | Description |
|------|-------------|
| `jira-create-payload.json` | Create a new Jira issue with common fields |
| `jira-create-template.json` | Reusable Jira create template with `${VAR}` placeholders |
| `jira-update-payload.json` | Update issue fields (summary, description, labels, priority) |
| `jira-batch-config.json` | Batch v2 config with create capture, inline create, and captured-key update |
| `jira-bulk-summaries.json` | JSON mapping for bulk summary updates |
| `jira-bulk-summaries.csv` | CSV mapping for bulk summary updates |

## Project defaults

| File | Description |
|------|-------------|
| `cojira-project.json` | Example `.cojira.json` with default project/space settings |

## Confluence

| File | Description |
|------|-------------|
| `confluence-batch-config.json` | Batch config for update/rename/move operations |
| `confluence-page-content.html` | Sample page in storage format with common macros |

## Usage

Copy and modify these files for your needs:

```bash
# Jira: Create an issue
cp examples/jira-create-payload.json my-issue.json
# Edit my-issue.json with your values
cojira jira create my-issue.json

# Jira: Quick-create an issue without a payload file
cojira jira create --project PROJ --type Task --summary "Quick issue"

# Jira: Use a template
cojira jira create --template examples/jira-create-template.json --var PROJECT=PROJ --var SUMMARY="Templated issue" --var TYPE=Task

# Confluence: Update a page
cp examples/confluence-page-content.html page.html
# Edit page.html
cojira confluence update 12345 page.html
```
