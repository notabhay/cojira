package meta

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestParseWhoami(t *testing.T) {
	assert.Equal(t, []string{"jira", "whoami"}, parseIntent("who am i"))
	assert.Equal(t, []string{"jira", "whoami"}, parseIntent("whoami"))
}

func TestParseTransition(t *testing.T) {
	result := parseIntent("move PROJ-123 to Done")
	assert.Equal(t, []string{"jira", "transition", "PROJ-123", "--to", "Done", "--dry-run"}, result)
}

func TestParseTransitionQuotedStatus(t *testing.T) {
	result := parseIntent(`transition PROJ-123 to "In Progress"`)
	assert.Equal(t, []string{"jira", "transition", "PROJ-123", "--to", "In Progress", "--dry-run"}, result)
}

func TestParseCloseSynonyms(t *testing.T) {
	assert.Equal(t, []string{"jira", "transition", "PROJ-123", "--to", "Closed", "--dry-run"}, parseIntent("close PROJ-123"))
	assert.Equal(t, []string{"jira", "transition", "PROJ-123", "--to", "Done", "--dry-run"}, parseIntent("finish PROJ-123"))
	assert.Equal(t, []string{"jira", "transition", "PROJ-123", "--to", "In Review", "--dry-run"}, parseIntent(`resolve PROJ-123 to "In Review"`))
}

func TestParseAssign(t *testing.T) {
	result := parseIntent("assign PROJ-123 to me")
	assert.Equal(t, []string{"jira", "assign", "PROJ-123", "me", "--dry-run"}, result)
}

func TestParseComment(t *testing.T) {
	result := parseIntent(`comment on PROJ-123 "Please take a look"`)
	assert.Equal(t, []string{"jira", "comment", "PROJ-123", "--add", "Please take a look", "--dry-run"}, result)
}

func TestParseConfluenceComment(t *testing.T) {
	result := parseIntent(`comment on page 6573916430 "Please review this page"`)
	assert.Equal(t, []string{"confluence", "comment", "6573916430", "--add", "Please review this page", "--dry-run"}, result)
}

func TestParseWatchAndWorklog(t *testing.T) {
	assert.Equal(t, []string{"jira", "watchers", "PROJ-123", "--add", "me", "--dry-run"}, parseIntent("watch PROJ-123"))
	assert.Equal(t, []string{"jira", "worklog", "PROJ-123", "--add", "--time-spent", "2h", "--dry-run"}, parseIntent("log 2h on PROJ-123"))
}

func TestParseAdditionalClaudeIntents(t *testing.T) {
	assert.Equal(t, []string{"jira", "link", "PROJ-1", "PROJ-2", "--type", "blocks", "--dry-run"}, parseIntent("link PROJ-1 blocks PROJ-2"))
	assert.Equal(t, []string{"jira", "sprint", "add-issues", "42", "PROJ-1", "--dry-run"}, parseIntent("add PROJ-1 to sprint 42"))
	assert.Equal(t, []string{"jira", "update", "PROJ-1", "--set", "Story Points=5", "--dry-run"}, parseIntent("set story points on PROJ-1 to 5"))
	assert.Equal(t, []string{"jira", "report", "sprint", "123"}, parseIntent("show sprint progress for board 123"))
	assert.Equal(t, []string{"jira", "blocked", "PROJ-1"}, parseIntent("what's blocking PROJ-1?"))
	assert.Equal(t, []string{"jira", "transition", "PROJ-1", "--to", "Reopened", "--dry-run"}, parseIntent("reopen PROJ-1"))
	assert.Equal(t, []string{"jira", "bulk-transition", "--jql", "sprint = 42 AND statusCategory = Done", "--to", "Closed", "--dry-run"}, parseIntent("bulk close all done issues in sprint 42"))
}

func TestParseCreateIssue(t *testing.T) {
	result := parseIntent(`create bug in RAPTOR titled "Fix login bug"`)
	assert.Equal(t, []string{"jira", "create", "--summary", "Fix login bug", "--dry-run", "--project", "RAPTOR", "--type", "Bug"}, result)
}

func TestParseCreateConfluenceContent(t *testing.T) {
	assert.Equal(t,
		[]string{"confluence", "create", "Architecture Overview", "--plan", "--space", "CAIS", "--parent", "6573916430"},
		parseIntent(`create confluence page titled "Architecture Overview" in CAIS under 6573916430`),
	)
	assert.Equal(t,
		[]string{"confluence", "blog", "create", "April Release Notes", "--plan", "--space", "CAIS"},
		parseIntent(`create blog post titled "April Release Notes" in CAIS`),
	)
}

