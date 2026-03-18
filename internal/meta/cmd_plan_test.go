package meta

import (
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
)

func testPlanRoot() *cobra.Command {
	root := &cobra.Command{Use: "cojira"}
	jiraCmd := &cobra.Command{Use: "jira"}
	updateCmd := &cobra.Command{Use: "update"}
	createCmd := &cobra.Command{Use: "create"}
	swimlanesCmd := &cobra.Command{Use: "board-swimlanes"}
	applyCmd := &cobra.Command{Use: "apply"}
	swimlanesCmd.AddCommand(applyCmd)
	jiraCmd.AddCommand(updateCmd, createCmd, swimlanesCmd)
	root.AddCommand(jiraCmd)
	return root
}

func TestInjectPlanAddsFlagAfterLeafCommand(t *testing.T) {
	args := []string{"jira", "update", "PROJ-1", "--summary", "Fix"}
	result := injectPlan(testPlanRoot(), args)
	assert.Equal(t, []string{"jira", "update", "--plan", "PROJ-1", "--summary", "Fix"}, result)
}

func TestInjectPlanAddsFlagAfterNestedLeafCommand(t *testing.T) {
	args := []string{"jira", "--experimental", "board-swimlanes", "apply", "123", "--file", "swimlanes.json"}
	result := injectPlan(testPlanRoot(), args)
	assert.Equal(t, []string{"jira", "--experimental", "board-swimlanes", "apply", "--plan", "123", "--file", "swimlanes.json"}, result)
}

func TestInjectPlanRespectsExistingDryRun(t *testing.T) {
	args := []string{"jira", "update", "PROJ-1", "--dry-run"}
	result := injectPlan(testPlanRoot(), args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingPlanFlag(t *testing.T) {
	args := []string{"jira", "update", "PROJ-1", "--plan"}
	result := injectPlan(testPlanRoot(), args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingPreviewFlag(t *testing.T) {
	args := []string{"jira", "update", "PROJ-1", "--preview"}
	result := injectPlan(testPlanRoot(), args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingDiffFlag(t *testing.T) {
	args := []string{"jira", "update", "PROJ-1", "--diff"}
	result := injectPlan(testPlanRoot(), args)
	assert.Equal(t, args, result)
}

func TestInjectPlanSkipsDoubleDash(t *testing.T) {
	args := []string{"--", "jira", "create", "payload.json"}
	result := injectPlan(testPlanRoot(), args)
	assert.Equal(t, []string{"jira", "create", "--plan", "payload.json"}, result)
}

func TestInjectPlanEmptyArgs(t *testing.T) {
	result := injectPlan(testPlanRoot(), nil)
	assert.Nil(t, result)
}
