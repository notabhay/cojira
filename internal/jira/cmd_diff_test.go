package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestChronologicalHistoryEntriesReversesNewestFirst(t *testing.T) {
	entries := []map[string]any{
		{"id": "3", "created": "2026-04-16T10:00:00Z"},
		{"id": "2", "created": "2026-04-15T10:00:00Z"},
		{"id": "1", "created": "2026-04-14T10:00:00Z"},
	}

	result := chronologicalHistoryEntries(entries)
	assert.Equal(t, "1", result[0]["id"])
	assert.Equal(t, "3", result[2]["id"])
}

func TestSelectHistoryEntriesByRange(t *testing.T) {
	entries := []map[string]any{
		{"id": "1"},
		{"id": "2"},
		{"id": "3"},
	}

	selected, fromID, toID, err := selectHistoryEntries(entries, "", "1", "2")
	require.NoError(t, err)
	require.Len(t, selected, 2)
	assert.Equal(t, "1", fromID)
	assert.Equal(t, "2", toID)
}

func TestAggregateHistoryChangesMergesFieldRange(t *testing.T) {
	entries := []map[string]any{
		{
			"id": "1",
			"changes": []map[string]any{
				{"field": "status", "from": "To Do", "to": "In Progress"},
				{"field": "summary", "from": "Old", "to": "Mid"},
			},
		},
		{
			"id": "2",
			"changes": []map[string]any{
				{"field": "status", "from": "In Progress", "to": "Done"},
			},
		},
	}

	result := aggregateHistoryChanges(entries)
	require.Len(t, result, 2)
	assert.Equal(t, "To Do", result[0]["from"])
	assert.Equal(t, "Done", result[0]["to"])
	assert.Equal(t, 2, result[0]["change_count"])
}
