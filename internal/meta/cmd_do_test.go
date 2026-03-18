package meta

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseWhoami(t *testing.T) {
	result, err := parseIntent("who am i")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "whoami"}, result)

	result, err = parseIntent("whoami")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "whoami"}, result)
}

func TestParseTransition(t *testing.T) {
	result, err := parseIntent("move PROJ-123 to Done")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "transition", "PROJ-123", "--to", "Done", "--dry-run"}, result)
}

func TestParseTransitionQuotedStatus(t *testing.T) {
	result, err := parseIntent(`transition PROJ-123 to "In Progress"`)
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "transition", "PROJ-123", "--to", "In Progress", "--dry-run"}, result)
}

func TestParseShowJira(t *testing.T) {
	result, err := parseIntent("show PROJ-123")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "info", "PROJ-123", "--output-mode", "summary"}, result)
}

func TestParseShowConfluenceNumeric(t *testing.T) {
	result, err := parseIntent("show 12345")
	require.NoError(t, err)
	assert.Equal(t, []string{"confluence", "info", "12345", "--output-mode", "summary"}, result)
}

func TestParseFindOpenBugs(t *testing.T) {
	result, err := parseIntent("find all open bugs in FOO")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "search", "project = FOO AND type = Bug AND status != Done", "--output-mode", "summary"}, result)
}

func TestParseUpdateSet(t *testing.T) {
	result, err := parseIntent("add label urgent to PROJ-123")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "update", "PROJ-123", "--set", "labels+=urgent", "--dry-run"}, result)
}

func TestParseFindPages(t *testing.T) {
	result, err := parseIntent("find pages titled Architecture")
	require.NoError(t, err)
	assert.Equal(t, []string{"confluence", "find", "Architecture", "--output-mode", "summary"}, result)
}

func TestParseFieldsWithoutQuery(t *testing.T) {
	result, err := parseIntent("what fields are available?")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "fields"}, result)
}

func TestParseRecognizedButInsufficientIntent(t *testing.T) {
	_, err := parseIntent("change priority to High")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "need a Jira issue key")
}

func TestParseCreateIssueTitled(t *testing.T) {
	result, err := parseIntent("create a new issue in PROJ titled Investigate login bug")
	require.NoError(t, err)
	assert.Equal(t, []string{"jira", "create", "--project", "PROJ", "--type", "Task", "--summary", "Investigate login bug", "--dry-run"}, result)
}

func TestParseReadConfluencePageUsesViewText(t *testing.T) {
	result, err := parseIntent("read this confluence page 12345")
	require.NoError(t, err)
	assert.Equal(t, []string{"confluence", "view", "12345", "--format", "text", "--output-mode", "json"}, result)
}

func TestParseRecognizedUnsupportedIntent(t *testing.T) {
	_, err := parseIntent("add a comment to PROJ-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not supported")
}

func TestParseUnknownReturnsError(t *testing.T) {
	_, err := parseIntent("do something weird and random")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Could not parse intent")
}

func TestLooksLikeConfluence(t *testing.T) {
	assert.True(t, looksLikeConfluence("12345"))
	assert.True(t, looksLikeConfluence("https://confluence.example.com/pages/123"))
	assert.True(t, looksLikeConfluence("https://example.com/wiki/spaces/ENG"))
	assert.False(t, looksLikeConfluence("PROJ-123"))
}
