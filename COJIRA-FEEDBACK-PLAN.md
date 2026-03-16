# cojira Improvement Plan from Claude Code Feedback

## Goal

Use the friction from the transcript to improve `cojira` as an agent-first CLI, with priority on the gaps that blocked real work:

1. Missing Confluence comment support
2. Opaque credential source resolution
3. No authenticated escape hatch for unsupported Confluence APIs
4. Thin Confluence page metadata
5. Poor read-only ergonomics for page inspection

This plan is based on the current Go repo, not the archived implementation.

## Current State

- Confluence commands are limited to page CRUD/tree/batch operations; there is no `comments` or generic API subcommand in [internal/confluence/commands.go](internal/confluence/commands.go).
- `confluence get` is hard-coded to fetch `body.storage` only in [internal/confluence/cmd_get.go](internal/confluence/cmd_get.go).
- `confluence info` is hard-coded to fetch only `version,space,ancestors,children.page` in [internal/confluence/cmd_info.go](internal/confluence/cmd_info.go).
- `describe` explicitly says Confluence comments are unsupported in [internal/meta/cmd_describe.go](internal/meta/cmd_describe.go).
- Env loading provenance now surfaces through [internal/dotenv/dotenv.go](internal/dotenv/dotenv.go), [internal/meta/cmd_doctor.go](internal/meta/cmd_doctor.go), and [internal/meta/cmd_describe.go](internal/meta/cmd_describe.go).
- The dotenv loader is now merge-based across the default search paths: inherited shell environment wins, local `.env` fills missing keys next, and global credentials fill any remaining missing keys.
- The test suite covers client primitives and dotenv behavior reasonably well, but command-level Confluence tests are thin or absent for `get` and `info`.

## Product Direction

Adopt Claude's feedback selectively, not literally.

- The request for a readable Confluence format is valid.
- The specific suggestion to make Markdown a `get` format conflicts with `cojira`'s storage-first safety model.
- `get` should remain the canonical storage/XHTML command for edit workflows.
- Readable inspection should be added as a separate read-only path or an explicit alternate representation that cannot be confused with editable source.

## Priority 0: Fix Configuration Provenance UX

### Problem

Claude lost time because `cojira` was functioning with live credentials, while the visible global credentials file contained placeholders. The tool gave no indication of where working credentials came from.

The root cause was not only precedence. It was also the previous loader behavior: search paths were evaluated in order, the first existing file won, and later files were ignored entirely even if they contained different or missing keys. The current implementation now merges per key and reports provenance.

### Scope

- Track env provenance during load:
  - inherited shell environment
  - local `.env`
  - global credentials file
- Surface provenance in:
  - `doctor`
  - `describe --with-context`
  - optionally `describe` JSON even without live checks

### Implementation

- This landed by replacing the current string-returning loader in [internal/dotenv/dotenv.go](internal/dotenv/dotenv.go) with a richer result struct:
  - loaded path
  - candidate paths checked
  - env keys set
  - per-key source
- Thread that metadata into [internal/meta/cmd_doctor.go](internal/meta/cmd_doctor.go) and [internal/meta/cmd_describe.go](internal/meta/cmd_describe.go).
- Keep precedence explicit and merge-based: inherited shell environment first, then local `.env`, then global credentials.

### Acceptance Criteria

- `doctor` explicitly reports which source supplied Confluence and Jira credentials.
- JSON output exposes source metadata in a stable shape.
- Placeholder values in one source do not obscure the true active source in diagnostics.

## Priority 1: Enrich `confluence info`

### Problem

`info` is too shallow for basic agent reasoning. Claude needed last-modified context and did not get it.

### Scope

- Add these fields to `confluence info`:
  - `last_modified`
  - `last_modified_by`
  - `created_date`
  - `created_by`
- Preserve existing fields and output modes.

### Implementation

- Expand the fetch in [internal/confluence/cmd_info.go](internal/confluence/cmd_info.go) to include `history` and richer `version` data.
- Normalize the JSON envelope so human, summary, and JSON modes all benefit from the same info model.

### Acceptance Criteria

- JSON output contains author/date metadata.
- Summary mode can answer "who last touched this page and when?" without extra API calls.

## Priority 2: Add a Safe Confluence API Escape Hatch

### Problem

When `cojira` does not support an endpoint, agents are forced to leave the authenticated client and reconstruct requests manually. That defeats the point of the CLI.

### Scope

- Add a new authenticated passthrough command under Confluence.
- Start with read-only `GET`.
- Defer mutating passthrough until the UX and safety model are proven.
- Treat this as an immediate unblock, not just infrastructure. Once this exists, agents can read unsupported endpoints such as comments without leaving `cojira`.

### Implementation

- Add a new command, likely `confluence api`, using the existing `Client.Request(...)` plumbing in [internal/confluence/client.go](internal/confluence/client.go).
- Define the accepted path rule explicitly:
  - the CLI accepts an API-relative path beginning with `/`
  - the path must begin with `/content`, `/space`, `/user`, `/search`, or another explicitly allowlisted resource under the Confluence REST API surface
  - the initial allowlist must be written down in code, tests, and help text before release; expanding it later is an explicit product decision, not an ad hoc implementation shortcut
  - the command joins that path against the client's existing REST base instead of accepting arbitrary absolute URLs
- Reject absolute URLs and non-API paths.
- Return raw JSON in JSON mode and a compact summary in summary mode.
- Reuse the existing retry/output flag patterns.

