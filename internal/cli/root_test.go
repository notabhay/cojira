package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeOutputModeAutoResolvesJSONOffTTY(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	AddOutputFlags(cmd, false)
	require.NoError(t, cmd.Flags().Set("output-mode", "auto"))

	mode := NormalizeOutputMode(cmd)
	assert.Equal(t, "json", mode)
	assert.True(t, IsJSON(cmd))
}

func TestValidateOutputModeRejectsUnsupportedValue(t *testing.T) {
	cmd := &cobra.Command{Use: "test"}
	AddOutputFlags(cmd, false)
	require.NoError(t, cmd.Flags().Set("output-mode", "xml"))

	err := ValidateOutputMode(cmd)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Unsupported output mode")
}

func TestExecuteReturnsConfigErrorForInvalidAliasConfig(t *testing.T) {
	tmpDir := t.TempDir()
	origDir, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tmpDir))
	defer func() { _ = os.Chdir(origDir) }()

	require.NoError(t, os.WriteFile(filepath.Join(tmpDir, ".cojira.json"), []byte("{"), 0o644))

	root := &cobra.Command{Use: "cojira"}
	origArgs := os.Args
	os.Args = []string{"cojira", "alias-name"}
	defer func() { os.Args = origArgs }()

	err = Execute(root)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid JSON")
}
