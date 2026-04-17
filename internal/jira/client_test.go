package jira

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
		APIVersion:  "2",
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
	_, err := NewClient(ClientConfig{BaseURL: "https://jira.example.com"})
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.ConfigMissingEnv, ce.Code)
}

func TestNewClientBearerAuth(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL: "https://jira.example.com",
		Token:   "my-pat",
	})
	require.NoError(t, err)
	assert.Equal(t, "Bearer my-pat", c.headers.Get("Authorization"))
	assert.Nil(t, c.auth)
}

func TestNewClientBasicAuth(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL: "https://jira.example.com",
		Token:   "my-token",
		Email:   "user@company.com",
	})
	require.NoError(t, err)
	assert.NotNil(t, c.auth)
	assert.Equal(t, "user@company.com", c.auth.username)
	assert.Equal(t, "my-token", c.auth.password)
}

func TestNewClientPlaceholderEmailFallsBackToBearer(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL: "https://jira.example.com",
		Token:   "fake-token",
		Email:   "you@example.com",
	})
	require.NoError(t, err)
	assert.Nil(t, c.auth)
	assert.Equal(t, "Bearer fake-token", c.headers.Get("Authorization"))
}

func TestNewClientBearerAuthMode(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL:  "https://jira.example.com",
		Token:    "my-pat",
		Email:    "user@company.com",
		AuthMode: "bearer",
	})
	require.NoError(t, err)
	assert.Nil(t, c.auth)
	assert.Equal(t, "Bearer my-pat", c.headers.Get("Authorization"))
}

func TestConnectionErrorWrapped(t *testing.T) {
	// Use an invalid URL that will cause a connection error.
	c, err := NewClient(ClientConfig{
		BaseURL:     "http://127.0.0.1:1", // port 1 is almost certainly not listening
		Token:       "fake-token",
		Timeout:     100 * time.Millisecond,
		RetryConfig: noRetryConfig(),
	})
	require.NoError(t, err)

	_, err = c.Request("GET", "/myself", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.HTTPError, ce.Code)
	assert.Contains(t, ce.Message, "Network error")
}

func TestHTTP404Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = w.Write([]byte("Not Found"))
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.Request("GET", "/issue/PROJ-99999", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.HTTPError, ce.Code)
}

func TestHTTP401Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		_, _ = w.Write([]byte(`{"message":"Unauthorized"}`))
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.Request("GET", "/myself", nil, nil)
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
	_, err := c.Request("GET", "/myself", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.HTTP403, ce.Code)
	assert.NotEmpty(t, ce.Hint)
}

func TestHTTP429Error(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(429)
		_, _ = w.Write([]byte(`{"message":"Rate limit exceeded"}`))
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.Request("GET", "/myself", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Equal(t, cerrors.HTTP429, ce.Code)
	assert.NotEmpty(t, ce.Hint)
}

func TestGetIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/PROJ-123", r.URL.Path)
		assert.Equal(t, "summary,status", r.URL.Query().Get("fields"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":    "PROJ-123",
			"fields": map[string]any{"summary": "Test issue"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	issue, err := c.GetIssue("PROJ-123", "summary,status", "")
	require.NoError(t, err)
	assert.Equal(t, "PROJ-123", issue["key"])
}

func TestGetIssueUsesHTTPCache(t *testing.T) {
	callCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		assert.Equal(t, "/rest/api/2/issue/PROJ-123", r.URL.Path)
		w.Header().Set("ETag", `"issue-123"`)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"key":    "PROJ-123",
			"fields": map[string]any{"summary": "Cached issue"},
		})
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		Token:       "fake-token",
		APIVersion:  "2",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{TTL: time.Hour, Dir: t.TempDir()},
	})
	require.NoError(t, err)

	first, err := c.GetIssue("PROJ-123", "summary", "")
	require.NoError(t, err)
	second, err := c.GetIssue("PROJ-123", "summary", "")
	require.NoError(t, err)

	assert.Equal(t, "PROJ-123", first["key"])
	assert.Equal(t, "PROJ-123", second["key"])
	assert.Equal(t, 1, callCount)
}

func TestSearch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/search", r.URL.Path)
		assert.Equal(t, "project = PROJ", r.URL.Query().Get("jql"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total":  1,
			"issues": []map[string]any{{"key": "PROJ-1"}},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.Search("project = PROJ", 50, 0, "", "")
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["total"])
}

