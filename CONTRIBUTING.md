# Contributing

This repository is small enough that every structural decision matters. The goal is not to create the biggest Atlassian CLI; the goal is to keep `cojira` dependable for agent-driven workflows where mistakes are expensive and debugging is often happening through another tool’s terminal.

## Working Principles

- Keep the command surface explicit. Prefer a small, well-documented command tree over magical behavior.
- Keep mutations previewable. If a command changes Jira or Confluence state, it should be easy to inspect what will happen before applying it.
- Preserve Confluence fidelity. Storage-format XHTML is the source of truth; do not normalize it through Markdown or lossy transforms.
- Keep the dependency footprint lean. Reach for the standard library first.
- Make agent behavior observable. Structured JSON output, stable error codes, and actionable diagnostics are part of the product, not polish.

## Prerequisites

- Go 1.22 or newer for local builds.
- `git` for normal source workflows.
- `golangci-lint` if you want to run the same lint target used in CI locally.
- Jira and Confluence credentials only if you need live end-to-end validation against real services. Most tests are local and use mocked HTTP servers.

## Repository Layout

| Path | Responsibility |
| --- | --- |
| [`main.go`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/main.go) | Root command assembly, dotenv bootstrap, top-level registration, and process exit handling |
| [`install.sh`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/install.sh) | Curl-friendly installer that downloads source, ensures Go exists, builds the binary, and emits bootstrap assets |
| [`internal/cli`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/cli) | Shared Cobra helpers: output-mode normalization, retry flags, plan/idempotency flags, root alias expansion |
| [`internal/meta`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/meta) | Meta commands such as `describe`, `doctor`, `init`, `bootstrap`, `plan`, and `do` |
| [`internal/jira`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/jira) | Jira client, identifier handling, command handlers, sync helpers, and Jira-specific validation |
| [`internal/confluence`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/confluence) | Confluence client, identifier handling, page/tree workflows, XHTML validation, and Confluence batch flows |
| [`internal/board`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/board) | Experimental Jira board configuration flows built on GreenHopper endpoints |
| [`internal/dotenv`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/dotenv) | `.env` parsing, multi-source loading, placeholder detection, and credential provenance |
| [`internal/config`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/config) | `.cojira.json` parsing, alias lookup, and project-default handling |
| [`internal/httpclient`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/httpclient) | Shared retry and backoff behavior |
| [`internal/output`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/output) | Envelope format, JSON helpers, output modes, receipts, and idempotency-key helpers |
| [`internal/errors`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/errors) | Error codes, default messages, hints, and recovery metadata |
| [`internal/idempotency`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/idempotency) | Local idempotency store and replay-prevention helpers |
| [`internal/assets`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/assets) | Embedded bootstrap guide, env template, and example payload files used by `cojira bootstrap` |
| [`dist/`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/dist) | Generated release artifacts |
| [`.github/workflows/ci.yml`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/.github/workflows/ci.yml) | GitHub Actions workflow for vet, race tests, build, and lint |
| [`.goreleaser.yml`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/.goreleaser.yml) | Release packaging and cross-platform build definition |

## Local Development Workflow

The shortest reliable loop is:

```bash
make build
make test
make test-race
make vet
```

Equivalent direct commands:

```bash
go build -o cojira .
go test ./...
go test -race ./...
go vet ./...
```

Optional lint pass:

```bash
make lint
```

When you need a quick smoke test after a change:

```bash
./cojira --help
./cojira describe --output-mode json
```

## CI and Release Behavior

Current automation in this tree is intentionally simple:
- [`.github/workflows/ci.yml`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/.github/workflows/ci.yml) runs `go vet ./...`, `go test -race ./...`, and `go build -o cojira .` on pushes and pull requests targeting `main`.
- The same workflow runs `golangci-lint` in a separate job.
- [`.goreleaser.yml`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/.goreleaser.yml) packages Linux, macOS, and Windows binaries for `amd64` and `arm64`, with `CGO_ENABLED=0`.

One implication matters: if you are working on a long-lived non-`main` branch, local validation is the real gate until CI is wired for that branch.

## Bootstrap Asset Flow

Bootstrap content is a product surface, not a side document.

