package meta

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/dotenv"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCheckJiraMissingEnv(t *testing.T) {
	// Clear env.
	for _, k := range []string{"JIRA_BASE_URL", "JIRA_API_TOKEN", "JIRA_EMAIL",
		"JIRA_API_VERSION", "JIRA_AUTH_MODE", "JIRA_VERIFY_SSL", "JIRA_USER_AGENT"} {
		t.Setenv(k, "")
	}

	result := checkJira(defaultRetryConfig())
	assert.False(t, result.OK)
	assert.Equal(t, "jira", result.Name)
	assert.NotNil(t, result.Error)
	assert.Equal(t, "CONFIG_MISSING_ENV", result.Error["code"])

	details := result.Details
	missing, ok := details["missing_env"].([]string)
	assert.True(t, ok)
	assert.Contains(t, missing, "JIRA_BASE_URL")
	assert.Contains(t, missing, "JIRA_API_TOKEN")
}

func TestCheckConfluenceMissingEnv(t *testing.T) {
	t.Setenv("CONFLUENCE_BASE_URL", "")
	t.Setenv("CONFLUENCE_API_TOKEN", "")

	result := checkConfluence(defaultRetryConfig())
	assert.False(t, result.OK)
	assert.Equal(t, "confluence", result.Name)
	assert.NotNil(t, result.Error)
	assert.Equal(t, "CONFIG_MISSING_ENV", result.Error["code"])

	details := result.Details
	missing, ok := details["missing_env"].([]string)
	assert.True(t, ok)
	assert.Contains(t, missing, "CONFLUENCE_BASE_URL")
	assert.Contains(t, missing, "CONFLUENCE_API_TOKEN")
}

func TestDoctorFixWritesEnv(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	// Ensure the global credentials path resolves inside the temp dir.
	t.Setenv("XDG_CONFIG_HOME", tmpDir)

	// Clear required env vars.
	for _, key := range []string{"CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN",
		"JIRA_BASE_URL", "JIRA_API_TOKEN"} {
		t.Setenv(key, "")
	}

	values := map[string]string{
		"CONFLUENCE_BASE_URL":  "https://conf.example",
		"CONFLUENCE_API_TOKEN": "conf-token",
		"JIRA_BASE_URL":        "https://jira.example",
		"JIRA_API_TOKEN":       "jira-token",
	}

	// Override promptMissingEnv to return test values.
	origPrompt := promptMissingEnv
	promptMissingEnv = func(missing []string) map[string]string {
		result := map[string]string{}
		for _, k := range missing {
			if v, ok := values[k]; ok {
				result[k] = v
			}
		}
		return result
	}
	defer func() { promptMissingEnv = origPrompt }()

	fixResult := runFix(false)
	assert.NotNil(t, fixResult)
	written, ok := fixResult["written"].([]string)
	assert.True(t, ok)
	assert.Len(t, written, 4)

	envPath := filepath.Join(tmpDir, "cojira", "credentials")
	assert.FileExists(t, envPath)
	content, err := os.ReadFile(envPath)
	require.NoError(t, err)
	text := string(content)
	for key, value := range values {
		assert.Contains(t, text, key+"=\""+value+"\"")
	}
}

func TestFixWithoutInteractiveExits3(t *testing.T) {
	cmd := NewDoctorCmd()
	cmd.SetArgs([]string{"--fix"})
	err := cmd.Execute()
	assert.Error(t, err)
	exitErr, ok := err.(*exitError)
	assert.True(t, ok)
	assert.Equal(t, 3, exitErr.Code)
}

