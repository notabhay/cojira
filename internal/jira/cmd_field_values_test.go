package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFindCreateMetaFieldByNameAndID(t *testing.T) {
	fields := []map[string]any{
		{"fieldId": "priority", "name": "Priority"},
		{"fieldId": "customfield_10001", "name": "Story Points"},
	}

	fieldID, entry := findCreateMetaField(fields, "Priority")
	require.NotNil(t, entry)
	assert.Equal(t, "priority", fieldID)

	fieldID, entry = findCreateMetaField(fields, "customfield_10001")
	require.NotNil(t, entry)
	assert.Equal(t, "customfield_10001", fieldID)
}

func TestIssueProjectAndType(t *testing.T) {
	project, issueType := issueProjectAndType(map[string]any{
		"fields": map[string]any{
			"project":   map[string]any{"key": "RAPTOR"},
			"issuetype": map[string]any{"id": "3"},
		},
	})

	assert.Equal(t, "RAPTOR", project)
	assert.Equal(t, "3", issueType)
}

func TestCoerceAllowedValueEnums(t *testing.T) {
	values := coerceAllowedValueEnums([]any{
		map[string]any{"name": "High"},
		map[string]any{"value": "Medium", "name": "Medium"},
		map[string]any{"id": "1", "displayName": "Low"},
	})

	assert.Equal(t, []string{"High", "Medium", "Low"}, values)
}
