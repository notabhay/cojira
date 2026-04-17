package meta

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCompletionManGeneratesFiles(t *testing.T) {
	root := &cobra.Command{Use: "cojira"}
	root.AddCommand(&cobra.Command{Use: "doctor"})

	outDir := t.TempDir()
	cmd := NewCompletionCmd(root)
	cmd.SetArgs([]string{"man", "--dir", outDir})
	require.NoError(t, cmd.Execute())

	entries, err := os.ReadDir(outDir)
	require.NoError(t, err)
	assert.NotEmpty(t, entries)
	assert.FileExists(t, filepath.Join(outDir, "cojira.1"))
}

func TestCompletionManCreatesMissingDirectory(t *testing.T) {
	root := &cobra.Command{Use: "cojira"}
	root.AddCommand(&cobra.Command{Use: "doctor"})

	baseDir := t.TempDir()
	outDir := filepath.Join(baseDir, "docs", "man")

	cmd := NewCompletionCmd(root)
	cmd.SetArgs([]string{"man", "--dir", outDir})
	require.NoError(t, cmd.Execute())

	assert.FileExists(t, filepath.Join(outDir, "cojira.1"))
}
