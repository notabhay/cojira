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

func TestParseUnknownReturnsNil(t *testing.T) {
	assert.Nil(t, parseIntent("do something weird and random"))
}

func TestLooksLikeConfluence(t *testing.T) {
	assert.False(t, looksLikeConfluence("show 12345", "12345"))
	assert.True(t, looksLikeConfluence("show confluence page 12345", "12345"))
	assert.True(t, looksLikeConfluence("show https://confluence.example.com/pages/123", "https://confluence.example.com/pages/123"))
	assert.True(t, looksLikeConfluence("show https://example.com/wiki/spaces/ENG", "https://example.com/wiki/spaces/ENG"))
	assert.False(t, looksLikeConfluence("show PROJ-123", "PROJ-123"))
}