func TestValidateJQL(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/jql/parse", r.URL.Path)
		assert.Equal(t, http.MethodPost, r.Method)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, []any{"project = PROJ"}, payload["queries"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"queries": []map[string]any{{"query": "project = PROJ", "errors": []string{}}},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ValidateJQL("project = PROJ")
	require.NoError(t, err)
	queries, _ := result["queries"].([]any)
	require.Len(t, queries, 1)
}

func TestGetJQLAutoCompleteData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/jql/autocompletedata", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"visibleFieldNames":    []string{"status", "summary"},
			"visibleFunctionNames": []string{"currentUser()"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetJQLAutoCompleteData()
	require.NoError(t, err)
	assert.NotNil(t, result["visibleFieldNames"])
}

func TestListDashboards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/dashboard", r.URL.Path)
		assert.Equal(t, "25", r.URL.Query().Get("maxResults"))
		assert.Equal(t, "10", r.URL.Query().Get("startAt"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"dashboards": []map[string]any{
				{"id": "10000", "name": "Exec Dashboard"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListDashboards(25, 10)
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["total"])
}

func TestGetDashboard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/dashboard/10000", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   "10000",
			"name": "Exec Dashboard",
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetDashboard("10000")
	require.NoError(t, err)
	assert.Equal(t, "Exec Dashboard", result["name"])
}

func TestListDashboardGadgets(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/dashboard/10000/gadget", r.URL.Path)
		assert.Equal(t, "50", r.URL.Query().Get("maxResults"))
		assert.Equal(t, "0", r.URL.Query().Get("startAt"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"gadgets": []map[string]any{
				{"id": "20000", "title": "Filter Results"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListDashboardGadgets("10000", 50, 0)
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["total"])
}

func TestUpdateIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("notifyUsers"))
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.UpdateIssue("PROJ-123", map[string]any{"fields": map[string]any{"summary": "New"}}, true)
	require.NoError(t, err)
}

func TestCreateIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue", r.URL.Path)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"key": "PROJ-999", "id": "10001"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.CreateIssue(map[string]any{
		"fields": map[string]any{"summary": "New issue", "project": map[string]any{"key": "PROJ"}},
	})
	require.NoError(t, err)
	assert.Equal(t, "PROJ-999", result["key"])
}

func TestListTransitions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/transitions", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"transitions": []map[string]any{
				{"id": "31", "name": "Done"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListTransitions("PROJ-123")
	require.NoError(t, err)
	transitions := result["transitions"].([]any)
	require.Len(t, transitions, 1)
}

func TestGetMyself(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/myself", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"displayName":  "John Doe",
			"emailAddress": "john@example.com",
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetMyself()
	require.NoError(t, err)
	assert.Equal(t, "John Doe", result["displayName"])
}

func TestListFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/field", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "summary", "name": "Summary"},
			{"id": "priority", "name": "Priority"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	fields, err := c.ListFields()
	require.NoError(t, err)
	require.Len(t, fields, 2)
	assert.Equal(t, "summary", fields[0]["id"])
}

func TestListComments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/comment", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"comments": []map[string]any{
				{"id": "10", "body": "hello"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListComments("PROJ-123", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["total"])
}

func TestAddComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/comment", r.URL.Path)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "10"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.AddComment("PROJ-123", "hello")
	require.NoError(t, err)
	assert.Equal(t, "10", result["id"])
}

func TestAddCommentV3ConvertsPlainTextToADF(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/3/issue/PROJ-123/comment", r.URL.Path)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		body := payload["body"].(map[string]any)
		assert.Equal(t, "doc", body["type"])
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "10"})
	}))
	defer server.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     server.URL,
		Token:       "fake-token",
		APIVersion:  "3",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{Disabled: true},
	})
	require.NoError(t, err)

	result, err := c.AddComment("PROJ-123", "hello")
	require.NoError(t, err)
	assert.Equal(t, "10", result["id"])
}

