package confluence

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecuteBatchOpCreateSupportsMarkdown(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "page.md")
	require.NoError(t, os.WriteFile(bodyPath, []byte("# Architecture\n\nHello"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content", r.URL.Path)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "page", payload["type"])
		assert.Equal(t, "Architecture", payload["title"])
		assert.Equal(t, "CAIS", getNestedString(payload, "space", "key"))
		assert.Equal(t, "12345", getFirstAncestorID(payload))
		assert.Contains(t, getNestedString(payload, "body", "storage", "value"), "<h1>Architecture</h1>")

		_ = json.NewEncoder(w).Encode(map[string]any{"id": "999", "title": "Architecture"})
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":     "create",
		"title":  "Architecture",
		"space":  "CAIS",
		"parent": "12345",
		"file":   "page.md",
		"format": "markdown",
	}, "create", dir, false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "create page 'Architecture' in CAIS", desc)
	assert.Equal(t, "999", item["page"].(map[string]any)["id"])
}

func TestExecuteBatchOpCommentSupportsMarkdown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content", r.URL.Path)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "comment", payload["type"])
		assert.Equal(t, "12345", getNestedString(payload, "container", "id"))
		assert.Contains(t, getNestedString(payload, "body", "storage", "value"), "<h1>Review</h1>")

		_ = json.NewEncoder(w).Encode(map[string]any{"id": "456"})
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":     "comment",
		"page":   "12345",
		"body":   "# Review\n\nLooks good",
		"format": "markdown",
	}, "comment", t.TempDir(), false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "comment on 12345", desc)
	assert.Equal(t, "456", item["comment"].(map[string]any)["id"])
}

func TestExecuteBatchOpLabels(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path+"?"+r.URL.RawQuery)
		switch {
		case r.Method == "POST" && r.URL.Path == "/rest/api/content/12345/label":
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		case r.Method == "DELETE" && r.URL.Path == "/rest/api/content/12345/label":
			w.WriteHeader(204)
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":     "labels",
		"page":   "12345",
		"add":    []any{"important"},
		"remove": []any{"old"},
	}, "labels", t.TempDir(), false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "update labels on 12345", desc)
	assert.Len(t, calls, 2)
}

func TestExecuteBatchOpArchive(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == "GET" && r.URL.Path == "/rest/api/content/12345":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "12345",
				"title": "Page",
				"version": map[string]any{
					"number": 4,
				},
				"body": map[string]any{
					"storage": map[string]any{"value": "<p>body</p>"},
				},
			})
		case r.Method == "PUT" && r.URL.Path == "/rest/api/content/12345":
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, "98765", getFirstAncestorID(payload))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345"})
		case r.Method == "POST" && r.URL.Path == "/rest/api/content/12345/label":
			w.WriteHeader(200)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":        "archive",
		"page":      "12345",
		"to_parent": "98765",
		"label":     "archived",
	}, "archive", t.TempDir(), false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "archive 12345 under 98765", desc)
	assert.Len(t, calls, 3)
}

func TestExecuteBatchOpBlogCreateSupportsMarkdown(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "blog.md")
	require.NoError(t, os.WriteFile(bodyPath, []byte("# Blog\n\nHello"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content", r.URL.Path)

		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "blogpost", payload["type"])
		assert.Equal(t, "Release Notes", payload["title"])
		assert.Equal(t, "CAIS", getNestedString(payload, "space", "key"))
		assert.Contains(t, getNestedString(payload, "body", "storage", "value"), "<h1>Blog</h1>")

		_ = json.NewEncoder(w).Encode(map[string]any{"id": "777", "title": "Release Notes"})
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":     "blog-create",
		"title":  "Release Notes",
		"space":  "CAIS",
		"file":   "blog.md",
		"format": "markdown",
	}, "blog-create", dir, false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "create blog post 'Release Notes' in CAIS", desc)
	assert.Equal(t, "777", item["blog"].(map[string]any)["id"])
}

