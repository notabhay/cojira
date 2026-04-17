package credstore

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseKnownEnv(t *testing.T) {
	values := ParseKnownEnv(map[string]string{
		"JIRA_BASE_URL":       "https://jira.example.com",
		"JIRA_API_TOKEN":      "token",
		"UNRELATED":           "ignore",
		"CONFLUENCE_BASE_URL": "",
	})
	assert.Equal(t, "https://jira.example.com", values["JIRA_BASE_URL"])
	assert.Equal(t, "token", values["JIRA_API_TOKEN"])
	_, ok := values["UNRELATED"]
	assert.False(t, ok)
}

func TestWritePlainCredentials(t *testing.T) {
	root := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", root)

	path, err := WritePlainCredentials(map[string]string{
		"JIRA_BASE_URL":  "https://jira.example.com",
		"JIRA_API_TOKEN": "token",
	})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(root, "cojira", "credentials"), path)
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "JIRA_BASE_URL=https://jira.example.com")
}

func TestResolveStoreNameFromEnv(t *testing.T) {
	t.Setenv("COJIRA_CRED_STORE", "keychain")
	assert.Equal(t, StoreKeyring, ResolveStoreName())
}