### Why This Comes Before Full Comment Support

This is the fastest way to unblock unsupported read workflows while the dedicated comment UX is still being designed.

### Acceptance Criteria

- A user can fetch `/content/{id}/child/comment?...` through `cojira` without external `curl`.
- Auth, retry, and error formatting match normal `cojira` behavior.

## Priority 3: Add First-Class Confluence Comment Support

### Problem

This was the biggest functional gap in the transcript. Claude had to fetch page storage separately, then call the REST API manually, then correlate inline comment markers by hand.

### Scope

- Add `confluence comments <page>`.
- Support:
  - all comments
  - inline-only comments
  - footer/page comments
- Include:
  - author
  - timestamp
  - rendered comment body
  - anchor context when available

### Implementation

- Add client methods in [internal/confluence/client.go](internal/confluence/client.go) for:
  - page comments via `/content/{id}/child/comment`
  - comment expansion with `body.view`, `version`, and `history`
- For inline comment context:
  - fetch `body.storage` for the page
  - parse `ac:inline-comment-marker` anchors
  - correlate marker refs with comment metadata
- Add a new Cobra command in `internal/confluence`.

### Open Design Question

If the REST payload does not reliably include selected text for inline comments, decide whether `cojira` should:

- return best-effort anchor context from storage markup, or
- explicitly label unresolved anchors instead of guessing

The second option is safer.

### Acceptance Criteria

- The command returns Kent-style inline comments in one call.
- Inline comments include anchor text or an explicit "anchor unresolved" field.
- `describe` no longer lists Confluence comments as unsupported.
- Update the hardcoded unsupported-text block in [internal/meta/cmd_describe.go](internal/meta/cmd_describe.go); this change is manual, not implied by command registration.

## Priority 4: Improve Read-Only Page Inspection Without Breaking Edit Safety

### Problem

`confluence get` returning raw storage XHTML is correct for editing, but poor for reading and summarization.

### Recommendation

Do not turn `get` into a Markdown exporter.

Instead, stage this in two steps:

1. MVP: add `confluence get --representation storage|view`, with `storage` as the default.
2. Enhancement: add `confluence view <page>` later if usage shows that a dedicated read-only command would materially reduce confusion.

### Preferred Direction

Use `--representation` as the MVP and keep a separate read-only command as the cleaner end state if needed.

Reasons:

- It is the lowest-effort path because [internal/confluence/cmd_get.go](internal/confluence/cmd_get.go) already centralizes the fetch behavior.
- It keeps `storage` as the explicit default and recommended edit path.
- It avoids inventing a second command until there is evidence that the single-command UX is too ambiguous.
- A separate `view` command remains available if the mixed representation model proves confusing in practice.

### Acceptance Criteria

- Agents can inspect page content without wading through storage XHTML.
- Edit workflows still begin with storage content, not rendered output.

## Test Plan

Add command-level tests, not just client tests.

### New Tests

- `internal/confluence/cmd_info_test.go`
  - richer metadata fields
  - summary/human formatting
- `internal/confluence/cmd_get_test.go`
  - output modes
  - file output
  - future representation handling
- `internal/confluence/cmd_comments_test.go`
  - inline and footer comment output
  - unresolved anchor behavior
- `internal/confluence/cmd_api_test.go`
  - path validation
  - GET passthrough JSON output
- `internal/meta/cmd_doctor_test.go`
  - credential source reporting
  - precedence cases
- `internal/meta/cmd_describe_test.go`
  - command manifest updates
  - supported/unsupported list changes

## Suggested Delivery Sequence

### Phase 1

- Credential provenance in `doctor` and `describe`
- Richer `confluence info`
- Command-level tests for `doctor`, `describe`, and `info`

Low risk, fast payoff, and immediately improves agent trust.

Phase 1 is not complete until:

- provenance is visible in human and JSON diagnostics
- `info` includes author/date metadata
- the new command-level tests land with the feature work

### Phase 2

- Read-only `confluence api` passthrough
- Command-level tests for the passthrough path-validation and output contract

This gives agents an authenticated escape hatch before all dedicated features exist, including the comment use case.

Phase 2 is not complete until:

- API-relative path validation is enforced
- the concrete allowlist is documented in the command help and covered by tests
- unsupported or absolute paths are rejected clearly
- the command tests cover success and rejection cases

### Phase 3

- First-class `confluence comments`
- Command-level tests for inline/footer comment output and unresolved anchors

This is the highest-value product gap, but it is also the first feature that needs both API design and anchor-correlation behavior.

Phase 3 is not complete until:

- inline and footer comments are both supported
- anchor-correlation behavior is explicit
- the hardcoded unsupported-comments note is removed from `describe`
- command tests cover the supported variants

### Phase 4

- `confluence get --representation ...` MVP, if not already shipped earlier
- Optional dedicated `confluence view` follow-up if needed
- Command-level tests for representation handling

Do this after comments/api if the passthrough and comment work already cover the highest-priority agent gaps. If the team wants the fastest readability win, the `--representation` MVP can move earlier without changing the rest of the plan.

## Recommended Owner Mindset

- Treat comments and raw API as product features, not one-off utilities.
- Treat credential provenance as trust infrastructure.
- Treat rendered output as read-only context, not editable source.

## Success Metric

An agent should be able to handle the exact transcript flow entirely inside `cojira`, without dropping to ad hoc HTTP calls and without ambiguity about which credentials are active.
