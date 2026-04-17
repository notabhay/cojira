package jira

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveUserReferenceSupportsMe(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "/rest/api/2/myself", r.URL.Path)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"displayName":  "Abhay",
			"emailAddress": "abhay@example.com",
			"name":         "abhay",
		})
	}))
	defer server.Close()

	client := testClient(t, server)
	user, err := resolveUserReference(client, "me")
	require.NoError(t, err)
	assert.Equal(t, "Abhay", user["displayName"])
}
