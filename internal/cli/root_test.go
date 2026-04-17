package cli

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestIsBuiltInToolUsesRegisteredCommands(t *testing.T) {
	root := NewRootCmd("test")
	root.AddCommand(&cobra.Command{Use: "doctor", Aliases: []string{"doc"}})

	assert.True(t, isBuiltInTool(root, "doctor"))
	assert.True(t, isBuiltInTool(root, "doc"))
	assert.False(t, isBuiltInTool(root, "jira"))
}

func TestBuildRootLongIncludesRegisteredCommands(t *testing.T) {
	root := NewRootCmd("test")
	root.AddCommand(&cobra.Command{Use: "doctor", Short: "Run checks", Run: func(_ *cobra.Command, _ []string) {}})
	root.AddCommand(&cobra.Command{Use: "auth", Short: "Auth helpers", Run: func(_ *cobra.Command, _ []string) {}})

	long := buildRootLong(root)

	assert.Contains(t, long, "doctor")
	assert.Contains(t, long, "Run checks")
	assert.Contains(t, long, "auth")
	assert.Contains(t, long, "Auth helpers")
	assert.Contains(t, long, "Examples:")
}
