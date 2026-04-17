package confluence

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func captureConfluenceJSON(t *testing.T, run func() error) map[string]any {
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

func TestAttachmentDownloadAllJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345/child/attachment":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "9", "title": "spec.pdf", "_links": map[string]any{"download": "/download/attachments/12345/spec.pdf"}},
				},
			})
		case "/download/attachments/12345/spec.pdf":
			_, _ = w.Write([]byte("hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	outDir := t.TempDir()
	cmd := NewAttachmentCmd()
	cmd.SetArgs([]string{"12345", "--download-all", "--output-dir", outDir, "--output-mode", "json"})
	payload := captureConfluenceJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	_, err := os.ReadFile(filepath.Join(outDir, "spec.pdf"))
	require.NoError(t, err)
}

func TestAttachmentSyncDryRunJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "spec.pdf"), []byte("hello"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345/child/attachment":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewAttachmentCmd()
	cmd.SetArgs([]string{"12345", "--sync-dir", dir, "--dry-run", "--output-mode", "json"})
	payload := captureConfluenceJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, []any{"spec.pdf"}, result["upload_files"])
}

func TestAttachmentStdinDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewAttachmentCmd()
	cmd.SetIn(strings.NewReader("hello from stdin"))
	cmd.SetArgs([]string{"12345", "--stdin", "--filename", "stdin.txt", "--dry-run", "--output-mode", "json"})
	payload := captureConfluenceJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, true, result["stdin"])
	assert.Equal(t, "stdin.txt", result["filename"])
}

func TestCommentEditJSON(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			assert.Equal(t, "/rest/api/content/200", r.URL.Path)
			assert.Equal(t, "GET", r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":        "200",
				"version":   map[string]any{"number": 1},
				"container": map[string]any{"type": "page", "id": "12345"},
			})
		case 2:
			assert.Equal(t, "/rest/api/content/200", r.URL.Path)
			assert.Equal(t, "PUT", r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "200"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewCommentCmd()
	cmd.SetArgs([]string{"12345", "--edit", "200", "--add", "<p>updated</p>", "--output-mode", "json"})
	payload := captureConfluenceJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["updated"])
}

func TestSpacesCreateJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/space", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "TEAM", "name": "Team Space"})
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewSpacesCmd()
	cmd.SetArgs([]string{"create", "TEAM", "--name", "Team Space", "--output-mode", "json"})
	payload := captureConfluenceJSON(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	assert.Equal(t, "TEAM", payload["result"].(map[string]any)["key"])
}
