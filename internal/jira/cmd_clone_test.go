package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildClonePayloadCopiesSourceFields(t *testing.T) {
	cmd := NewCloneCmd()
	source := map[string]any{
		"key": "RAPTOR-1",
		"fields": map[string]any{
			"summary":     "Original issue",
			"description": "Original description",
			"issuetype":   map[string]any{"name": "Bug"},
			"priority":    map[string]any{"name": "High"},
			"labels":      []any{"foo", "bar"},
			"components":  []any{map[string]any{"name": "API"}},
			"versions":    []any{map[string]any{"name": "1.0"}},
			"fixVersions": []any{map[string]any{"name": "1.1"}},
			"project":     map[string]any{"key": "RAPTOR"},
		},
	}

	payload, summary, project, err := buildClonePayload(cmd, source)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, "Original issue", summary)
	assert.Equal(t, "RAPTOR", project)
	assert.Equal(t, "Original issue", fields["summary"])
	assert.Equal(t, "Original description", fields["description"])
	assert.Equal(t, map[string]any{"name": "Bug"}, fields["issuetype"])
	assert.Equal(t, map[string]any{"name": "High"}, fields["priority"])
	assert.Equal(t, []string{"foo", "bar"}, fields["labels"])
}

func TestBuildClonePayloadAppliesOverrides(t *testing.T) {
	cmd := NewCloneCmd()
	require.NoError(t, cmd.Flags().Set("project", "OTHER"))
	require.NoError(t, cmd.Flags().Set("summary-prefix", "Copy of: "))
	require.NoError(t, cmd.Flags().Set("type", "Task"))
	require.NoError(t, cmd.Flags().Set("set", "labels+=baz"))

	source := map[string]any{
		"key": "RAPTOR-1",
		"fields": map[string]any{
			"summary":   "Original issue",
			"issuetype": map[string]any{"name": "Bug"},
			"labels":    []any{"foo"},
			"project":   map[string]any{"key": "RAPTOR"},
		},
	}

	payload, summary, project, err := buildClonePayload(cmd, source)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, "Copy of: Original issue", summary)
	assert.Equal(t, "OTHER", project)
	assert.Equal(t, map[string]any{"key": "OTHER"}, fields["project"])
	assert.Equal(t, map[string]any{"name": "Task"}, fields["issuetype"])
	assert.Equal(t, []string{"foo", "baz"}, fields["labels"])
}

func TestBuildClonePayloadDropsParentAcrossProjects(t *testing.T) {
	cmd := NewCloneCmd()
	require.NoError(t, cmd.Flags().Set("project", "OTHER"))
	require.NoError(t, cmd.Flags().Set("keep-parent", "true"))

	source := map[string]any{
		"fields": map[string]any{
			"summary": "Original issue",
			"project": map[string]any{"key": "RAPTOR"},
			"parent":  map[string]any{"key": "RAPTOR-99"},
		},
	}

	payload, _, _, err := buildClonePayload(cmd, source)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.NotContains(t, fields, "parent")
}