The runtime flow is:
1. [`install.sh`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/install.sh) downloads source for the requested Git ref and builds `cojira`.
2. The installer runs `cojira bootstrap --output /tmp/cojira/COJIRA-BOOTSTRAP.md --force`.
3. [`internal/meta/cmd_bootstrap.go`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/meta/cmd_bootstrap.go) reads embedded files from [`internal/assets`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/assets) and writes:
   - `COJIRA-BOOTSTRAP.md`
   - `.env.example`
   - `examples/README.md`
   - the example JSON, CSV, and XHTML templates

That means there are two bootstrap-markdown copies that contributors must keep synchronized:
- [`COJIRA-BOOTSTRAP.md`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/COJIRA-BOOTSTRAP.md): the repo-root, human-readable source copy.
- [`internal/assets/COJIRA-BOOTSTRAP.md`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/assets/COJIRA-BOOTSTRAP.md): the embedded runtime copy used by `cojira bootstrap`.

If you change the bootstrap guide, update both in the same change and verify they are byte-for-byte identical.

There is also an env-template split to be aware of:
- [`/.env.example`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/.env.example) is the checked-in repo copy people see while working in source.
- [`internal/assets/env.example`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/assets/env.example) is what `cojira bootstrap` writes into a target workspace as `.env.example`.

Keep those aligned in intent even though the filenames differ.

## How To Add or Change a Command

### 1. Put the command in the right package

- Jira commands belong under [`internal/jira`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/jira).
- Confluence commands belong under [`internal/confluence`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/confluence).
- Setup, planning, diagnostics, and agent-helper flows belong under [`internal/meta`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/meta).
- Board-configuration commands that depend on GreenHopper belong under [`internal/board`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/board).

### 2. Follow the existing command shape

Typical pattern:
- `New...Cmd()` returns a `*cobra.Command`.
- The command reads configuration from env and flags using shared helpers.
- JSON output goes through [`internal/output`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/output).
- Structured failures use [`internal/errors`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/internal/errors).

Useful helpers:
- `cli.AddOutputFlags(...)`
- `cli.AddHTTPRetryFlags(...)`
- `cli.AddIdempotencyFlags(...)`
- `cli.NormalizeOutputMode(...)`
- `output.BuildEnvelope(...)`

### 3. Register it explicitly

- Register Jira and Confluence commands in their package-level `commands.go`.
- Register top-level meta commands in [`main.go`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/main.go).
- Board commands are attached to the Jira command tree from [`main.go`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/main.go), not as a separate root tool.

### 4. Preserve product conventions

- Prefer flexible identifiers over forcing users to normalize URLs into IDs.
- Prefer explicit preview or validation surfaces on mutating flows.
- Confluence page commands must preserve storage-format fidelity.
- Raw API commands should stay narrow and allowlisted.
- If a command is experimental or relies on internal Atlassian APIs, say so in help text and docs.

### 5. Test the actual behavior

The existing repo leans on package-local command tests and mocked HTTP servers. Match that style:
- Add tests in the same package when changing a command handler.
- Cover both `--output-mode json` and human/summary output when behavior differs.
- Prefer narrow regression tests for parsing, validation, and output contracts.

### 6. Update docs in the same change

A command-surface change is not done until you update:
- [`README.md`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/README.md) if the top-level feature set changed,
- [`COJIRA-BOOTSTRAP.md`](/Users/abhay.a.sriwastawa/Documents/projects/cojira/COJIRA-BOOTSTRAP.md) and its embedded copy,
- and any agent-facing guidance that mentions the old behavior.

## Architectural Notes Worth Preserving

- The CLI root is intentionally thin. It wires commands together and delegates real behavior into internal packages.
- The output envelope is part of the external contract. Be careful when changing field names or semantics.
- `dotenv` does more than load env vars: it also tracks provenance for diagnostics and agent introspection.
- `describe` is not marketing copy. It is a machine-readable capability manifest and should stay grounded in the real command tree.
- `install.sh` is a first-class distribution path. If a packaging change breaks the curl install flow, that is a user-facing regression.

## Documentation Expectations

Documentation quality matters because `cojira` is frequently bootstrapped through another agent.

When you update docs:
- Prefer concrete examples over generic claims.
- Distinguish supported, experimental, and unsupported capabilities clearly.
- Do not document files or generated assets that do not exist.
- Keep the bootstrap guide self-contained: an agent reading only that file should understand install, configuration, safety rules, command coverage, and recovery behavior.
