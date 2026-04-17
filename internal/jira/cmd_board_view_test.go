package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildBoardColumnsMapsStatusesToConfiguredColumns(t *testing.T) {
	config := map[string]any{
		"columnConfig": map[string]any{
			"columns": []any{
				map[string]any{
					"name": "To Do",
					"statuses": []any{
						map[string]any{"id": "1"},
					},
				},
				map[string]any{
					"name": "Done",
					"statuses": []any{
						map[string]any{"id": "6"},
					},
				},
			},
		},
	}
	issues := []map[string]any{
		{"key": "PROJ-1", "status_id": "1", "summary": "Todo item"},
		{"key": "PROJ-2", "status_id": "6", "summary": "Done item"},
		{"key": "PROJ-3", "status_id": "999", "summary": "Other item"},
	}

	columns := buildBoardColumns(config, issues)
	require.Len(t, columns, 3)
	assert.Equal(t, "To Do", columns[0].Name)
	assert.Len(t, columns[0].Items, 1)
	assert.Equal(t, "Done", columns[1].Name)
	assert.Len(t, columns[1].Items, 1)
	assert.Equal(t, "Unmapped", columns[2].Name)
	assert.Len(t, columns[2].Items, 1)
}

func TestLimitIssues(t *testing.T) {
	items := []map[string]any{
		{"key": "A"},
		{"key": "B"},
		{"key": "C"},
	}

	visible, overflow := limitIssues(items, 2)
	require.Len(t, visible, 2)
	assert.Equal(t, 1, overflow)
}
