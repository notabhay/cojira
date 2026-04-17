package oauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveAtlassianOAuth2WithExplicitCloudID(t *testing.T) {
	t.Setenv("JIRA_OAUTH_ACCESS_TOKEN", "access-token")
	t.Setenv("JIRA_OAUTH_CLOUD_ID", "cloud-123")

	resolved, err := ResolveAtlassianOAuth2(context.Background(), "jira", "https://example.atlassian.net", "JIRA")
	require.NoError(t, err)
	assert.Equal(t, "access-token", resolved.AccessToken)
	assert.Equal(t, "cloud-123", resolved.CloudID)
	assert.Equal(t, "https://api.atlassian.com/ex/jira/cloud-123", resolved.APIBaseURL)
}

func TestResolveAtlassianOAuth2RefreshesToken(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		require.NoError(t, json.NewDecoder(r.Body).Decode(&payload))
		assert.Equal(t, "refresh_token", payload["grant_type"])
		assert.Equal(t, "client-id", payload["client_id"])
		assert.Equal(t, "client-secret", payload["client_secret"])
		assert.Equal(t, "refresh-token", payload["refresh_token"])
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "fresh-token",
			"expires_in":   3600,
			"token_type":   "Bearer",
		})
	}))
	defer tokenServer.Close()

	t.Setenv("CONFLUENCE_OAUTH_ACCESS_TOKEN", "stale-token")
	t.Setenv("CONFLUENCE_OAUTH_EXPIRY", time.Now().Add(-1*time.Hour).Format(time.RFC3339))
	t.Setenv("CONFLUENCE_OAUTH_REFRESH_TOKEN", "refresh-token")
	t.Setenv("CONFLUENCE_OAUTH_CLIENT_ID", "client-id")
	t.Setenv("CONFLUENCE_OAUTH_CLIENT_SECRET", "client-secret")
	t.Setenv("CONFLUENCE_OAUTH_TOKEN_URL", tokenServer.URL)
	t.Setenv("CONFLUENCE_OAUTH_CLOUD_ID", "cloud-456")
	persistPath := filepath.Join(t.TempDir(), "credentials")
	t.Setenv("COJIRA_OAUTH_PERSIST_PATH", persistPath)

	resolved, err := ResolveAtlassianOAuth2(context.Background(), "confluence", "https://example.atlassian.net/wiki", "CONFLUENCE")
	require.NoError(t, err)
	assert.Equal(t, "fresh-token", resolved.AccessToken)
	assert.Equal(t, "cloud-456", resolved.CloudID)
	assert.Equal(t, "https://api.atlassian.com/ex/confluence/cloud-456", resolved.APIBaseURL)

	raw, err := os.ReadFile(persistPath)
	require.NoError(t, err)
	assert.Contains(t, string(raw), "CONFLUENCE_OAUTH_ACCESS_TOKEN=fresh-token")
	assert.Contains(t, string(raw), "CONFLUENCE_OAUTH_REFRESH_TOKEN=refresh-token")
	assert.Contains(t, string(raw), "CONFLUENCE_OAUTH_EXPIRY=")
}

func TestResolveAtlassianOAuth2AccessibleResources(t *testing.T) {
	resourceServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "Bearer access-token", r.Header.Get("Authorization"))
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"id":     "other",
				"url":    "https://other.atlassian.net",
				"scopes": []string{"read:jira-work"},
			},
			{
				"id":     "match",
				"url":    "https://example.atlassian.net",
				"scopes": []string{"read:jira-work"},
			},
		})
	}))
	defer resourceServer.Close()

	original := atlassianAccessibleResourcesURL
	atlassianAccessibleResourcesURL = resourceServer.URL
	defer func() { atlassianAccessibleResourcesURL = original }()

	t.Setenv("JIRA_OAUTH_ACCESS_TOKEN", "access-token")

	resolved, err := ResolveAtlassianOAuth2(context.Background(), "jira", "https://example.atlassian.net", "JIRA")
	require.NoError(t, err)
	assert.Equal(t, "match", resolved.CloudID)
	assert.Equal(t, "https://api.atlassian.com/ex/jira/match", resolved.APIBaseURL)
}

func TestPersistOAuthValuesMergesExistingEnvFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials")
	require.NoError(t, os.WriteFile(path, []byte("JIRA_BASE_URL=https://example\n"), 0o600))
	t.Setenv("COJIRA_OAUTH_PERSIST_PATH", path)

	token := &refreshedToken{
		AccessToken:  "new-access",
		RefreshToken: "new-refresh",
		ExpiresIn:    3600,
	}
	persistOAuthValues("JIRA", token, time.Date(2026, 4, 16, 0, 0, 0, 0, time.UTC))

	raw, err := os.ReadFile(path)
	require.NoError(t, err)
	content := string(raw)
	assert.Contains(t, content, "JIRA_BASE_URL=https://example")
	assert.Contains(t, content, "JIRA_OAUTH_ACCESS_TOKEN=new-access")
	assert.Contains(t, content, "JIRA_OAUTH_REFRESH_TOKEN=new-refresh")
	assert.Contains(t, content, "JIRA_OAUTH_EXPIRY=2026-04-16T00:00:00Z")
	assert.True(t, strings.HasSuffix(content, "\n"))
}
