package confluence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureTemplateJSON(t *testing.T, run func() error) map[string]any {
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

func TestTemplatesCreateJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/template":
			_ = json.NewEncoder(w).Encode(map[string]any{"templateId": "1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	filePath := filepath.Join(t.TempDir(), "template.html")
	require.NoError(t, os.WriteFile(filePath, []byte("<p>Body</p>"), 0o644))

	cmd := NewTemplatesCmd()
	cmd.SetArgs([]string{"create", "Test Template", "--file", filePath, "--output-mode", "json"})
	payload := captureTemplateJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, "1", payload["result"].(map[string]any)["templateId"])
}
