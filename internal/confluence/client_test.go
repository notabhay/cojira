package confluence

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/httpclient"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func noRetryConfig() httpclient.RetryConfig {
	return httpclient.RetryConfig{
		Retries:       0,
		RetryStatuses: map[int]bool{429: true, 500: true, 502: true, 503: true, 504: true},
		Sleep:         func(d time.Duration) {},
	}
}

func testClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		Token:       "fake-token",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{Disabled: true},
	})
	require.NoError(t, err)
	return c
}

func TestNewClientMissingBaseURL(t *testing.T) {
	_, err := NewClient(ClientConfig{Token: "tok"})
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.ConfigMissingEnv, ce.Code)
}

func TestNewClientMissingToken(t *testing.T) {
	_, err := NewClient(ClientConfig{BaseURL: "https://confluence.example.com"})
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.ConfigMissingEnv, ce.Code)
}

func TestConnectionErrorWrapped(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL:     "http://127.0.0.1:1",
		Token:       "fake-token",
		Timeout:     100 * time.Millisecond,
		RetryConfig: noRetryConfig(),
	})
	require.NoError(t, err)

	_, err = c.Request("GET", "/content/12345", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.HTTPError, ce.Code)
	assert.Contains(t, ce.Message, "Network error")
}

func TestHTTP401Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.Request("GET", "/content/12345", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.HTTP401, ce.Code)
	assert.NotEmpty(t, ce.Hint)
}

func TestHTTP403Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		_, _ = w.Write([]byte(`{"message":"Forbidden"}`))
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.Request("GET", "/content/12345", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.HTTP403, ce.Code)
}

func TestGetPageByID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		assert.Equal(t, "version,space", r.URL.Query().Get("expand"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "12345",
			"title": "Test Page",
			"space": map[string]any{"key": "TEAM"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	page, err := c.GetPageByID("12345", "version,space")
	require.NoError(t, err)
	assert.Equal(t, "12345", page["id"])
	assert.Equal(t, "Test Page", page["title"])
}

func TestGetPageByIDV2NormalizesPageShape(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/pages/12345", r.URL.Path)
		assert.Equal(t, "storage", r.URL.Query().Get("body-format"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":      "12345",
			"title":   "V2 Page",
			"spaceId": "777",
			"version": map[string]any{"number": 2},
			"body": map[string]any{
				"representation": "storage",
				"value":          "<p>hello</p>",
			},
			"parentId": "999",
		})
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		APIVersion:  "2",
		Token:       "fake-token",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{Disabled: true},
	})
	require.NoError(t, err)

	page, err := c.GetPageByID("12345", "")
	require.NoError(t, err)
	assert.Equal(t, "V2 Page", page["title"])
	assert.Equal(t, "777", safeString(page, "space", "key"))
	assert.Equal(t, "<p>hello</p>", getNestedString(page, "body", "storage", "value"))
}

func TestGetPageByIDUsesHTTPCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		w.Header().Set("ETag", `"page-12345"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":    "12345",
			"title": "Cached Page",
		})
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		Token:       "fake-token",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{TTL: time.Hour, Dir: t.TempDir()},
	})
	require.NoError(t, err)

	first, err := c.GetPageByID("12345", "")
	require.NoError(t, err)
	second, err := c.GetPageByID("12345", "")
	require.NoError(t, err)

	assert.Equal(t, "12345", first["id"])
	assert.Equal(t, "12345", second["id"])
	assert.Equal(t, 1, callCount)
}

func TestGetPageVersion(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		assert.Equal(t, "historical", r.URL.Query().Get("status"))
		assert.Equal(t, "2", r.URL.Query().Get("version"))
		assert.Equal(t, "body.storage,version", r.URL.Query().Get("expand"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":     "12345",
			"title":  "Old Title",
			"status": "historical",
			"version": map[string]any{
				"number": 2,
			},
			"body": map[string]any{
				"storage": map[string]any{"value": "<p>old</p>"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	page, err := c.GetPageVersion("12345", 2, "body.storage,version")
	require.NoError(t, err)
	assert.Equal(t, "historical", page["status"])
}

func TestGetPageByTitle(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content", r.URL.Path)
		assert.Equal(t, "TEAM", r.URL.Query().Get("spaceKey"))
		assert.Equal(t, "My Page", r.URL.Query().Get("title"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "12345", "title": "My Page"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	page, err := c.GetPageByTitle("TEAM", "My Page")
	require.NoError(t, err)
	require.NotNil(t, page)
	assert.Equal(t, "12345", page["id"])
}

func TestListBlogPosts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content", r.URL.Path)
		assert.Equal(t, "blogpost", r.URL.Query().Get("type"))
		assert.Equal(t, "TEAM", r.URL.Query().Get("spaceKey"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"size": 1,
			"results": []map[string]any{
				{"id": "77", "title": "Release Notes", "type": "blogpost"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListBlogPosts("TEAM", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["size"])
}

func TestGetPageByTitleNotFound(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	c := testClient(t, server)
	page, err := c.GetPageByTitle("TEAM", "Nonexistent")
	require.NoError(t, err)
	assert.Nil(t, page)
}

func TestUpdatePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345", "title": "Updated"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UpdatePage("12345", map[string]any{
		"title":   "Updated",
		"type":    "page",
		"version": map[string]any{"number": 2},
		"body": map[string]any{
			"storage": map[string]any{"value": "<p>hello</p>", "representation": "storage"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "Updated", result["title"])
}

func TestCreatePage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content", r.URL.Path)
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "99999", "title": "New Page"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.CreatePage(map[string]any{
		"type":  "page",
		"title": "New Page",
		"space": map[string]any{"key": "TEAM"},
		"body": map[string]any{
			"storage": map[string]any{"value": "<p>content</p>", "representation": "storage"},
		},
	})
	require.NoError(t, err)
	assert.Equal(t, "99999", result["id"])
}

func TestSetPageLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content/12345/label", r.URL.Path)
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.SetPageLabel("12345", "archived")
	require.NoError(t, err)
}

func TestDeletePageLabel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/content/12345/label", r.URL.Path)
		assert.Equal(t, "archived", r.URL.Query().Get("name"))
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.DeletePageLabel("12345", "archived")
	require.NoError(t, err)
}

func TestGetPageLabels(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345/label", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"name": "archived"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetPageLabels("12345", 25, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestGetPageHistory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345/history", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"createdDate": "2024-01-01T00:00:00.000+0000",
			"lastUpdated": map[string]any{
				"when": "2024-01-02T00:00:00.000+0000",
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetPageHistory("12345")
	require.NoError(t, err)
	assert.Equal(t, "2024-01-01T00:00:00.000+0000", result["createdDate"])
}

func TestUpdateRestrictions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/content/12345/restriction", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"updated": true})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UpdateRestrictions("12345", []map[string]any{
		{"operation": "read", "restrictions": map[string]any{"user": []map[string]any{}, "group": []map[string]any{}}},
	})
	require.NoError(t, err)
	assert.Equal(t, true, result["updated"])
}

func TestListAttachments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345/child/attachment", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"size": 1,
			"results": []map[string]any{
				{"id": "9", "title": "spec.pdf"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListAttachments("12345", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["size"])
}

func TestUploadAttachment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content/12345/child/attachment", r.URL.Path)
		assert.Equal(t, "nocheck", r.Header.Get("X-Atlassian-Token"))
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "9", "title": "spec.pdf"},
			},
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "spec.pdf")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	c := testClient(t, server)
	result, err := c.UploadAttachment("12345", path)
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestUploadAttachmentBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content/12345/child/attachment", r.URL.Path)
		assert.Equal(t, "nocheck", r.Header.Get("X-Atlassian-Token"))
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "10", "title": "stdin.txt"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UploadAttachmentBytes("12345", "stdin.txt", []byte("hello"))
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestDownloadAttachment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/download/attachments/12345/spec.pdf", r.URL.Path)
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()

	c := testClient(t, server)
	outPath := filepath.Join(t.TempDir(), "spec.pdf")
	require.NoError(t, c.DownloadAttachment("/download/attachments/12345/spec.pdf", outPath))
	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestDownloadAttachmentContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/download/attachments/12345/spec.pdf", r.URL.Path)
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()

	c := testClient(t, server)
	data, err := c.DownloadAttachmentContent("/download/attachments/12345/spec.pdf")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestDeleteAttachment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/content/9", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteAttachment("9"))
}

func TestDownloadPageExportPDF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/spaces/flyingpdf/pdfpageexport.action", r.URL.Path)
		assert.Equal(t, "12345", r.URL.Query().Get("pageId"))
		w.Header().Set("Content-Disposition", `attachment; filename="page.pdf"`)
		_, _ = io.WriteString(w, "pdf-bytes")
	}))
	defer server.Close()

	c := testClient(t, server)
	data, filename, err := c.DownloadPageExport("12345", "pdf")
	require.NoError(t, err)
	assert.Equal(t, "page.pdf", filename)
	assert.Equal(t, "pdf-bytes", string(data))
}

func TestListPageComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/12345/child/comment", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"id": "200"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListPageComments("12345", 20, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestAddPageComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "200"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.AddPageComment("12345", "<p>hello</p>")
	require.NoError(t, err)
	assert.Equal(t, "200", result["id"])
}

func TestUpdatePageComment(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			assert.Equal(t, "GET", r.Method)
			assert.Equal(t, "/rest/api/content/200", r.URL.Path)
			assert.Equal(t, "version,container", r.URL.Query().Get("expand"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":   "200",
				"type": "comment",
				"version": map[string]any{
					"number": 2,
				},
				"container": map[string]any{
					"type": "page",
					"id":   "12345",
				},
			})
		case 2:
			assert.Equal(t, "PUT", r.Method)
			assert.Equal(t, "/rest/api/content/200", r.URL.Path)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "200"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UpdatePageComment("200", "<p>updated</p>")
	require.NoError(t, err)
	assert.Equal(t, "200", result["id"])
}

func TestDeletePageComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/content/200", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeletePageComment("200"))
}

func TestListInlineComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/pages/12345/inline-comments", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"id": "99"}},
		})
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		APIVersion:  "2",
		Token:       "fake-token",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{Disabled: true},
	})
	require.NoError(t, err)
	result, err := c.ListInlineComments("12345", 25, "", "storage")
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestCreateInlineComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/api/v2/inline-comments", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "99"})
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		APIVersion:  "2",
		Token:       "fake-token",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{Disabled: true},
	})
	require.NoError(t, err)
	result, err := c.CreateInlineComment(map[string]any{"pageId": "12345"})
	require.NoError(t, err)
	assert.Equal(t, "99", result["id"])
}

func TestListPageProperties(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/api/v2/pages/12345/properties", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"id": "1", "key": "foo"}}})
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		APIVersion:  "2",
		Token:       "fake-token",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{Disabled: true},
	})
	require.NoError(t, err)
	result, err := c.ListPageProperties("12345", 25)
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestRestoreTrashedContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/content/12345/version", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"restored": true})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.RestoreTrashedContent("12345", 3, "restore", true)
	require.NoError(t, err)
	assert.Equal(t, true, result["restored"])
}

func TestDeleteContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteContent("12345"))
}

func TestGetChildren(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Contains(t, r.URL.Path, "/rest/api/content/12345/child/page")
		if callCount == 1 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"results": []map[string]any{
					{"id": "100", "title": "Child 1"},
					{"id": "101", "title": "Child 2"},
				},
			})
		} else {
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []any{}})
		}
	}))
	defer server.Close()

	c := testClient(t, server)
	children, err := c.GetChildren("12345", 50)
	require.NoError(t, err)
	require.Len(t, children, 2)
	assert.Equal(t, "100", children[0]["id"])
}

func TestCQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/content/search", r.URL.Path)
		assert.Contains(t, r.URL.Query().Get("cql"), "title")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{{"id": "1", "title": "Found"}},
			"size":    1,
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.CQL("title = 'Test'", 25, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestGetCurrentUser(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/user/current", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"displayName": "Jane Doe",
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	user, err := c.GetCurrentUser()
	require.NoError(t, err)
	assert.Equal(t, "Jane Doe", user["displayName"])
}

func TestListSpaces(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/space", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"results": []map[string]any{
				{"key": "TEAM", "name": "Team Space"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListSpaces(25, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestGetSpace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/rest/api/space/TEAM", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "TEAM", "name": "Team Space"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetSpace("TEAM")
	require.NoError(t, err)
	assert.Equal(t, "TEAM", result["key"])
}

func TestCreateSpace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/space", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "TEAM", "name": "Team Space"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.CreateSpace(map[string]any{"key": "TEAM", "name": "Team Space"})
	require.NoError(t, err)
	assert.Equal(t, "TEAM", result["key"])
}

func TestUpdateSpace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/space/TEAM", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "TEAM", "name": "Updated Space"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UpdateSpace("TEAM", map[string]any{"key": "TEAM", "name": "Updated Space"})
	require.NoError(t, err)
	assert.Equal(t, "Updated Space", result["name"])
}

func TestDeleteSpace(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/space/TEAM", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteSpace("TEAM"))
}

func TestListTemplates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/template/page", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"templateId": "1"}}})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListTemplates("", 25, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["results"])
}

func TestCreateTemplate(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/template", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"templateId": "1"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.CreateTemplate(map[string]any{"name": "Test"})
	require.NoError(t, err)
	assert.Equal(t, "1", result["templateId"])
}

func TestMovePage(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		switch callCount {
		case 1:
			assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
			assert.Equal(t, "version,body.storage", r.URL.Query().Get("expand"))
			_ = json.NewEncoder(w).Encode(map[string]any{
				"id":    "12345",
				"title": "Child",
				"version": map[string]any{
					"number": 2,
				},
				"body": map[string]any{
					"storage": map[string]any{"value": "<p>body</p>"},
				},
			})
		case 2:
			assert.Equal(t, "/rest/api/content/12345", r.URL.Path)
			assert.Equal(t, "PUT", r.Method)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "12345"})
		default:
			t.Fatalf("unexpected request %d", callCount)
		}
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.MovePage("12345", "99999")
	require.NoError(t, err)
	assert.Equal(t, "12345", result["id"])
}

func TestBaseURLTrailingSlashStripped(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL: "https://confluence.example.com/confluence/",
		Token:   "tok",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://confluence.example.com/confluence", c.baseURL)
	assert.Equal(t, "https://confluence.example.com/confluence/rest/api", c.restBase)
}
