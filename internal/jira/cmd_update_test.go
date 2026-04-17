package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestApplySetOpCoercesIssueType(t *testing.T) {
	fields := map[string]any{}
	err := applySetOp("issuetype", OpSet, "Bug", fields, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"name": "Bug"}, fields["issuetype"])
}

func TestApplySetOpCoercesReporterByEmail(t *testing.T) {
	fields := map[string]any{}
	err := applySetOp("reporter", OpSet, "user@example.com", fields, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, map[string]any{"emailAddress": "user@example.com"}, fields["reporter"])
}

func TestApplySetOpCoercesProjectAndParent(t *testing.T) {
	fields := map[string]any{}
	require.NoError(t, applySetOp("project", OpSet, "RAPTOR", fields, map[string]any{}))
	require.NoError(t, applySetOp("parent", OpSet, "RAPTOR-123", fields, map[string]any{}))
	assert.Equal(t, map[string]any{"key": "RAPTOR"}, fields["project"])
	assert.Equal(t, map[string]any{"key": "RAPTOR-123"}, fields["parent"])
}

func TestApplySetOpCoercesCommaSeparatedLists(t *testing.T) {
	fields := map[string]any{}
	require.NoError(t, applySetOp("labels", OpSet, "foo, bar", fields, map[string]any{}))
	require.NoError(t, applySetOp("components", OpSet, "Frontend, Backend", fields, map[string]any{}))
	assert.Equal(t, []string{"foo", "bar"}, fields["labels"])
	assert.Equal(t, []map[string]any{{"name": "Frontend"}, {"name": "Backend"}}, fields["components"])
}

func TestMergedFieldStateAllowsSequentialListSetOps(t *testing.T) {
	fields := map[string]any{}
	require.NoError(t, applySetOp("labels", OpListAppend, "alpha", fields, mergedFieldState(map[string]any{}, fields)))
	require.NoError(t, applySetOp("labels", OpListAppend, "beta", fields, mergedFieldState(map[string]any{}, fields)))
	assert.Equal(t, []string{"alpha", "beta"}, fields["labels"])
}

func TestApplyResolvedSetOpCoercesNumericCustomFields(t *testing.T) {
	fields := map[string]any{}
	err := applyResolvedSetOp("customfield_10001", OpSet, "5", map[string]any{
		"schema": map[string]any{"type": "number"},
	}, fields, map[string]any{})
	require.NoError(t, err)
	assert.Equal(t, 5.0, fields["customfield_10001"])
}
