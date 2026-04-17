package meta

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDryRunRecordAndApply(t *testing.T) {
	planDir := t.TempDir()
	outDir := t.TempDir()
	t.Setenv("COJIRA_PLAN_DIR", planDir)
	t.Setenv("TEST_RECORD_OUT", filepath.Join(outDir, "apply.txt"))

	scriptPath := filepath.Join(t.TempDir(), "cojira-test.sh")
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"--profile\" ]; then shift 2; fi\n" +
		"for arg in \"$@\"; do\n" +
		"  if [ \"$arg\" = \"--plan\" ]; then\n" +
		"    echo '{\"preview\":true}'\n" +
		"    exit 0\n" +
		"  fi\n" +
		"done\n" +
		"printf '%s' \"$*\" > \"$TEST_RECORD_OUT\"\n" +
		"echo '{\"applied\":true}'\n"
	require.NoError(t, os.WriteFile(scriptPath, []byte(script), 0o755))
	t.Setenv("COJIRA_EXECUTABLE_OVERRIDE", scriptPath)

	root := testRecordRoot()
	root.SetArgs([]string{"dry-run", "record", "jira", "transition", "PROJ-1", "--to", "Done"})
	require.NoError(t, root.Execute())

	entries, err := filepath.Glob(filepath.Join(planDir, "*.json"))
	require.NoError(t, err)
	require.Len(t, entries, 1)

	planData, err := os.ReadFile(entries[0])
	require.NoError(t, err)
	assert.Contains(t, string(planData), `"planned_args"`)
	assert.Contains(t, string(planData), `--plan`)

	planID := strings.TrimSuffix(filepath.Base(entries[0]), filepath.Ext(entries[0]))
	root = testRecordRoot()
	root.SetArgs([]string{"apply", planID, "--yes"})
	require.NoError(t, root.Execute())

	data, err := os.ReadFile(filepath.Join(outDir, "apply.txt"))
	require.NoError(t, err)
	assert.Equal(t, "jira transition PROJ-1 --to Done", string(data))
}

func TestApplyRejectsContextMismatch(t *testing.T) {
	t.Setenv("COJIRA_PLAN_DIR", t.TempDir())
	plan := recordedPlan{
		ID:                 "abc123",
		RecordedAt:         "2026-01-01T00:00:00Z",
		Args:               []string{"jira", "get", "PROJ-1"},
		ApplyArgs:          []string{"jira", "get", "PROJ-1"},
		ContextFingerprint: "different",
	}
	_, err := saveRecordedPlan(plan)
	require.NoError(t, err)

	root := testRecordRoot()
	root.SetArgs([]string{"apply", "abc123", "--yes"})
	err = root.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "context does not match")
}

func testRecordRoot() *cobra.Command {
	root := &cobra.Command{Use: "cojira", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().String("profile", "", "")
	jiraCmd := &cobra.Command{Use: "jira"}
	transitionCmd := &cobra.Command{Use: "transition"}
	jiraCmd.AddCommand(transitionCmd)
	root.AddCommand(jiraCmd)
	root.AddCommand(NewDryRunCmd(root))
	root.AddCommand(NewApplyCmd())
	return root
}
