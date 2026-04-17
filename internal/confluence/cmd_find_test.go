package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNormalizeCQLPageSupportsWrappedAndFlatShapes(t *testing.T) {
	wrapped := normalizeCQLPage(map[string]any{
		"content": map[string]any{
			"id":    "123",
			"title": "Wrapped",
			"space": map[string]any{"key": "DOC"},
		},
		"url": "/x/123",
	})
	assert.Equal(t, "123", wrapped["id"])
	assert.Equal(t, "Wrapped", wrapped["title"])
	assert.Equal(t, "DOC", wrapped["space"])
	assert.Equal(t, "/x/123", wrapped["url"])

	flat := normalizeCQLPage(map[string]any{
		"id":    "456",
		"title": "Flat",
		"space": map[string]any{"key": "ENG"},
	})
	assert.Equal(t, "456", flat["id"])
	assert.Equal(t, "Flat", flat["title"])
	assert.Equal(t, "ENG", flat["space"])
}
