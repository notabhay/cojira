package confluence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureConfluenceMacroJSON(t *testing.T, run func() error) map[string]any {
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

func TestMacrosRenderJSON(t *testing.T) {
	cmd := NewMacrosCmd()
	cmd.SetArgs([]string{"render", "info", "--body", "<p>Hello</p>", "--output-mode", "json"})
	payload := captureConfluenceMacroJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Contains(t, result["content"], `ac:name="info"`)
}

func TestMacrosInsertJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345":
			switch r.Method {
			case "GET":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":    "12345",
					"title": "Macro Page",
					"version": map[string]any{
						"number": 1,
					},
					"body": map[string]any{
						"storage": map[string]any{"value": "<p>Start</p>"},
					},
				})
			case "PUT":
				_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345", "title": "Macro Page"})
			default:
				http.NotFound(w, r)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewMacrosCmd()
	cmd.SetArgs([]string{"insert", "12345", "info", "--body", "<p>Hello</p>", "--output-mode", "json"})
	payload := captureConfluenceMacroJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, "info", result["macro"])
}
