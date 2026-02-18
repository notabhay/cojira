package jira

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
		APIVersion:  "2",
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
