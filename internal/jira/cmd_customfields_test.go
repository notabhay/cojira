package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilterCustomFields(t *testing.T) {
	items := filterCustomFields([]map[string]any{
		{"id": "summary", "name": "Summary", "schema": map[string]any{"type": "string"}},
		{"id": "customfield_10001", "name": "Story Points", "schema": map[string]any{"type": "number"}},
		{"id": "customfield_10002", "name": "Epic Link", "schema": map[string]any{"type": "string"}},
	}, "story")

	assert.Len(t, items, 1)
	assert.Equal(t, "customfield_10001", items[0]["id"])
	assert.Equal(t, "number", items[0]["type"])
}