func TestExecuteBatchOpBlogUpdate(t *testing.T) {
	dir := t.TempDir()
	bodyPath := filepath.Join(dir, "blog.md")
	require.NoError(t, os.WriteFile(bodyPath, []byte("<p>updated</p>"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/rest/api/content/12345":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "12345",
				"title": "Release Notes",
				"version": map[string]any{
					"number": 2,
				},
			})
		case r.Method == "PUT" && r.URL.Path == "/rest/api/content/12345":
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, "blogpost", payload["type"])
			assert.Equal(t, "Release Notes", payload["title"])
			assert.Equal(t, float64(3), getNestedFloat(payload, "version", "number"))
			assert.Equal(t, "<p>updated</p>", getNestedString(payload, "body", "storage", "value"))
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":   "blog-update",
		"blog": "12345",
		"file": "blog.md",
	}, "blog-update", dir, false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "update blog 12345 from blog.md", desc)
}

func TestExecuteBatchOpBlogDelete(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{"op": "blog-delete", "blog": "12345"}, "blog-delete", t.TempDir(), false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "delete blog 12345", desc)
}

func TestExecuteBatchOpAttachmentUpload(t *testing.T) {
	dir := t.TempDir()
	filePath := filepath.Join(dir, "spec.txt")
	require.NoError(t, os.WriteFile(filePath, []byte("hello"), 0o644))

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content/12345/child/attachment", r.URL.Path)
		assert.Equal(t, "nocheck", r.Header.Get("X-Atlassian-Token"))
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "9", "title": "spec.txt"}}})
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{"op": "attachment-upload", "page": "12345", "file": "spec.txt"}, "attachment-upload", dir, false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "upload 1 attachment(s) to 12345", desc)
}

func TestExecuteBatchOpAttachmentDownload(t *testing.T) {
	dir := t.TempDir()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.Path == "/rest/api/content/12345/child/attachment":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "9", "title": "spec.txt", "_links": map[string]any{"download": "/download/attachments/12345/spec.txt"}},
				},
			})
		case r.Method == "GET" && r.URL.Path == "/download/attachments/12345/spec.txt":
			_, _ = io.WriteString(w, "hello")
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":         "attachment-download",
		"page":       "12345",
		"attachment": "9",
		"output":     "out/spec.txt",
	}, "attachment-download", dir, false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "download attachment 9 from 12345", desc)
	data, err := os.ReadFile(filepath.Join(dir, "out", "spec.txt"))
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestExecuteBatchOpCopyTreeUsesNativeAPI(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch {
		case r.Method == "POST" && r.URL.Path == "/rest/api/content/12345/pagehierarchy/copy":
			var payload map[string]any
			require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
			assert.Equal(t, "98765", payload["destinationPageId"])
			assert.Equal(t, true, payload["copyDescendants"])
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "task-1", "state": "queued"})
		default:
			t.Fatalf("unexpected request: %s %s", r.Method, r.URL.String())
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":     "copy-tree",
		"page":   "12345",
		"parent": "98765",
	}, "copy-tree", t.TempDir(), false, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "copy tree 12345 under 98765", desc)
	assert.Equal(t, "api", item["method"])
	assert.Len(t, calls, 1)
}

func TestExecuteBatchOpCopyTreeDryRunCountsPages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/content/12345/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "200"},
					{"id": "201"},
				},
			})
		case "/rest/api/content/200/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		case "/rest/api/content/201/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "300"},
				},
			})
		case "/rest/api/content/300/child/page":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{}})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	client := testClient(t, server)
	item := map[string]any{}
	desc := ""
	err := executeBatchOp(client, map[string]any{
		"op":     "copy-tree",
		"page":   "12345",
		"parent": "98765",
	}, "copy-tree", t.TempDir(), true, &desc, item)
	require.NoError(t, err)
	assert.Equal(t, "copy tree 12345 under 98765", desc)
	assert.Equal(t, 4, item["summary"].(map[string]any)["pages"])
}

func getFirstAncestorID(payload map[string]any) string {
	ancestors, _ := payload["ancestors"].([]any)
	if len(ancestors) == 0 {
		return ""
	}
	first, _ := ancestors[0].(map[string]any)
	return getNestedString(first, "id")
}
