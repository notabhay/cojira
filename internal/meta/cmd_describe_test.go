package meta

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func testRootCmd() *cobra.Command {
	root := &cobra.Command{Use: "cojira"}

	// Add stub jira subcommand with sample sub-subcommands.
	jiraCmd := &cobra.Command{Use: "jira", Short: "Jira operations"}
	for _, name := range []string{"info", "get", "update", "create", "template", "transition",
		"transitions", "search", "query", "mine", "recent", "current", "branch",
		"commit-template", "pr-title", "finish-branch", "board-issues", "fields",
		"field-values", "graph", "blocked", "critical-path", "whoami", "validate",
		"poll", "offline", "batch", "bulk-update", "bulk-transition", "bulk-update-summaries",
		"sync", "sync-from-dir"} {
		sub := &cobra.Command{Use: name, Short: name + " command"}
		jiraCmd.AddCommand(sub)
	}
	root.AddCommand(jiraCmd)

	// Add stub confluence subcommand.
	confCmd := &cobra.Command{Use: "confluence", Short: "Confluence operations"}
	for _, name := range []string{"info", "get", "update", "create", "rename",
		"move", "tree", "find", "copy-tree", "archive", "validate", "batch"} {
		sub := &cobra.Command{Use: name, Short: name + " command"}
		confCmd.AddCommand(sub)
	}
	root.AddCommand(confCmd)

	// Add meta commands.
	root.AddCommand(NewBootstrapCmd())
	root.AddCommand(NewCompletionCmd(root))
	root.AddCommand(NewDescribeCmd(root))
	root.AddCommand(NewDoctorCmd())
	root.AddCommand(NewInitCmd())
	root.AddCommand(NewPlanCmd(root))
	root.AddCommand(NewDoCmd(root))

	return root
}

func TestBuildManifest(t *testing.T) {
	root := testRootCmd()
	manifest := buildManifest(root)

	assert.Equal(t, "cojira", manifest["name"])

	parsers, ok := manifest["parsers"].(map[string]any)
	require.True(t, ok)

	// Jira subcommands should be present.
	jiraParser, ok := parsers["jira"].(map[string]any)
	require.True(t, ok)
	jiraSubs, ok := jiraParser["subcommands"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, jiraSubs, "bulk-transition")
	assert.Contains(t, jiraSubs, "update")
	assert.Contains(t, jiraSubs, "query")
	assert.Contains(t, jiraSubs, "offline")

	// Confluence subcommands should be present.
	confParser, ok := parsers["confluence"].(map[string]any)
	require.True(t, ok)
	confSubs, ok := confParser["subcommands"].(map[string]any)
	require.True(t, ok)
	assert.Contains(t, confSubs, "copy-tree")
	assert.Contains(t, confSubs, "archive")
}

func TestManifestIncludesAllEnvVars(t *testing.T) {
	root := testRootCmd()
	manifest := buildManifest(root)

	env, ok := manifest["env"].(map[string]any)
	require.True(t, ok)

	confEnv, ok := env["confluence"].(map[string]any)
	require.True(t, ok)
	confRequired, ok := confEnv["required"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN"}, confRequired)

	jiraEnv, ok := env["jira"].(map[string]any)
	require.True(t, ok)
	jiraRequired, ok := jiraEnv["required"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"JIRA_BASE_URL", "JIRA_API_TOKEN"}, jiraRequired)

	jiraOptional, ok := jiraEnv["optional"].([]string)
	require.True(t, ok)
	for _, v := range []string{"JIRA_EMAIL", "JIRA_PROJECT", "JIRA_API_VERSION",
		"JIRA_AUTH_MODE", "JIRA_VERIFY_SSL", "JIRA_USER_AGENT"} {
		assert.Contains(t, jiraOptional, v)
	}
}

func TestAgentPromptIncludesSafetyRules(t *testing.T) {
	root := testRootCmd()
	manifest := buildManifest(root)
	prompt := agentPrompt(manifest)
	lower := prompt
	assert.Contains(t, lower, "dry-run")
	assert.Contains(t, lower, "macros")
	assert.Contains(t, lower, "storage format")
}

func TestAgentPromptIncludesAllCommands(t *testing.T) {
	root := testRootCmd()
	manifest := buildManifest(root)
	parsers, _ := manifest["parsers"].(map[string]any)
	prompt := agentPrompt(manifest)

	jiraParser, _ := parsers["jira"].(map[string]any)
	jiraSubs, _ := jiraParser["subcommands"].(map[string]any)
	for cmd := range jiraSubs {
		assert.Contains(t, prompt, cmd)
	}

	confParser, _ := parsers["confluence"].(map[string]any)
	confSubs, _ := confParser["subcommands"].(map[string]any)
	for cmd := range confSubs {
		assert.Contains(t, prompt, cmd)
	}
}

func TestDescribeJSONIsValidJSON(t *testing.T) {
	root := testRootCmd()

	// Capture stdout.
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// Must set args on root - cobra's Execute() traverses to root.
	root.SetArgs([]string{"describe", "--output-mode", "json"})
	err := root.Execute()

	_ = w.Close()
	os.Stdout = origStdout

	require.NoError(t, err)

	buf := make([]byte, 65536)
	n, _ := r.Read(buf)
	require.Greater(t, n, 0)

	var data map[string]any
	require.NoError(t, json.Unmarshal(buf[:n], &data))
	assert.Equal(t, true, data["ok"])
}

func TestConfiguredToolsFromEnv(t *testing.T) {
	// Clear all.
	t.Setenv("JIRA_BASE_URL", "")
	t.Setenv("JIRA_API_TOKEN", "")
	t.Setenv("CONFLUENCE_BASE_URL", "")
	t.Setenv("CONFLUENCE_API_TOKEN", "")
	assert.Empty(t, configuredToolsFromEnv())

	// Set Jira only.
	t.Setenv("JIRA_BASE_URL", "https://jira.example.com")
	t.Setenv("JIRA_API_TOKEN", "token")
	tools := configuredToolsFromEnv()
	assert.Equal(t, []string{"jira"}, tools)

	// Set both.
	t.Setenv("CONFLUENCE_BASE_URL", "https://confluence.example.com")
	t.Setenv("CONFLUENCE_API_TOKEN", "token")
	tools = configuredToolsFromEnv()
	assert.Equal(t, []string{"confluence", "jira"}, tools)
}