func TestFixWithoutInteractiveJSONExits3(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	// Redirect stdout to capture JSON output.
	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	cmd := NewDoctorCmd()
	cmd.SetArgs([]string{"--fix", "--output-mode", "json"})
	err = cmd.Execute()

	_ = w.Close()
	os.Stdout = origStdout

	assert.Error(t, err)
	exitErr, ok := err.(*exitError)
	assert.True(t, ok)
	assert.Equal(t, 3, exitErr.Code)

	// Read captured output.
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

func TestToBool(t *testing.T) {
	assert.True(t, toBool("true", false))
	assert.True(t, toBool("1", false))
	assert.True(t, toBool("yes", false))
	assert.False(t, toBool("false", true))
	assert.False(t, toBool("0", true))
	assert.False(t, toBool("no", true))
	assert.True(t, toBool("", true))
	assert.False(t, toBool("", false))
	assert.True(t, toBool("garbage", true))
}

func TestAppendEnvValues(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".env")

	values := map[string]string{"FOO": "bar", "BAZ": "qux"}
	written, err := appendEnvValues(path, values, map[string]string{})
	require.NoError(t, err)
	assert.Len(t, written, 2)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, `FOO="bar"`)
	assert.Contains(t, text, `BAZ="qux"`)
}

func TestAppendEnvValuesSkipsExisting(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, ".env")
	require.NoError(t, os.WriteFile(path, []byte("FOO=existing\n"), 0o644))

	values := map[string]string{"FOO": "bar", "BAZ": "qux"}
	existing := map[string]string{"FOO": "existing"}
	written, err := appendEnvValues(path, values, existing)
	require.NoError(t, err)
	assert.Equal(t, []string{"BAZ"}, written)
}

func TestDoctorJSONIncludesEnvLoadingAndSources(t *testing.T) {
	dotenv.ResetTracking()
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	confServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/rest/api/user/current":
			_ = json.NewEncoder(w).Encode(map[string]any{"displayName": "Conf User", "accountId": "abc"})
		case "/rest/api/space":
			_ = json.NewEncoder(w).Encode(map[string]any{"results": []map[string]any{{"key": "TEAM"}}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer confServer.Close()

	envPath := filepath.Join(tmpDir, ".env")
	require.NoError(t, os.WriteFile(envPath, []byte(
		"CONFLUENCE_BASE_URL="+confServer.URL+"\nCONFLUENCE_API_TOKEN=conf-token\n",
	), 0o644))
	unsetEnvForLoadTest(t, "CONFLUENCE_BASE_URL", "CONFLUENCE_API_TOKEN")

	t.Setenv("JIRA_BASE_URL", "")
	t.Setenv("JIRA_API_TOKEN", "")

	origStdout := os.Stdout
	r, w, err := os.Pipe()
	require.NoError(t, err)
	os.Stdout = w

	cmd := NewDoctorCmd()
	cmd.SetArgs([]string{"--output-mode", "json", "--retries", "0", "--timeout", "1"})
	err = cmd.Execute()

	_ = w.Close()
	os.Stdout = origStdout

	assert.Error(t, err)
	exitErr, ok := err.(*exitError)
	require.True(t, ok)
	assert.Equal(t, 1, exitErr.Code)

	buf, _ := io.ReadAll(r)
	require.NotEmpty(t, buf)

	var payload map[string]any
	require.NoError(t, json.Unmarshal(buf, &payload))
	result := payload["result"].(map[string]any)
	envLoading := result["env_loading"].(map[string]any)
	envSources := result["env_sources"].(map[string]any)
	assert.Equal(t, canonicalPathForTest(envPath), canonicalPathForTest(envLoading["loaded_path"].(string)))

	confBase := envSources["CONFLUENCE_BASE_URL"].(map[string]any)
	assert.Equal(t, canonicalPathForTest(envPath), canonicalPathForTest(confBase["source"].(string)))
}

func defaultRetryConfig() RetryConfig {
	return RetryConfig{
		Timeout:        10.0,
		Retries:        1,
		RetryBaseDelay: 0.5,
		RetryMaxDelay:  2.0,
		Debug:          false,
	}
}

// RetryConfig alias for test usage.
type RetryConfig = cli.RetryConfig
