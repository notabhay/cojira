package confluence

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
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

func TestBaseURLTrailingSlashStripped(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL: "https://confluence.example.com/confluence/",
		Token:   "tok",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://confluence.example.com/confluence", c.baseURL)
	assert.Equal(t, "https://confluence.example.com/confluence/rest/api", c.restBase)
}
