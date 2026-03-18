package meta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBootstrapWritesFiles(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := NewBootstrapCmd()
	cmd.SetArgs([]string{})
	err = cmd.Execute()
	require.NoError(t, err)

	// Check bootstrap markdown was written.
	bootstrapPath := filepath.Join(tmpDir, "COJIRA-BOOTSTRAP.md")
	assert.FileExists(t, bootstrapPath)
	content, err := os.ReadFile(bootstrapPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "cojira bootstrap")

	// Check .env.example was written.
	envExample := filepath.Join(tmpDir, ".env.example")
	assert.FileExists(t, envExample)
	envContent, err := os.ReadFile(envExample)
	require.NoError(t, err)
	assert.Contains(t, string(envContent), "CONFLUENCE_BASE_URL")

	// Check an example payload exists.
	examplePayload := filepath.Join(tmpDir, "examples", "jira-create-payload.json")
	assert.FileExists(t, examplePayload)
	assert.FileExists(t, filepath.Join(tmpDir, "examples", "jira-create-template.json"))

	// Idempotent: second run should succeed without overwriting.
	cmd2 := NewBootstrapCmd()
	cmd2.SetArgs([]string{})
	err = cmd2.Execute()
	assert.NoError(t, err)
}

func TestBootstrapStdoutDoesNotWrite(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := NewBootstrapCmd()
	cmd.SetArgs([]string{"--stdout"})
	err = cmd.Execute()
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(tmpDir, "COJIRA-BOOTSTRAP.md"))
	assert.NoFileExists(t, filepath.Join(tmpDir, ".env.example"))
	assert.NoDirExists(t, filepath.Join(tmpDir, "examples"))
}

func TestBootstrapForce(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	// First run.
	cmd := NewBootstrapCmd()
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	// Modify a file.
	bsPath := filepath.Join(tmpDir, "COJIRA-BOOTSTRAP.md")
	require.NoError(t, os.WriteFile(bsPath, []byte("modified"), 0o644))

	// Run with --force.
	cmd2 := NewBootstrapCmd()
	cmd2.SetArgs([]string{"--force"})
	require.NoError(t, cmd2.Execute())

	content, err := os.ReadFile(bsPath)
	require.NoError(t, err)
	assert.Contains(t, string(content), "cojira bootstrap")
	assert.NotEqual(t, "modified", string(content))
}

func TestBootstrapNoExamples(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := NewBootstrapCmd()
	cmd.SetArgs([]string{"--no-examples"})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(tmpDir, "COJIRA-BOOTSTRAP.md"))
	assert.NoFileExists(t, filepath.Join(tmpDir, ".env.example"))
	assert.NoDirExists(t, filepath.Join(tmpDir, "examples"))
}

func TestReadAsset(t *testing.T) {
	content, err := readAsset("COJIRA-BOOTSTRAP.md")
	require.NoError(t, err)
	assert.Contains(t, content, "cojira")
}

func TestReadAssetNotFound(t *testing.T) {
	_, err := readAsset("nonexistent.txt")
	assert.Error(t, err)
}

func TestWriteTextFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "test.txt")

	status, err := writeTextFile(path, "hello", false)
	require.NoError(t, err)
	assert.Equal(t, "written", status)

	// Second write without force should skip.
	status, err = writeTextFile(path, "hello2", false)
	require.NoError(t, err)
	assert.Equal(t, "skipped", status)

	// Verify content unchanged.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello", string(data))

	// With force should overwrite.
	status, err = writeTextFile(path, "hello2", true)
	require.NoError(t, err)
	assert.Equal(t, "written", status)
	data, err = os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "hello2", string(data))
}

func TestWriteTextFileCreatesParentDirs(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "a", "b", "test.txt")

	status, err := writeTextFile(path, "hello", false)
	require.NoError(t, err)
	assert.Equal(t, "written", status)
	assert.FileExists(t, path)
}
