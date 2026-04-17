package jira

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeFieldMetadataClient struct {
	editMeta   map[string]any
	issue      map[string]any
	createMeta []map[string]any
	fields     []map[string]any
}

func (f *fakeFieldMetadataClient) GetEditMeta(issue string) (map[string]any, error) {
	return f.editMeta, nil
}

func (f *fakeFieldMetadataClient) GetIssue(issue string, fields string, expand string) (map[string]any, error) {
	return f.issue, nil
}

func (f *fakeFieldMetadataClient) GetCreateMetaIssueTypeFields(projectKey, issueTypeID string) ([]map[string]any, error) {
	return f.createMeta, nil
}

func (f *fakeFieldMetadataClient) ListCreateMetaIssueTypes(projectKey string) ([]map[string]any, error) {
	return []map[string]any{{"id": "3", "name": "Bug"}}, nil
}

func (f *fakeFieldMetadataClient) ListFields() ([]map[string]any, error) {
	return f.fields, nil
}

func TestFieldResolverResolvesBuiltinAlias(t *testing.T) {
	resolver := newIssueFieldResolver(&fakeFieldMetadataClient{}, "PROJ-1")
	fieldID, err := resolver.Resolve("fix versions")
	require.NoError(t, err)
	assert.Equal(t, "fixVersions", fieldID)
}

func TestFieldResolverResolvesEditMetaField(t *testing.T) {
	resolver := newIssueFieldResolver(&fakeFieldMetadataClient{
		editMeta: map[string]any{
			"fields": map[string]any{
				"customfield_10001": map[string]any{"name": "Story Points"},
			},
		},
	}, "PROJ-1")
	fieldID, err := resolver.Resolve("Story Points")
	require.NoError(t, err)
	assert.Equal(t, "customfield_10001", fieldID)
}

func TestFieldResolverResolvesCreateMetaField(t *testing.T) {
	resolver := newCreateFieldResolver(&fakeFieldMetadataClient{
		createMeta: []map[string]any{
			{"fieldId": "customfield_10002", "name": "Sprint"},
		},
	}, map[string]any{
		"project":   map[string]any{"key": "RAPTOR"},
		"issuetype": map[string]any{"id": "3"},
	})
	fieldID, err := resolver.Resolve("Sprint")
	require.NoError(t, err)
	assert.Equal(t, "customfield_10002", fieldID)
}

func TestResolveFieldMapKeysUsesResolvedID(t *testing.T) {
	resolver := newIssueFieldResolver(&fakeFieldMetadataClient{
		editMeta: map[string]any{
			"fields": map[string]any{
				"customfield_10001": map[string]any{"name": "Story Points"},
			},
		},
	}, "PROJ-1")
	fields, err := resolveFieldMapKeys(map[string]any{"Story Points": 5, "summary": "ok"}, resolver)
	require.NoError(t, err)
	assert.Equal(t, 5, fields["customfield_10001"])
	assert.Equal(t, "ok", fields["summary"])
}
