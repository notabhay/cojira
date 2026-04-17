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

	// Check workspace prompt files were written.
	assert.FileExists(t, filepath.Join(tmpDir, "AGENTS.md"))
	assert.FileExists(t, filepath.Join(tmpDir, "CLAUDE.md"))
	assert.NoFileExists(t, filepath.Join(tmpDir, "COJIRA-BOOTSTRAP.md"))
	assert.NoFileExists(t, filepath.Join(tmpDir, ".env.example"))
	assert.NoDirExists(t, filepath.Join(tmpDir, "examples"))

	// Idempotent: second run should succeed without overwriting.
	cmd2 := NewBootstrapCmd()
	cmd2.SetArgs([]string{})
	err = cmd2.Execute()
	assert.NoError(t, err)
}

func TestBootstrapWritesOnlyPromptFiles(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := NewBootstrapCmd()
	cmd.SetArgs([]string{})
	err = cmd.Execute()
	require.NoError(t, err)

	assert.NoFileExists(t, filepath.Join(tmpDir, "COJIRA-BOOTSTRAP.md"))
	assert.NoFileExists(t, filepath.Join(tmpDir, ".env.example"))
	assert.NoDirExists(t, filepath.Join(tmpDir, "examples"))
	assert.FileExists(t, filepath.Join(tmpDir, "AGENTS.md"))
	assert.FileExists(t, filepath.Join(tmpDir, "CLAUDE.md"))
}

func TestBootstrapDirFlag(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := NewBootstrapCmd()
	cmd.SetArgs([]string{"--dir", "nested"})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(tmpDir, "nested", "AGENTS.md"))
	assert.FileExists(t, filepath.Join(tmpDir, "nested", "CLAUDE.md"))
	assert.NoFileExists(t, filepath.Join(tmpDir, "nested", "COJIRA-BOOTSTRAP.md"))
}

func TestBootstrapDoesNotWriteExamples(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	cmd := NewBootstrapCmd()
	cmd.SetArgs([]string{})
	require.NoError(t, cmd.Execute())

	assert.NoFileExists(t, filepath.Join(tmpDir, "COJIRA-BOOTSTRAP.md"))
	assert.NoFileExists(t, filepath.Join(tmpDir, ".env.example"))
	assert.NoDirExists(t, filepath.Join(tmpDir, "examples"))
	assert.FileExists(t, filepath.Join(tmpDir, "AGENTS.md"))
	assert.FileExists(t, filepath.Join(tmpDir, "CLAUDE.md"))
}

func TestReadAsset(t *testing.T) {
	content, err := readAsset("workspace/AGENTS.md")
	require.NoError(t, err)
	assert.Contains(t, content, "cojira")
}

func TestWorkspaceAssetsStayInSyncWithRepoDocs(t *testing.T) {
	agentsAsset, err := readAsset("workspace/AGENTS.md")
	require.NoError(t, err)
	agentsRepo, err := os.ReadFile(filepath.Join("..", "..", "AGENTS.md"))
	require.NoError(t, err)
	assert.Equal(t, string(agentsRepo), agentsAsset)

	claudeAsset, err := readAsset("workspace/CLAUDE.md")
	require.NoError(t, err)
	claudeRepo, err := os.ReadFile(filepath.Join("..", "..", "CLAUDE.md"))
	require.NoError(t, err)
	assert.Equal(t, string(claudeRepo), claudeAsset)
	assert.Equal(t, agentsAsset, claudeAsset)
	assert.Equal(t, string(agentsRepo), string(claudeRepo))
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

func TestWriteWorkspacePromptFileCreatesNewFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "AGENTS.md")

	status, err := writeWorkspacePromptFile(path, "hello")
	require.NoError(t, err)
	assert.Equal(t, "written", status)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), workspacePromptBlockStart)
	assert.Contains(t, string(data), "hello")
	assert.Contains(t, string(data), workspacePromptBlockEnd)
}

func TestWriteWorkspacePromptFileMergesExistingFile(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "AGENTS.md")
	original := "# Existing\n\nKeep this.\n"
	require.NoError(t, os.WriteFile(path, []byte(original), 0o644))

	status, err := writeWorkspacePromptFile(path, "cojira block")
	require.NoError(t, err)
	assert.Equal(t, "merged", status)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "# Existing")
	assert.Contains(t, text, "Keep this.")
	assert.Contains(t, text, "cojira block")
	assert.Contains(t, text, workspacePromptBlockStart)
}

func TestWriteWorkspacePromptFileUpdatesExistingBlock(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "CLAUDE.md")
	existing := "# Existing\n\n" + workspacePromptBlockStart + "\nold\n" + workspacePromptBlockEnd + "\n"
	require.NoError(t, os.WriteFile(path, []byte(existing), 0o644))

	status, err := writeWorkspacePromptFile(path, "new")
	require.NoError(t, err)
	assert.Equal(t, "merged", status)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	text := string(data)
	assert.Contains(t, text, "# Existing")
	assert.NotContains(t, text, "\nold\n")
	assert.Contains(t, text, "\nnew\n")
}
