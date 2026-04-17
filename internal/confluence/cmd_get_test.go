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

func captureConfluenceCommandOutput(t *testing.T, run func() error) map[string]any {
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

func TestGetMarkdownJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "12345",
				"body": map[string]any{
					"storage": map[string]any{
						"value": "<h1>Title</h1><p>Hello</p>",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewGetCmd()
	cmd.SetArgs([]string{"12345", "--format", "markdown", "--output-mode", "json"})
	payload := captureConfluenceCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, "markdown", result["format"])
	assert.Contains(t, result["content"], "# Title")
}

func TestExportDefaultsToMarkdownJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "12345",
				"body": map[string]any{
					"storage": map[string]any{
						"value": "<p>Hello</p>",
					},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewExportCmd()
	cmd.SetArgs([]string{"12345", "--output-mode", "json"})
	payload := captureConfluenceCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, "markdown", result["format"])
}

func TestWatchPageJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "12345",
				"title": "Watched Page",
				"version": map[string]any{
					"number": 3,
				},
				"history": map[string]any{
					"lastUpdated": map[string]any{"when": "2026-04-17T00:00:00Z"},
				},
				"body": map[string]any{
					"storage": map[string]any{"value": "<p>Hello</p>"},
				},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	cmd := NewWatchCmd()
	cmd.SetArgs([]string{"page", "12345", "--cycles", "1", "--output-mode", "json"})
	payload := captureConfluenceCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, "polling", result["transport"])
}

func TestExportPDFJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/spaces/flyingpdf/pdfpageexport.action":
			w.Header().Set("Content-Disposition", `attachment; filename="page.pdf"`)
			_, _ = w.Write([]byte("pdf-bytes"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	outPath := filepath.Join(t.TempDir(), "page.pdf")
	cmd := NewExportCmd()
	cmd.SetArgs([]string{"12345", "--format", "pdf", "--output", outPath, "--output-mode", "json"})
	payload := captureConfluenceCommandOutput(t, cmd.Execute)

	assert.Equal(t, true, payload["ok"])
	result := payload["result"].(map[string]any)
	assert.Equal(t, "pdf", result["format"])
	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, "pdf-bytes", string(data))
}
