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

func captureJSMJSON(t *testing.T, run func() error) map[string]any {
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

func TestJSMRequestGetJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/servicedeskapi/request/RAPTOR-1", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"issueKey": "RAPTOR-1"})
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")
	t.Setenv("JIRA_AUTH_MODE", "bearer")

	cmd := NewJSMCmd()
	cmd.SetArgs([]string{"request", "get", "RAPTOR-1", "--output-mode", "json"})
	payload := captureJSMJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, "RAPTOR-1", payload["result"].(map[string]any)["issueKey"])
}
