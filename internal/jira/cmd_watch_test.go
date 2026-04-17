package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureJiraWatchJSON(t *testing.T, run func() error) map[string]any {
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

func TestWatchIssueJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/PROJ-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":  "1",
				"key": "PROJ-1",
				"fields": map[string]any{
					"summary": "Watch me",
					"status":  map[string]any{"name": "To Do"},
					"updated": "2026-04-17T00:00:00Z",
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")
	t.Setenv("JIRA_AUTH_MODE", "bearer")

	cmd := NewWatchCmd()
	cmd.SetArgs([]string{"issue", "PROJ-1", "--cycles", "1", "--output-mode", "json"})
	payload := captureJiraWatchJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, "polling", result["transport"])
}
