package meta

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeURLInput(t *testing.T) {
	assert.Equal(t, "https://example.com", normalizeURLInput("example.com"))
	assert.Equal(t, "https://example.com", normalizeURLInput("https://example.com"))
	assert.Equal(t, "", normalizeURLInput(""))
}

func TestInferConfluenceBaseURLWiki(t *testing.T) {
	url := "https://acme.atlassian.net/wiki/spaces/ENG/pages/123/Title"
	assert.Equal(t, "https://acme.atlassian.net/wiki", inferConfluenceBaseURL(url))
}

func TestInferConfluenceBaseURLDisplay(t *testing.T) {
	url := "https://confluence.example.com/display/SPACE/Page+Title"
	assert.Equal(t, "https://confluence.example.com", inferConfluenceBaseURL(url))
}

func TestTokenURLForBase(t *testing.T) {
	assert.Equal(t,
		"https://id.atlassian.com/manage-profile/security/api-tokens",
		tokenURLForBase("https://acme.atlassian.net/wiki"))
	assert.Equal(t,
		"https://confluence.example.com/wiki/plugins/servlet/personal-access-tokens",
		tokenURLForBase("https://confluence.example.com/wiki"))
	assert.Equal(t, "", tokenURLForBase(""))
}

func TestProbeJiraURLSucceedsOnFirstTry(t *testing.T) {
	origProbe := probeJiraURL
	probeJiraURL = func(baseURL string, timeout float64) string {
		if baseURL == "https://jira.example.com" {
			return "https://jira.example.com"
		}
		return ""
	}
	defer func() { probeJiraURL = origProbe }()

	result := probeJiraURL("https://jira.example.com", 10.0)
	assert.Equal(t, "https://jira.example.com", result)
}

func TestProbeJiraURLEmptyReturnsEmpty(t *testing.T) {
	origProbe := probeJiraURL
	probeJiraURL = func(baseURL string, timeout float64) string {
		if baseURL == "" {
			return ""
		}
		return baseURL
	}
	defer func() { probeJiraURL = origProbe }()

	result := probeJiraURL("", 10.0)
	assert.Equal(t, "", result)
}

func TestNonInteractiveExits3(t *testing.T) {
	cmd := NewInitCmd()
	cmd.SetArgs([]string{"--non-interactive"})
	err := cmd.Execute()
	assert.Error(t, err)
	exitErr, ok := err.(*exitError)
	assert.True(t, ok)
	assert.Equal(t, 3, exitErr.Code)
}

func TestNonInteractiveJSONExits3(t *testing.T) {
	origStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	cmd := NewInitCmd()
	cmd.SetArgs([]string{"--non-interactive", "--output-mode", "json"})
	err := cmd.Execute()

	_ = w.Close()
	os.Stdout = origStdout

	assert.Error(t, err)
	exitErr, ok := err.(*exitError)
	assert.True(t, ok)
	assert.Equal(t, 3, exitErr.Code)

	buf, _ := io.ReadAll(r)
	if len(buf) > 0 {
		var payload map[string]any
		jsonErr := json.Unmarshal(buf, &payload)
		if jsonErr == nil {
			assert.Equal(t, float64(3), payload["exit_code"])
			assert.Equal(t, false, payload["ok"])
		}
	}
}

func TestWriteCojiraJSONStubCreatesFile(t *testing.T) {
	tmpDir := t.TempDir()
	result := writeCojiraJSONStub(tmpDir, "", "")
	assert.NotEmpty(t, result)
	assert.Equal(t, filepath.Join(tmpDir, ".cojira.json"), result)

	data, err := os.ReadFile(result)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Contains(t, parsed, "jira")
	assert.Contains(t, parsed, "confluence")
	assert.Contains(t, parsed, "aliases")
	aliases, ok := parsed["aliases"].(map[string]any)
	assert.True(t, ok)
	assert.Empty(t, aliases)
}

func TestWriteCojiraJSONStubSkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	existing := filepath.Join(tmpDir, ".cojira.json")
	require.NoError(t, os.WriteFile(existing, []byte(`{"existing": true}`), 0o644))

	result := writeCojiraJSONStub(tmpDir, "", "")
	assert.Empty(t, result)

	// Verify original content was not overwritten.
	data, err := os.ReadFile(existing)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	assert.Equal(t, true, parsed["existing"])
}

func TestWriteCojiraJSONStubDetectsProjectFromURL(t *testing.T) {
	tmpDir := t.TempDir()
	result := writeCojiraJSONStub(tmpDir,
		"https://jira.example.com/jira/browse/MYPROJ-456", "")
	assert.NotEmpty(t, result)

	data, err := os.ReadFile(result)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	jiraSection, ok := parsed["jira"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "MYPROJ", jiraSection["default_project"])
	assert.Equal(t, "project = MYPROJ", jiraSection["default_jql_scope"])
}

func TestWriteCojiraJSONStubDetectsSpaceFromURL(t *testing.T) {
	tmpDir := t.TempDir()
	result := writeCojiraJSONStub(tmpDir, "",
		"https://confluence.example.com/confluence/display/TEAMX/Some+Page")
	assert.NotEmpty(t, result)

	data, err := os.ReadFile(result)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	confSection, ok := parsed["confluence"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "TEAMX", confSection["default_space"])
}

func TestWriteCojiraJSONStubDetectsProjectFromEnv(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("JIRA_PROJECT", "ENVPROJ")
	result := writeCojiraJSONStub(tmpDir, "", "")
	assert.NotEmpty(t, result)

	data, err := os.ReadFile(result)
	require.NoError(t, err)
	var parsed map[string]any
	require.NoError(t, json.Unmarshal(data, &parsed))
	jiraSection, ok := parsed["jira"].(map[string]any)
	assert.True(t, ok)
	assert.Equal(t, "ENVPROJ", jiraSection["default_project"])
}

func TestIsAlphaNumDash(t *testing.T) {
	assert.True(t, isAlphaNumDash("TEAMX"))
	assert.True(t, isAlphaNumDash("my-space"))
	assert.True(t, isAlphaNumDash("abc123"))
	assert.False(t, isAlphaNumDash("has space"))
	assert.False(t, isAlphaNumDash("has.dot"))
}
