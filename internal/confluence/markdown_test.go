package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStorageToMarkdownBasic(t *testing.T) {
	input := `<h1>Title</h1><p>Hello <strong>world</strong>.</p><ul><li>One</li><li>Two</li></ul>`
	output, warnings, err := storageToMarkdown(input)
	require.NoError(t, err)
	assert.Contains(t, output, "# Title")
	assert.Contains(t, output, "Hello **world**.")
	assert.Contains(t, output, "- One")
	assert.Empty(t, warnings)
}

func TestStorageToMarkdownMacroWarning(t *testing.T) {
	input := `<ac:structured-macro ac:name="info"><ac:rich-text-body><p>Hi</p></ac:rich-text-body></ac:structured-macro>`
	output, warnings, err := storageToMarkdown(input)
	require.NoError(t, err)
	assert.Contains(t, output, "[Confluence macro: info]")
	require.Len(t, warnings, 1)
}
