package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureCommandOutput(t *testing.T, run func() error) map[string]any {
	t.Helper()
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	err = run()
	require.NoError(t, err)
	require.NoError(t, w.Close())

	var payload map[string]any
	require.NoError(t, json.NewDecoder(r).Decode(&payload))
	return payload
}

func TestBulkAssignDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}, {"key": "PROJ-2"}},
				"total":  2,
			})
		case "/rest/api/2/user/search":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "jdoe", "displayName": "Jane Doe"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkAssignCmd()
	cmd.SetArgs([]string{"jdoe", "--jql", "project = PROJ", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	summary := result["summary"].(map[string]any)
	assert.Equal(t, float64(2), summary["ok"])
	items := result["items"].([]any)
	require.Len(t, items, 2)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
}

func TestBulkCommentDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}},
				"total":  1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkCommentCmd()
	cmd.SetArgs([]string{"--jql", "project = PROJ", "--add", "hello world", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
	assert.Equal(t, "hello world", first["body"])
}

func TestBulkWatchDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}},
				"total":  1,
			})
		case "/rest/api/2/user/search":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"accountId": "abc", "displayName": "Jane Doe"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkWatchCmd()
	cmd.SetArgs([]string{"jdoe", "--jql", "project = PROJ", "--dry-run", "--remove", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, "remove", first["action"])
	assert.Equal(t, true, first["dry_run"])
}

func TestBulkDeleteDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}},
				"total":  1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkDeleteCmd()
	cmd.SetArgs([]string{"--jql", "project = PROJ", "--delete-subtasks", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
	assert.Equal(t, true, first["delete_subtasks"])
}

func TestBulkLinkDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}},
				"total":  1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkLinkCmd()
	cmd.SetArgs([]string{"PROJ-99", "--jql", "project = PROJ", "--type", "Blocks", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
	payloadMap := first["payload"].(map[string]any)
	assert.Equal(t, "Blocks", payloadMap["type"].(map[string]any)["name"])
}

func TestBulkLabelDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{
					{"key": "PROJ-1", "fields": map[string]any{"labels": []string{"triage"}}},
				},
				"total": 1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkLabelCmd()
	cmd.SetArgs([]string{"--jql", "project = PROJ", "--add", "agent-reviewed", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
	assert.Equal(t, []any{"triage"}, first["labels_before"])
	assert.Equal(t, []any{"triage", "agent-reviewed"}, first["labels_after"])
	assert.Equal(t, true, first["changed"])
}

func TestBulkAttachmentDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}},
				"total":  1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkAttachmentCmd()
	cmd.SetArgs([]string{"--jql", "project = PROJ", "--upload", "README.md", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
	assert.Equal(t, []any{"README.md"}, first["files"])
}

func TestBulkWorklogDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}},
				"total":  1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkWorklogCmd()
	cmd.SetArgs([]string{"--jql", "project = PROJ", "--time-spent", "1h", "--comment", "agent note", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
	worklogPayload := first["payload"].(map[string]any)
	assert.Equal(t, "1h", worklogPayload["timeSpent"])
	assert.Equal(t, "agent note", worklogPayload["comment"])
}

func TestBulkAttachmentStdinDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/search":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issues": []map[string]any{{"key": "PROJ-1"}},
				"total":  1,
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewBulkAttachmentCmd()
	cmd.SetIn(strings.NewReader("stdin payload"))
	cmd.SetArgs([]string{"--jql", "project = PROJ", "--stdin", "--filename", "stdin.txt", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["items"].([]any)
	require.Len(t, items, 1)
	first := items[0].(map[string]any)
	assert.Equal(t, true, first["dry_run"])
	assert.Equal(t, true, first["stdin"])
	assert.Equal(t, "stdin.txt", first["filename"])
}
