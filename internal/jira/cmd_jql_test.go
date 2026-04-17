package jira

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildJQLFromFlags(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().StringSlice("project", nil, "")
	cmd.Flags().StringSlice("status", nil, "")
	cmd.Flags().StringSlice("type", nil, "")
	cmd.Flags().StringSlice("label", nil, "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("reporter", "", "")
	cmd.Flags().String("text", "", "")
	cmd.Flags().StringArray("clause", nil, "")
	cmd.Flags().Bool("unresolved", false, "")
	cmd.Flags().String("order-by", "updated DESC", "")
	require.NoError(t, cmd.Flags().Set("project", "RAPTOR"))
	require.NoError(t, cmd.Flags().Set("status", "In Progress"))
	require.NoError(t, cmd.Flags().Set("unresolved", "true"))

	query, clauses, err := buildJQLFromFlags(cmd)
	require.NoError(t, err)
	assert.Equal(t, []string{"project = RAPTOR", `status = "In Progress"`, "resolution = Unresolved"}, clauses)
	assert.Equal(t, `project = RAPTOR AND status = "In Progress" AND resolution = Unresolved ORDER BY updated DESC`, query)
}

func TestFilterJQLSuggestions(t *testing.T) {
	result := filterJQLSuggestions(map[string]any{
		"visibleFieldNames":    []any{map[string]any{"displayName": "status"}, "summary"},
		"visibleFunctionNames": []any{map[string]any{"value": "currentUser()"}, "membersOf()"},
		"jqlReservedWords":     []any{"AND", "ORDER"},
	}, "cur", 10)

	assert.Equal(t, []string{"currentUser()"}, result["functions"])
	assert.Empty(t, result["fields"])
}