func TestParseBoardQueries(t *testing.T) {
	assert.Equal(t, []string{"jira", "board-view", "12345", "--all"}, parseIntent("show board 12345"))
	assert.Equal(t, []string{"jira", "boards", "--type", "scrum"}, parseIntent("list scrum boards"))
}

func TestParseHistoryAndClone(t *testing.T) {
	assert.Equal(t, []string{"jira", "history", "PROJ-123"}, parseIntent("show history for PROJ-123"))
	assert.Equal(t, []string{"jira", "clone", "PROJ-123", "--dry-run", "--project", "RAPTOR"}, parseIntent("clone PROJ-123 to RAPTOR"))
}

func TestParseDeleteAndArchive(t *testing.T) {
	assert.Equal(t, []string{"jira", "delete", "PROJ-123", "--dry-run"}, parseIntent("delete issue PROJ-123"))
	assert.Equal(t, []string{"confluence", "delete", "6573916430", "--dry-run"}, parseIntent("delete page 6573916430"))
	assert.Equal(t, []string{"confluence", "archive", "6573916430", "--to-parent", "6573916431", "--dry-run", "--label", "archived"}, parseIntent("archive page 6573916430 under 6573916431 label archived"))
}

func TestParseDiff(t *testing.T) {
	assert.Equal(t, []string{"jira", "diff", "PROJ-123", "--from-history", "100", "--to-history", "200"}, parseIntent("diff PROJ-123 history 100 to 200"))
}

func TestParseShowJira(t *testing.T) {
	result := parseIntent("show PROJ-123")
	assert.Equal(t, []string{"jira", "info", "PROJ-123"}, result)
}

func TestParseShowNumericDefaultsToJira(t *testing.T) {
	result := parseIntent("show 12345")
	assert.Equal(t, []string{"jira", "info", "12345"}, result)
}

func TestParseShowConfluencePageNumeric(t *testing.T) {
	result := parseIntent("show page 12345")
	assert.Equal(t, []string{"confluence", "info", "12345"}, result)
}

func TestParseSearch(t *testing.T) {
	result := parseIntent("search FOO for open bugs")
	assert.NotNil(t, result)
	assert.Equal(t, "jira", result[0])
	assert.Equal(t, "search", result[1])
	assert.Contains(t, result[2], "FOO")
	assert.Contains(t, result[2], "open bugs")
}

func TestParseUpdateSet(t *testing.T) {
	result := parseIntent("update PROJ-123 set summary to New title")
	assert.Equal(t, []string{"jira", "update", "PROJ-123", "--set", "summary=New title", "--dry-run"}, result)
}

func TestParseFindPages(t *testing.T) {
	result := parseIntent("find pages titled Architecture")
	assert.Equal(t, []string{"confluence", "find", "Architecture"}, result)
}

func TestParseAddLabel(t *testing.T) {
	result := parseIntent("add label urgent to PROJ-123")
	assert.Equal(t, []string{"jira", "update", "PROJ-123", "--set", "labels+=urgent", "--dry-run"}, result)
}

func TestParseConfluenceLabels(t *testing.T) {
	assert.Equal(t, []string{"confluence", "labels", "6573916430", "--add", "important", "--plan"}, parseIntent("add label important to page 6573916430"))
	assert.Equal(t, []string{"confluence", "labels", "6573916430", "--remove", "important", "--plan"}, parseIntent("remove label important from page 6573916430"))
}

func TestParseUnknownReturnsNil(t *testing.T) {
	assert.Nil(t, parseIntent("do something weird and random"))
}

func TestSuggestIntent(t *testing.T) {
	assert.Contains(t, suggestIntent("create something"), "jira create --project <KEY> --summary \"...\"")
	assert.Contains(t, suggestIntent("show me a board"), "jira board-view <BOARD> --all")
	assert.Contains(t, suggestIntent("clone this issue"), "jira clone <ISSUE> [--project <KEY>]")
	assert.Contains(t, suggestIntent("show me a diff"), "jira diff <ISSUE> --from-history <ID> [--to-history <ID>]")
	assert.Contains(t, suggestIntent("archive this page"), "confluence archive <PAGE> --to-parent <PAGE> --dry-run")
	assert.Contains(t, suggestIntent("delete something"), "jira delete <ISSUE> --dry-run")
}

func TestLooksLikeConfluence(t *testing.T) {
	assert.False(t, looksLikeConfluence("show 12345", "12345"))
	assert.True(t, looksLikeConfluence("show confluence page 12345", "12345"))
	assert.True(t, looksLikeConfluence("show https://confluence.example.com/pages/123", "https://confluence.example.com/pages/123"))
	assert.True(t, looksLikeConfluence("show https://example.com/wiki/spaces/ENG", "https://example.com/wiki/spaces/ENG"))
	assert.False(t, looksLikeConfluence("show PROJ-123", "PROJ-123"))
}