func TestGetBoardIssuesUsesAPIBaseURL(t *testing.T) {
	var directServerCalled bool
	directServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		directServerCalled = true
		w.WriteHeader(500)
	}))
	defer directServer.Close()

	apiServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/agile/1.0/board/42/issue", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"issues": []map[string]any{}})
	}))
	defer apiServer.Close()

	c, err := NewClient(ClientConfig{
		BaseURL:     directServer.URL,
		APIBaseURL:  apiServer.URL,
		Token:       "fake-token",
		APIVersion:  "2",
		UserAgent:   "test/0.1",
		Timeout:     5 * time.Second,
		RetryConfig: noRetryConfig(),
		CacheConfig: httpclient.CacheConfig{Disabled: true},
	})
	require.NoError(t, err)

	_, err = c.GetBoardIssues("42", "", 10, 0, "")
	require.NoError(t, err)
	assert.False(t, directServerCalled)
}

func TestUpdateComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/comment/10", r.URL.Path)
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "10", "body": "updated"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UpdateComment("PROJ-123", "10", "updated")
	require.NoError(t, err)
	assert.Equal(t, "updated", result["body"])
}

func TestDeleteComment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/comment/10", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteComment("PROJ-123", "10"))
}

func TestCreateIssueLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issueLink", r.URL.Path)
		w.WriteHeader(201)
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.CreateIssueLink(map[string]any{"type": map[string]any{"name": "Relates"}})
	require.NoError(t, err)
}

func TestDeleteIssueLink(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/issueLink/77", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteIssueLink("77"))
}

func TestListIssueLinkTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "GET", r.Method)
		assert.Equal(t, "/rest/api/2/issueLinkType", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issueLinkTypes": []map[string]any{
				{"id": "1", "name": "Relates", "outward": "relates to", "inward": "relates to"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	items, err := c.ListIssueLinkTypes()
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "Relates", items[0]["name"])
}

func TestAssignIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/assignee", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.AssignIssue("PROJ-123", map[string]any{"name": "jdoe"})
	require.NoError(t, err)
}

func TestSearchUsers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/user/search", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"displayName": "Jane Doe", "emailAddress": "jane@example.com"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	users, err := c.SearchUsers("Jane", 20)
	require.NoError(t, err)
	require.Len(t, users, 1)
	assert.Equal(t, "Jane Doe", users[0]["displayName"])
}

func TestListProjects(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/project", r.URL.Path)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"key": "RAPTOR", "name": "Raptor"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	projects, err := c.ListProjects()
	require.NoError(t, err)
	require.Len(t, projects, 1)
	assert.Equal(t, "RAPTOR", projects[0]["key"])
}

func TestListCreateMetaIssueTypes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/createmeta/RAPTOR/issuetypes", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"id": "3", "name": "Task"},
				{"id": "1", "name": "Bug"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	items, err := c.ListCreateMetaIssueTypes("RAPTOR")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "Task", items[0]["name"])
}

func TestGetCreateMetaIssueTypeFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/createmeta/RAPTOR/issuetypes/3", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"fieldId": "priority", "name": "Priority"},
				{"fieldId": "summary", "name": "Summary"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	items, err := c.GetCreateMetaIssueTypeFields("RAPTOR", "3")
	require.NoError(t, err)
	require.Len(t, items, 2)
	assert.Equal(t, "priority", items[0]["fieldId"])
}

func TestGetWatchers(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/watchers", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"watchCount": 1,
			"watchers": []map[string]any{
				{"displayName": "Jane Doe"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetWatchers("PROJ-123")
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["watchCount"])
}

func TestAddWatcher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/watchers", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.AddWatcher("PROJ-123", "jdoe"))
}

func TestRemoveWatcher(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/watchers", r.URL.Path)
		assert.Equal(t, "jdoe", r.URL.Query().Get("username"))
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.RemoveWatcher("PROJ-123", "username", "jdoe"))
}

func TestListWorklogs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/worklog", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total": 1,
			"worklogs": []map[string]any{
				{"id": "77", "timeSpent": "1h"},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListWorklogs("PROJ-123", 20, 0)
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["total"])
}

func TestAddWorklog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/worklog", r.URL.Path)
		w.WriteHeader(201)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "77"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.AddWorklog("PROJ-123", map[string]any{"timeSpent": "1h"})
	require.NoError(t, err)
	assert.Equal(t, "77", result["id"])
}

func TestUpdateWorklog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/worklog/77", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "77"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UpdateWorklog("PROJ-123", "77", map[string]any{"timeSpent": "2h"})
	require.NoError(t, err)
	assert.Equal(t, "77", result["id"])
}

func TestDeleteWorklog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/worklog/77", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteWorklog("PROJ-123", "77"))
}

func TestDeleteIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123", r.URL.Path)
		assert.Equal(t, "true", r.URL.Query().Get("deleteSubtasks"))
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteIssue("PROJ-123", true))
}

func TestListBoardSprints(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/agile/1.0/board/45434/sprint", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{{"id": 1, "name": "Sprint 1"}},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListBoardSprints("45434", "", 20, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["values"])
}

func TestGetSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/agile/1.0/sprint/88", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 88, "name": "Sprint 88"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetSprint("88")
	require.NoError(t, err)
	assert.Equal(t, "Sprint 88", result["name"])
}

func TestCreateSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/agile/1.0/sprint", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 99, "name": "Sprint 99"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.CreateSprint(map[string]any{"name": "Sprint 99", "originBoardId": 45434})
	require.NoError(t, err)
	assert.Equal(t, "Sprint 99", result["name"])
}

func TestUpdateSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/agile/1.0/sprint/99", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": 99, "state": "active"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UpdateSprint("99", map[string]any{"state": "active"})
	require.NoError(t, err)
	assert.Equal(t, "active", result["state"])
}

func TestDeleteSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/agile/1.0/sprint/99", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteSprint("99"))
}

func TestAddIssuesToSprint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/agile/1.0/sprint/99/issue", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.AddIssuesToSprint("99", []string{"PROJ-1", "PROJ-2"}))
}

func TestGetBacklogIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/agile/1.0/board/45434/backlog", r.URL.Path)
		assert.Equal(t, "summary,status", r.URL.Query().Get("fields"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total":  1,
			"issues": []map[string]any{{"key": "PROJ-1"}},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetBacklogIssues("45434", "", 20, 0, "summary,status", "")
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["total"])
}

func TestRankIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "PUT", r.Method)
		assert.Equal(t, "/rest/agile/1.0/issue/rank", r.URL.Path)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "PROJ-9", payload["rankAfterIssue"])
		assert.Equal(t, float64(12345), payload["rankCustomFieldId"])
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.RankIssues([]string{"PROJ-1"}, "", "PROJ-9", 12345))
}

func TestMoveIssuesToBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/agile/1.0/board/45434/issue", r.URL.Path)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "PROJ-9", payload["rankBeforeIssue"])
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.MoveIssuesToBoard("45434", []string{"PROJ-1"}, "PROJ-9", "", 0))
}

func TestMoveIssuesToBacklog(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/agile/1.0/backlog/issue", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.MoveIssuesToBacklog([]string{"PROJ-1", "PROJ-2"}))
}

func TestMoveIssuesToBacklogForBoard(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/agile/1.0/backlog/45434/issue", r.URL.Path)
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "PROJ-9", payload["rankAfterIssue"])
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.MoveIssuesToBacklogForBoard("45434", []string{"PROJ-1"}, "", "PROJ-9", 0))
}

func TestListCustomerRequests(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/servicedeskapi/request", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"values": []map[string]any{{"issueKey": "RAPTOR-1"}}})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListCustomerRequests(25, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["values"])
}

func TestGetCustomerRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/servicedeskapi/request/RAPTOR-1", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{"issueKey": "RAPTOR-1"})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetCustomerRequest("RAPTOR-1")
	require.NoError(t, err)
	assert.Equal(t, "RAPTOR-1", result["issueKey"])
}

func TestGetEpicIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/agile/1.0/epic/RAPTOR-1/issue", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total":  1,
			"issues": []map[string]any{{"key": "PROJ-2"}},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetEpicIssues("RAPTOR-1", "", 20, 0, "summary", "")
	require.NoError(t, err)
	assert.Equal(t, float64(1), result["total"])
}

func TestMoveIssuesToEpic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/agile/1.0/epic/RAPTOR-1/issue", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.MoveIssuesToEpic("RAPTOR-1", []string{"PROJ-1"}))
}

func TestRemoveIssuesFromEpic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/agile/1.0/epic/none/issue", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.RemoveIssuesFromEpic([]string{"PROJ-1"}))
}

