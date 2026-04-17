package jira

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildCreatePayloadFromInlineFlags(t *testing.T) {
	cmd := NewCreateCmd()
	require.NoError(t, cmd.Flags().Set("project", "RAPTOR"))
	require.NoError(t, cmd.Flags().Set("summary", "Fix login bug"))
	require.NoError(t, cmd.Flags().Set("type", "Bug"))
	require.NoError(t, cmd.Flags().Set("priority", "High"))
	require.NoError(t, cmd.Flags().Set("labels", "backend,urgent"))
	require.NoError(t, cmd.Flags().Set("components", "API"))
	require.NoError(t, cmd.Flags().Set("assignee", "me"))

	payload, err := buildCreatePayload(cmd, "")
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, "Fix login bug", fields["summary"])
	assert.Equal(t, map[string]any{"key": "RAPTOR"}, fields["project"])
	assert.Equal(t, map[string]any{"name": "Bug"}, fields["issuetype"])
	assert.Equal(t, map[string]any{"name": "High"}, fields["priority"])
	assert.Equal(t, []string{"backend", "urgent"}, fields["labels"])
	assert.Equal(t, []map[string]any{{"name": "API"}}, fields["components"])
	assert.Equal(t, map[string]any{"name": "me"}, fields["assignee"])
}

func TestBuildCreatePayloadDefaultsProjectAndType(t *testing.T) {
	t.Setenv("JIRA_PROJECT", "RAPTOR")

	cmd := NewCreateCmd()
	require.NoError(t, cmd.Flags().Set("summary", "Create from defaults"))

	payload, err := buildCreatePayload(cmd, "")
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, map[string]any{"key": "RAPTOR"}, fields["project"])
	assert.Equal(t, map[string]any{"name": "Task"}, fields["issuetype"])
}

func TestBuildCreatePayloadMergesFileAndFlags(t *testing.T) {
	dir := t.TempDir()
	payloadPath := filepath.Join(dir, "payload.json")
	require.NoError(t, os.WriteFile(payloadPath, []byte(`{"fields":{"project":{"key":"OLD"},"summary":"Old summary","labels":["legacy"]}}`), 0o644))

	cmd := NewCreateCmd()
	require.NoError(t, cmd.Flags().Set("project", "NEW"))
	require.NoError(t, cmd.Flags().Set("summary", "New summary"))
	require.NoError(t, cmd.Flags().Set("set", "labels+=fresh"))

	payload, err := buildCreatePayload(cmd, payloadPath)
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	assert.Equal(t, map[string]any{"key": "NEW"}, fields["project"])
	assert.Equal(t, "New summary", fields["summary"])
	assert.Equal(t, []string{"legacy", "fresh"}, fields["labels"])
}

func TestBuildCreatePayloadConvertsDescriptionToADFOnV3(t *testing.T) {
	t.Setenv("JIRA_API_VERSION", "3")

	cmd := NewCreateCmd()
	require.NoError(t, cmd.Flags().Set("project", "RAPTOR"))
	require.NoError(t, cmd.Flags().Set("summary", "ADF issue"))
	require.NoError(t, cmd.Flags().Set("description", "Hello world"))

	payload, err := buildCreatePayload(cmd, "")
	require.NoError(t, err)

	fields := payload["fields"].(map[string]any)
	description := fields["description"].(map[string]any)
	assert.Equal(t, "doc", description["type"])
}

func TestBuildCreatePayloadRequiresSummaryForFlagMode(t *testing.T) {
	cmd := NewCreateCmd()
	require.NoError(t, cmd.Flags().Set("project", "RAPTOR"))

	_, err := buildCreatePayload(cmd, "")
	require.Error(t, err)
}

func TestApplyCreateFlagsRejectsDescriptionConflicts(t *testing.T) {
	cmd := &cobra.Command{}
	cmd.Flags().String("summary", "", "")
	cmd.Flags().String("project", "", "")
	cmd.Flags().String("type", "", "")
	cmd.Flags().String("priority", "", "")
	cmd.Flags().String("description", "", "")
	cmd.Flags().String("description-file", "", "")
	cmd.Flags().String("assignee", "", "")
	cmd.Flags().String("reporter", "", "")
	cmd.Flags().String("parent", "", "")
	cmd.Flags().StringSlice("labels", nil, "")
	cmd.Flags().StringSlice("components", nil, "")
	cmd.Flags().StringSlice("versions", nil, "")
	cmd.Flags().StringSlice("fix-versions", nil, "")
	cmd.Flags().StringArray("set", nil, "")
	require.NoError(t, cmd.Flags().Set("description", "inline"))
	require.NoError(t, cmd.Flags().Set("description-file", "desc.txt"))

	_, err := applyCreateFlags(cmd, map[string]any{})
	require.Error(t, err)
}

func TestPreviewCreateFieldsIncludesTemplateAndFlags(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, ".cojira.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{"jira":{"templates":{"bug":{"project":"RAPTOR","type":"Bug","labels":["templated"]}}}}`), 0o644))

	prevWD, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(dir))
	defer func() {
		_ = os.Chdir(prevWD)
	}()

	cmd := NewCreateCmd()
	require.NoError(t, cmd.Flags().Set("template", "bug"))
	require.NoError(t, cmd.Flags().Set("summary", "Prompt me"))

	fields, err := previewCreateFields(cmd)
	require.NoError(t, err)
	assert.Equal(t, "Prompt me", fields["summary"])
	assert.Equal(t, map[string]any{"key": "RAPTOR"}, fields["project"])
	assert.Equal(t, map[string]any{"name": "Bug"}, fields["issuetype"])
	assert.Equal(t, []string{"templated"}, fields["labels"])
}

func TestPromptCreateIssueTypeSupportsNumericChoice(t *testing.T) {
	value, err := promptCreateIssueType(bufio.NewReader(strings.NewReader("2\n")), nil, "")
	require.NoError(t, err)
	assert.Equal(t, "Bug", value)
}

func TestPromptCreateTextUsesDefaultAndRequiresValue(t *testing.T) {
	value, err := promptCreateText(bufio.NewReader(strings.NewReader("\n")), "Summary", "Keep default", true)
	require.NoError(t, err)
	assert.Equal(t, "Keep default", value)
}

func TestIssueTypePromptOptionsUsesCreateMetaNames(t *testing.T) {
	options := issueTypePromptOptions([]map[string]any{
		{"name": "Task"},
		{"name": "Operation"},
	})

	assert.Equal(t, []string{"Task", "Operation"}, options)
}
