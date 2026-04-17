package jira

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

func TestAttachmentDownloadAllJSON(t *testing.T) {
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/PROJ-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fields": map[string]any{
					"attachment": []map[string]any{
						{
							"id":       "55",
							"filename": "sample.txt",
							"content":  server.URL + "/files/sample.txt",
						},
					},
				},
			})
		case "/files/sample.txt":
			_, _ = w.Write([]byte("hello"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	outDir := t.TempDir()
	cmd := NewAttachmentCmd()
	cmd.SetArgs([]string{"PROJ-1", "--download-all", "--output-dir", outDir, "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	items := result["attachments"].([]any)
	require.Len(t, items, 1)
	_, err := os.ReadFile(filepath.Join(outDir, "sample.txt"))
	require.NoError(t, err)
}

func TestAttachmentSyncDryRunJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "local.txt"), []byte("hello"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/PROJ-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fields": map[string]any{
					"attachment": []map[string]any{},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewAttachmentCmd()
	cmd.SetArgs([]string{"PROJ-1", "--sync-dir", dir, "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, []any{"local.txt"}, result["upload_files"])
}

func TestAttachmentSyncDryRunConflictJSON(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "sample.txt"), []byte("local"), 0o644))

	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/2/issue/PROJ-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"fields": map[string]any{
					"attachment": []map[string]any{
						{
							"id":       "55",
							"filename": "sample.txt",
							"size":     6,
							"content":  server.URL + "/files/sample.txt",
						},
					},
				},
			})
		case "/files/sample.txt":
			_, _ = w.Write([]byte("remote"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewAttachmentCmd()
	cmd.SetArgs([]string{"PROJ-1", "--sync-dir", dir, "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	conflicts := result["conflicts"].([]any)
	require.Len(t, conflicts, 1)
}

func TestAttachmentStdinDryRunJSON(t *testing.T) {
	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()

	t.Setenv("JIRA_BASE_URL", server.URL)
	t.Setenv("JIRA_API_TOKEN", "token")

	cmd := NewAttachmentCmd()
	cmd.SetIn(strings.NewReader("hello from stdin"))
	cmd.SetArgs([]string{"PROJ-1", "--stdin", "--filename", "stdin.txt", "--dry-run", "--output-mode", "json"})
	payload := captureCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, true, result["dry_run"])
	assert.Equal(t, true, result["stdin"])
	assert.Equal(t, "stdin.txt", result["filename"])
}
