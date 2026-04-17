package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRankDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewRankCmd()
	cmd.SetArgs([]string{"PROJ-1", "--after", "PROJ-9", "--rank-field", "customfield_12345", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, float64(12345), result["rank_custom_field_id"])
}

func TestBacklogMoveToDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := newBacklogMoveToCmd()
	cmd.SetArgs([]string{"45434", "PROJ-1", "--after", "PROJ-9", "--rank-field", "12345", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, float64(12345), result["rank_custom_field_id"])
}

func TestEpicAddDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := newEpicAddCmd()
	cmd.SetArgs([]string{"RAPTOR-1", "PROJ-1", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, "RAPTOR-1", result["epic"])
}

func TestEpicChildrenJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/agile/1.0/epic/RAPTOR-1/issue":
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

	cmd := newEpicChildrenCmd()
	cmd.SetArgs([]string{"RAPTOR-1", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["issues"].([]any)
	require.Len(t, items, 1)
}

func TestBacklogListJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/agile/1.0/board/45434/backlog":
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

	cmd := newBacklogListCmd()
	cmd.SetArgs([]string{"45434", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["issues"].([]any)
	require.Len(t, items, 1)
}
