package dotenv

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseLines(t *testing.T) {
	content := `
# comment
FOO=bar
BAZ="quoted"
SINGLE='single'
export EXPORTED=val
  SPACED = spaces

EMPTY=
NO_EQUALS_LINE
=no_key
`
	result := ParseLines(content)

	assert.Equal(t, "bar", result["FOO"])
	assert.Equal(t, "quoted", result["BAZ"])
	assert.Equal(t, "single", result["SINGLE"])
	assert.Equal(t, "val", result["EXPORTED"])
	assert.Equal(t, "spaces", result["SPACED"])
	assert.Equal(t, "", result["EMPTY"])
	assert.NotContains(t, result, "NO_EQUALS_LINE")
	assert.NotContains(t, result, "")
}

func TestLoadIfPresent(t *testing.T) {
	ResetTracking()
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	err := os.WriteFile(envFile, []byte("COJIRA_TEST_VAR=hello\n"), 0644)
	require.NoError(t, err)

	// Ensure the var is not set yet.
	t.Setenv("COJIRA_TEST_VAR", "")
	_ = os.Unsetenv("COJIRA_TEST_VAR")

	loaded := LoadIfPresent([]string{envFile})
	assert.Equal(t, envFile, loaded.LoadedPath)
	assert.Equal(t, "hello", os.Getenv("COJIRA_TEST_VAR"))
	assert.Equal(t, envFile, SourceForKey("COJIRA_TEST_VAR"))
}

func TestLoadIfPresentDoesNotOverwrite(t *testing.T) {
	ResetTracking()
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	err := os.WriteFile(envFile, []byte("COJIRA_EXISTING=new\n"), 0644)
	require.NoError(t, err)

	t.Setenv("COJIRA_EXISTING", "original")

	loaded := LoadIfPresent([]string{envFile})
	assert.Equal(t, envFile, loaded.LoadedPath)
	assert.Equal(t, "original", os.Getenv("COJIRA_EXISTING"))
	assert.Equal(t, SourceEnvironment, SourceForKey("COJIRA_EXISTING"))
}

func TestLoadIfPresentNoFile(t *testing.T) {
	ResetTracking()
	loaded := LoadIfPresent([]string{"/nonexistent/.env"})
	assert.Empty(t, loaded.LoadedPath)
}

func TestLoadIfPresentMergesFilesWithEarlierPrecedence(t *testing.T) {
	ResetTracking()
	dir := t.TempDir()
	env1 := filepath.Join(dir, "a.env")
	env2 := filepath.Join(dir, "b.env")
	require.NoError(t, os.WriteFile(env1, []byte("COJIRA_FIRST=a\n"), 0o644))
	require.NoError(t, os.WriteFile(env2, []byte("COJIRA_FIRST=b\nCOJIRA_SECOND=c\n"), 0o644))

	t.Setenv("COJIRA_FIRST", "")
	_ = os.Unsetenv("COJIRA_FIRST")
	t.Setenv("COJIRA_SECOND", "")
	_ = os.Unsetenv("COJIRA_SECOND")

	loaded := LoadIfPresent([]string{env1, env2})
	assert.Equal(t, env1, loaded.LoadedPath)
	assert.Equal(t, []string{env1, env2}, loaded.LoadedPaths)
	assert.Equal(t, "a", os.Getenv("COJIRA_FIRST"))
	assert.Equal(t, "c", os.Getenv("COJIRA_SECOND"))
}

func TestProvenanceTracksLoadedAndEnvironmentSources(t *testing.T) {
	ResetTracking()
	dir := t.TempDir()
	envFile := filepath.Join(dir, ".env")
	require.NoError(t, os.WriteFile(envFile, []byte("COJIRA_FILE=file-value\nCOJIRA_EXISTING=file-ignored\n"), 0o644))

	t.Setenv("COJIRA_EXISTING", "shell-value")
	loaded := LoadIfPresent([]string{envFile})

	assert.Equal(t, envFile, loaded.LoadedPath)
	report := Provenance([]string{"COJIRA_FILE", "COJIRA_EXISTING", "COJIRA_MISSING"})
	assert.Equal(t, envFile, report["COJIRA_FILE"]["source"])
	assert.Equal(t, true, report["COJIRA_FILE"]["present"])
	assert.Equal(t, SourceEnvironment, report["COJIRA_EXISTING"]["source"])
	assert.Equal(t, true, report["COJIRA_EXISTING"]["present"])
	assert.Equal(t, "", report["COJIRA_MISSING"]["source"])
	assert.Equal(t, false, report["COJIRA_MISSING"]["present"])
}

func TestDefaultSearchPaths(t *testing.T) {
	paths := DefaultSearchPaths()
	require.NotEmpty(t, paths)
	assert.Contains(t, paths[0], ".env")
}

// --- Placeholder tests ---

func TestKnownPlaceholdersDetected(t *testing.T) {
	knowns := []string{
		"you@example.com",
		"your-email@example.com",
		"user@example.com",
		"your.email@example.com",
		"your-personal-access-token-here",
		"your-api-token-here",
	}
	for _, val := range knowns {
		assert.True(t, IsPlaceholder(val, ""), "expected placeholder: %s", val)
	}
}

func TestKnownPlaceholdersCaseInsensitive(t *testing.T) {
	assert.True(t, IsPlaceholder("You@Example.COM", ""))
	assert.True(t, IsPlaceholder("YOUR-API-TOKEN-HERE", ""))
}

func TestRealValuesPass(t *testing.T) {
	reals := []string{
		"alice@company.com",
		"bob@rakuten.com",
		"some-actual-token-abc123",
	}
	for _, val := range reals {
		assert.False(t, IsPlaceholder(val, ""), "false positive: %s", val)
	}
}

func TestNoneAndEmpty(t *testing.T) {
	assert.False(t, IsPlaceholder("", ""))
	assert.False(t, IsPlaceholder("  ", ""))
}

func TestEmailFieldHeuristic(t *testing.T) {
	assert.True(t, IsPlaceholder("your-team@example.org", "email"))
	assert.True(t, IsPlaceholder("yourname@example.net", "JIRA_EMAIL"))
	// Without field hint, generic heuristic doesn't apply.
	assert.False(t, IsPlaceholder("your-team@example.org", ""))
}

func TestWhitespaceStripped(t *testing.T) {
	assert.True(t, IsPlaceholder("  you@example.com  ", ""))
}