func TestUploadAttachment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/attachments", r.URL.Path)
		assert.Equal(t, "no-check", r.Header.Get("X-Atlassian-Token"))
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "55", "filename": "sample.txt"},
		})
	}))
	defer server.Close()

	dir := t.TempDir()
	path := filepath.Join(dir, "sample.txt")
	require.NoError(t, os.WriteFile(path, []byte("hello"), 0o644))

	c := testClient(t, server)
	result, err := c.UploadAttachment("PROJ-123", path)
	require.NoError(t, err)
	assert.Equal(t, "55", result["id"])
}

func TestUploadAttachmentBytes(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/attachments", r.URL.Path)
		assert.Equal(t, "no-check", r.Header.Get("X-Atlassian-Token"))
		assert.Contains(t, r.Header.Get("Content-Type"), "multipart/form-data")
		w.WriteHeader(200)
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{"id": "77", "filename": "stdin.txt"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.UploadAttachmentBytes("PROJ-123", "stdin.txt", []byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, "77", result["id"])
}

func TestDeleteAttachment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/rest/api/2/attachment/55", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	require.NoError(t, c.DeleteAttachment("55"))
}

func TestDownloadAttachment(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/files/sample.txt", r.URL.Path)
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()

	c := testClient(t, server)
	outPath := filepath.Join(t.TempDir(), "sample.txt")
	require.NoError(t, c.DownloadAttachment(server.URL+"/files/sample.txt", outPath))

	data, err := os.ReadFile(outPath)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestDownloadAttachmentContent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/files/sample.txt", r.URL.Path)
		_, _ = io.WriteString(w, "hello")
	}))
	defer server.Close()

	c := testClient(t, server)
	data, err := c.DownloadAttachmentContent(server.URL + "/files/sample.txt")
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))
}

func TestGetBoardIssues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Contains(t, r.URL.Path, "/rest/agile/1.0/board/45434/issue")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"total":  2,
			"issues": []map[string]any{{"key": "PROJ-1"}, {"key": "PROJ-2"}},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetBoardIssues("45434", "", 50, 0, "")
	require.NoError(t, err)
	assert.Equal(t, float64(2), result["total"])
}

func TestListBoards(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/agile/1.0/board", r.URL.Path)
		assert.Equal(t, "scrum", r.URL.Query().Get("type"))
		assert.Equal(t, "RAPTOR", r.URL.Query().Get("projectKeyOrId"))
		assert.Equal(t, "Delivery", r.URL.Query().Get("name"))
		_ = json.NewEncoder(w).Encode(map[string]any{
			"values": []map[string]any{
				{"id": 45434, "name": "Delivery Board", "type": "scrum"},
			},
			"total": 1,
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.ListBoards("scrum", "Delivery", "RAPTOR", 20, 0)
	require.NoError(t, err)
	assert.NotNil(t, result["values"])
	assert.Equal(t, float64(1), result["total"])
}

func TestGetBoardConfiguration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/agile/1.0/board/45434/configuration", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"id":   45434,
			"name": "Delivery Board",
			"columnConfig": map[string]any{
				"columns": []map[string]any{
					{"name": "To Do"},
				},
			},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	result, err := c.GetBoardConfiguration("45434")
	require.NoError(t, err)
	assert.Equal(t, "Delivery Board", result["name"])
}

func TestBaseURLTrailingSlashStripped(t *testing.T) {
	c, err := NewClient(ClientConfig{
		BaseURL: "https://jira.example.com/jira/",
		Token:   "tok",
	})
	require.NoError(t, err)
	assert.Equal(t, "https://jira.example.com/jira", c.baseURL)
	assert.Equal(t, "https://jira.example.com/jira/rest/api/2", c.restBase)
}

func TestFormatErrorWithJiraErrorMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"errorMessages": []string{"Issue does not exist"},
			"errors":        map[string]any{"summary": "Field required"},
		})
	}))
	defer server.Close()

	c := testClient(t, server)
	_, err := c.Request("GET", "/issue/BAD-1", nil, nil)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.True(t, errors.As(err, &ce))
	assert.Contains(t, ce.Message, "Issue does not exist")
	assert.Contains(t, ce.Message, "summary: Field required")
}

func TestTransitionIssue(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "POST", r.Method)
		assert.Equal(t, "/rest/api/2/issue/PROJ-123/transitions", r.URL.Path)
		w.WriteHeader(204)
	}))
	defer server.Close()

	c := testClient(t, server)
	err := c.TransitionIssue("PROJ-123", map[string]any{
		"transition": map[string]any{"id": "31"},
	}, true)
	require.NoError(t, err)
}
