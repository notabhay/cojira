package meta

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/cojira/cojira/internal/cli"
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

	envPath := filepath.Join(tmpDir, ".env")
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
	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if n > 0 {
		var payload map[string]any
		jsonErr := json.Unmarshal(buf[:n], &payload)
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
	written := appendEnvValues(path, values, map[string]string{})
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
	written := appendEnvValues(path, values, existing)
	assert.Equal(t, []string{"BAZ"}, written)
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
