package markdownconv

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToConfluenceStorage(t *testing.T) {
	input := "# Release Notes\n\n- Item one\n- Item two\n\nUse `cojira`.\n\n[Docs](https://example.com)"

	output, err := ToConfluenceStorage(input)
	require.NoError(t, err)

	assert.Contains(t, output, "<h1>Release Notes</h1>")
	assert.Contains(t, output, "<li>Item one</li>")
	assert.Contains(t, output, "<code>cojira</code>")
	assert.Contains(t, output, `<a href="https://example.com">Docs</a>`)
}

func TestToJiraWiki(t *testing.T) {
	input := "# Release Notes\n\n- Item one\n- Item two\n\nUse `cojira`.\n\n[Docs](https://example.com)"

	output, err := ToJiraWiki(input)
	require.NoError(t, err)

	assert.Contains(t, output, "h1. Release Notes")
	assert.Contains(t, output, "* Item one")
	assert.Contains(t, output, "* Item two")
	assert.Contains(t, output, "{{cojira}}")
	assert.Contains(t, output, "[Docs|https://example.com]")
}

func TestToJiraWikiTableAndCodeFence(t *testing.T) {
	input := "## Data\n\n| Name | Value |\n| --- | --- |\n| A | 1 |\n\n```json\n{\"ok\":true}\n```"

	output, err := ToJiraWiki(input)
	require.NoError(t, err)

	assert.Contains(t, output, "h2. Data")
	assert.Contains(t, output, "||Name||Value||")
	assert.Contains(t, output, "|A|1|")
	assert.True(t, strings.Contains(output, "{code:json}") || strings.Contains(output, "{code}"))
	assert.Contains(t, output, "{\"ok\":true}")
}

func TestToJiraADF(t *testing.T) {
	input := "# Release Notes\n\n- Item one\n- Item two\n\nUse `cojira` and [Docs](https://example.com)"

	doc, err := ToJiraADF(input)
	require.NoError(t, err)

	content := doc["content"].([]map[string]any)
	require.NotEmpty(t, content)
	assert.Equal(t, "heading", content[0]["type"])
	assert.Equal(t, "bulletList", content[1]["type"])
	assert.Equal(t, "paragraph", content[2]["type"])
}

func TestPlainTextToJiraADF(t *testing.T) {
	doc := PlainTextToJiraADF("Line one\nLine two")
	content := doc["content"].([]map[string]any)
	require.Len(t, content, 1)
	paragraph := content[0]
	assert.Equal(t, "paragraph", paragraph["type"])
	inline := paragraph["content"].([]map[string]any)
	assert.Len(t, inline, 3)
	assert.Equal(t, "hardBreak", inline[1]["type"])
}
