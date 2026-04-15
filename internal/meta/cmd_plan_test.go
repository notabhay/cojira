package meta

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func TestInjectPlanAddsFlagAfterFirstArg(t *testing.T) {
	root := testPlanRoot()
	args := []string{"update", "PROJ-1", "--summary", "Fix"}
	result := injectPlan(root, args)
	assert.Equal(t, []string{"update", "--plan", "PROJ-1", "--summary", "Fix"}, result)
}

func TestInjectPlanRespectsExistingDryRun(t *testing.T) {
	root := testPlanRoot()
	args := []string{"update", "PROJ-1", "--dry-run"}
	result := injectPlan(root, args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingPlanFlag(t *testing.T) {
	root := testPlanRoot()
	args := []string{"update", "PROJ-1", "--plan"}
	result := injectPlan(root, args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingPreviewFlag(t *testing.T) {
	root := testPlanRoot()
	args := []string{"update", "PROJ-1", "--preview"}
	result := injectPlan(root, args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingDiffFlag(t *testing.T) {
	root := testPlanRoot()
	args := []string{"update", "PROJ-1", "--diff"}
	result := injectPlan(root, args)
	assert.Equal(t, args, result)
}

func TestInjectPlanSkipsDoubleDash(t *testing.T) {
	root := testPlanRoot()
	args := []string{"--", "update", "PROJ-1"}
	result := injectPlan(root, args)
	assert.Equal(t, []string{"update", "--plan", "PROJ-1"}, result)
}

func TestInjectPlanEmptyArgs(t *testing.T) {
	result := injectPlan(testPlanRoot(), nil)
	assert.Nil(t, result)
}

func TestInjectPlanTargetsLeafCommand(t *testing.T) {
	root := testPlanRoot()
	args := []string{"jira", "transition", "PROJ-1", "--to", "Done"}
	result := injectPlan(root, args)
	assert.Equal(t, []string{"jira", "transition", "--plan", "PROJ-1", "--to", "Done"}, result)
}

func testPlanRoot() *cobra.Command {
	root := &cobra.Command{Use: "cojira"}
	jiraCmd := &cobra.Command{Use: "jira"}
	updateCmd := &cobra.Command{Use: "update"}
	transitionCmd := &cobra.Command{Use: "transition"}
	jiraCmd.AddCommand(updateCmd, transitionCmd)
	root.AddCommand(jiraCmd, &cobra.Command{Use: "update"})
	return root
}
