package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestComputeUnifiedDiffNoChange(t *testing.T) {
	diffText, additions, deletions := computeUnifiedDiff("<p>Hello</p>", "<p>Hello</p>", "12345")
	assert.Equal(t, "", diffText)
	assert.Equal(t, 0, additions)
	assert.Equal(t, 0, deletions)
}

func TestComputeUnifiedDiffReorderedParagraphsPreservesSharedLines(t *testing.T) {
	oldContent := "<p>Alpha</p>\n<p>Beta</p>\n<p>Gamma</p>"
	newContent := "<p>Beta</p>\n<p>Alpha</p>\n<p>Gamma</p>"

	diffText, additions, deletions := computeUnifiedDiff(oldContent, newContent, "12345")

	assert.Contains(t, diffText, "--- 12345.current")
	assert.Contains(t, diffText, "+++ 12345.new")
	assert.Contains(t, diffText, " <p>Beta</p>")
	assert.Contains(t, diffText, " <p>Gamma</p>")
	assert.Contains(t, diffText, "-<p>Alpha</p>")
	assert.Contains(t, diffText, "+<p>Alpha</p>")
	assert.Equal(t, 1, additions)
	assert.Equal(t, 1, deletions)
}

func TestComputeUnifiedDiffCountsInsertedAndDeletedLines(t *testing.T) {
	oldContent := "<p>One</p>\n<p>Two</p>"
	newContent := "<p>One</p>\n<p>Two</p>\n<p>Three</p>"

	diffText, additions, deletions := computeUnifiedDiff(oldContent, newContent, "12345")

	assert.Contains(t, diffText, "+<p>Three</p>")
	assert.Equal(t, 1, additions)
	assert.Equal(t, 0, deletions)
}
