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

func captureInlineCommentJSON(t *testing.T, run func() error) map[string]any {
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

func TestInlineCommentAddJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v2/inline-comments":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "99"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")
	t.Setenv("CONFLUENCE_API_VERSION", "2")

	cmd := NewInlineCommentCmd()
	cmd.SetArgs([]string{"add", "12345", "--body", "<p>Hello</p>", "--properties-json", `{"textSelectionMatchCount":1}`, "--output-mode", "json"})
	payload := captureInlineCommentJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
}
