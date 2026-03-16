package jira

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestJiraRawJSONFetchesAllowlistedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/PROJ-1", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key": "PROJ-1",
		})
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewRawCmd(), []string{"GET", "/issue/PROJ-1", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, "PROJ-1", result["key"])
}

func TestJiraDeleteDryRunJSON(t *testing.T) {
	t.Setenv("JIRA_BASE_URL", "https://jira.example.com")
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewDeleteCmd(), []string{"PROJ-1", "--dry-run", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
}

func TestJiraInfoJSONIncludesDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Query().Get("fields"), "description")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":  "10001",
			"key": "PROJ-1",
			"fields": map[string]any{
				"summary":     "Test",
				"description": "Long description",
				"status":      map[string]any{"name": "Open"},
				"project":     map[string]any{"key": "PROJ"},
			},
		})
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewInfoCmd(), []string{"PROJ-1", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, "Long description", result["description"])
}

func TestJiraUpdatePreviewResolvesComponentsAndDue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/PROJ-1":
			assert.Contains(t, r.URL.Query().Get("fields"), "project")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":  "10001",
				"key": "PROJ-1",
				"fields": map[string]any{
					"project":     map[string]any{"key": "PROJ"},
					"description": "before\nline two",
					"components":  []map[string]any{{"id": "10", "name": "Old"}},
				},
			})
		case "/rest/api/2/project/PROJ":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"key": "PROJ",
				"components": []map[string]any{
					{"id": "327071", "name": "[Analytics]"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewUpdateCmd(), []string{
		"PROJ-1",
		"--description", "after\nline two",
		"--due", "2026-03-11",
		"--component", "[Analytics]",
		"--diff",
		"--output-mode", "json",
	}, &payload))

	result := payload["result"].(map[string]any)
	unified := result["unified_diffs"].(map[string]any)
	assert.Contains(t, unified["description"], "--- description.current")

	diffs := result["diffs"].([]any)
	foundDue := false
	foundComponents := false
	for _, item := range diffs {
		diff := item.(map[string]any)
		switch diff["field"] {
		case "duedate":
			foundDue = true
		case "components":
			foundComponents = true
		}
	}
	assert.True(t, foundDue)
	assert.True(t, foundComponents)
}

func TestJiraCreateJSONIncludesPathContextOnMissingFile(t *testing.T) {
	t.Setenv("JIRA_BASE_URL", "https://jira.example.com")
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewCreateCmd(), []string{"missing.json", "--output-mode", "json"}, &payload))
	assert.Equal(t, false, payload["ok"])
	target := payload["target"].(map[string]any)
	assert.Equal(t, "missing.json", target["file"])
	assert.NotEmpty(t, target["absolute_file"])
	assert.NotEmpty(t, target["cwd"])
}

func executeJiraJSONCommand(t *testing.T, cmd *cobra.Command, args []string, out *map[string]any) error {
	t.Helper()
	output.SetMode("")

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	cmd.SetArgs(args)
	err = cmd.Execute()

	_ = w.Close()
	buf, _ := io.ReadAll(r)
	require.NotEmpty(t, buf)
	require.NoError(t, json.Unmarshal(buf, out))
	return err
}

func TestJiraRawRejectsAbsoluteURL(t *testing.T) {
	t.Setenv("JIRA_BASE_URL", "https://jira.example.com")
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewRawCmd(), []string{"GET", "https://example.com/rest/api/2/issue/PROJ-1", "--output-mode", "json"}, &payload))
	assert.Equal(t, false, payload["ok"])
}

func TestJiraRawRejectsUnsupportedNestedIssueSubresource(t *testing.T) {
	t.Setenv("JIRA_BASE_URL", "https://jira.example.com")
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewRawCmd(), []string{"POST", "/issue/PROJ-1/comment", "--output-mode", "json"}, &payload))
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, float64(2), payload["exit_code"])
}

func TestJiraDeleteJSONDeletesIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-1", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewDeleteCmd(), []string{"PROJ-1", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["deleted"])
}

func TestJiraRawSavesResponseToFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "PROJ-2"})
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	outputPath := filepath.Join(t.TempDir(), "raw.json")
	var payload map[string]any
	require.NoError(t, executeJiraJSONCommand(t, NewRawCmd(), []string{"GET", "/issue/PROJ-2", "-o", outputPath, "--output-mode", "json"}, &payload))
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Contains(t, string(data), "PROJ-2")
}
