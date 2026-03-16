# cojira

Agent-first CLI toolkit for Confluence and Jira automation.

`cojira` is built for automation-first workflows: structured JSON envelopes, dry-run support, flexible Jira/Confluence identifiers, and command surfaces that are designed to be driven by humans or agents.

## Highlights

- Jira: create, read, update, delete, transition, search, batch, sync, and bulk operations
- Confluence: read, update, create, move, archive, validate, batch, and tree/copy workflows
- Safety: `--dry-run`, idempotency keys on mutating flows, structured errors, and credential provenance
- Experimental board tooling: swimlane and issue-detail-view commands for Jira Software boards

## Experimental board note

The board-detail-view and board-swimlanes commands use internal GreenHopper APIs. They are intentionally marked experimental and may break after Jira upgrades.

## Install

Preferred (no git/Go): follow `COJIRA-BOOTSTRAP.md` → "Install with curl".

Alternative (requires Go 1.22+):

```bash
go build -o cojira .
```

## Setup

```bash
./cojira init
```

## Development

```bash
go test ./...
go test -race ./...
go vet ./...
```

## Docs

- [AGENTS.md](AGENTS.md): agent-facing usage guide and intent phrasebook
- [CLAUDE.md](CLAUDE.md): Claude-oriented companion guide
- [COJIRA-BOOTSTRAP.md](COJIRA-BOOTSTRAP.md): install and setup bootstrap material
