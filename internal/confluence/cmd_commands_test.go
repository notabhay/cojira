package confluence

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestConfluenceGetJSONIncludesRepresentation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		assert.Equal(t, "body.view", r.URL.Query().Get("expand"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "12345",
			"body": map[string]any{
				"view": map[string]any{"value": "<p>Rendered</p>"},
			},
		})
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewGetCmd(), []string{"12345", "--representation", "view", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, "view", result["representation"])
	assert.Equal(t, "<p>Rendered</p>", result["content"])
}

func TestConfluenceGetRejectsInvalidRepresentation(t *testing.T) {
	t.Setenv("CONFLUENCE_BASE_URL", "https://conf.example")
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewGetCmd(), []string{"12345", "--representation", "markdown", "--output-mode", "json"}, &payload))
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, float64(2), payload["exit_code"])
}

func TestConfluenceGetSavesRepresentationToFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "12345",
			"body": map[string]any{
				"view": map[string]any{"value": "<p>Saved</p>"},
			},
		})
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	outputPath := filepath.Join(t.TempDir(), "page.html")
	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewGetCmd(), []string{"12345", "--representation", "view", "-o", outputPath, "--output-mode", "json"}, &payload))

	result := payload["result"].(map[string]any)
	assert.Equal(t, outputPath, result["saved_to"])
	data, err := os.ReadFile(outputPath)
	require.NoError(t, err)
	assert.Equal(t, "<p>Saved</p>", string(data))
}

func TestConfluenceViewJSONFetchesRenderedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		assert.Equal(t, "body.view", r.URL.Query().Get("expand"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id": "12345",
			"body": map[string]any{
				"view": map[string]any{"value": "<p>Rendered view</p>"},
			},
		})
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewViewCmd(), []string{"12345", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, "view", result["representation"])
	assert.Equal(t, "<p>Rendered view</p>", result["content"])
	assert.Equal(t, "view", payload["command"])
}

func TestConfluenceInfoJSONIncludesLifecycleMetadata(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "version,history,space,ancestors,children.page", r.URL.Query().Get("expand"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "12345",
			"title": "Test Page",
			"space": map[string]any{"key": "TEAM"},
			"version": map[string]any{
				"number": 3,
				"when":   "2026-03-09T10:00:00.000+09:00",
				"by":     map[string]any{"displayName": "Kent"},
			},
			"history": map[string]any{
				"createdDate": "2026-03-01T09:00:00.000+09:00",
				"createdBy":   map[string]any{"displayName": "Abhay"},
			},
			"children": map[string]any{
				"page": map[string]any{"results": []any{}},
			},
		})
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewInfoCmd(), []string{"12345", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, "Kent", result["last_modified_by"])
	assert.Equal(t, "Abhay", result["created_by"])
	assert.Equal(t, "2026-03-09T10:00:00.000+09:00", result["last_modified"])
}

func TestConfluenceAPIJSONFetchesAllowlistedPath(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345/child/comment", r.URL.Path)
		assert.Equal(t, "body.view", r.URL.Query().Get("expand"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"id": "1"}},
		})
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewAPICmd(), []string{"GET", "/content/12345/child/comment?expand=body.view", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	results := result["results"].([]any)
	assert.Len(t, results, 1)
}

func TestConfluenceAPIRejectsAbsoluteURL(t *testing.T) {
	t.Setenv("CONFLUENCE_BASE_URL", "https://conf.example")
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewAPICmd(), []string{"GET", "https://example.com/rest/api/content/1", "--output-mode", "json"}, &payload))
	assert.Equal(t, false, payload["ok"])
	assert.Equal(t, float64(2), payload["exit_code"])
}

func TestConfluenceCommentsJSONResolvesInlineAnchorsFromStorage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345/child/comment":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{
						"id": "c1",
						"body": map[string]any{
							"view": map[string]any{"value": "<p>Inline comment</p>"},
						},
						"extensions": map[string]any{
							"location": "inline",
							"inlineProperties": map[string]any{
								"markerRef": "marker-1",
							},
						},
						"history": map[string]any{
							"createdDate": "2026-03-09T12:00:00.000+09:00",
							"createdBy":   map[string]any{"displayName": "Kent"},
						},
					},
					{
						"id": "c2",
						"body": map[string]any{
							"view": map[string]any{"value": "<p>Footer comment</p>"},
						},
						"history": map[string]any{
							"createdDate": "2026-03-09T13:00:00.000+09:00",
							"createdBy":   map[string]any{"displayName": "George"},
						},
					},
				},
			})
		case "/rest/api/content/12345":
			assert.Equal(t, "body.storage", r.URL.Query().Get("expand"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id": "12345",
				"body": map[string]any{
					"storage": map[string]any{
						"value": `<p>Before <ac:inline-comment-marker ac:ref="marker-1">selected text</ac:inline-comment-marker> after</p>`,
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

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewCommentsCmd(), []string{"12345", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, float64(2), result["total"])
	comments := result["comments"].([]any)
	inline := comments[0].(map[string]any)
	assert.Equal(t, "inline", inline["type"])
	assert.Equal(t, "selected text", inline["anchor_text"])
	assert.Equal(t, "storage", inline["anchor_status"])
}

func TestConfluenceCreateInfersSpaceFromParent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/98765":
			if r.URL.Query().Get("expand") == "space" {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"id":    "98765",
					"title": "Parent",
					"space": map[string]any{"key": "CAIS"},
				})
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "98765", "title": "Parent"})
		case "/rest/api/content":
			assert.Equal(t, "POST", r.Method)
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			space := payload["space"].(map[string]any)
			assert.Equal(t, "CAIS", space["key"])
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	t.Setenv("CONFLUENCE_BASE_URL", server.URL)
	t.Setenv("CONFLUENCE_API_TOKEN", "token")

	var payload map[string]any
	require.NoError(t, executeJSONCommand(t, NewCreateCmd(), []string{"New Title", "--parent", "98765", "--output-mode", "json"}, &payload))
	result := payload["result"].(map[string]any)
	assert.Equal(t, "CAIS", result["space"])
}

func executeJSONCommand(t *testing.T, cmd *cobra.Command, args []string, out *map[string]any) error {
	t.Helper()
	output.SetMode("")

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w
	defer func() { os.Stdout = origStdout }()

	cmd.SetArgs(args)
	err = cmd.Execute()

	_ = w.Close()
	buf, _ := io.ReadAll(r)
	require.NotEmpty(t, buf)
	require.NoError(t, json.Unmarshal(buf, out))
	return err
}
