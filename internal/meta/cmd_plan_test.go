package meta

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestInjectPlanAddsFlagAfterFirstArg(t *testing.T) {
	args := []string{"update", "PROJ-1", "--summary", "Fix"}
	result := injectPlan(args)
	assert.Equal(t, []string{"update", "--plan", "PROJ-1", "--summary", "Fix"}, result)
}

func TestInjectPlanRespectsExistingDryRun(t *testing.T) {
	args := []string{"update", "PROJ-1", "--dry-run"}
	result := injectPlan(args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingPlanFlag(t *testing.T) {
	args := []string{"update", "PROJ-1", "--plan"}
	result := injectPlan(args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingPreviewFlag(t *testing.T) {
	args := []string{"update", "PROJ-1", "--preview"}
	result := injectPlan(args)
	assert.Equal(t, args, result)
}

func TestInjectPlanRespectsExistingDiffFlag(t *testing.T) {
	args := []string{"update", "PROJ-1", "--diff"}
	result := injectPlan(args)
	assert.Equal(t, args, result)
}

func TestInjectPlanSkipsDoubleDash(t *testing.T) {
	args := []string{"--", "update", "PROJ-1"}
	result := injectPlan(args)
	assert.Equal(t, []string{"update", "--plan", "PROJ-1"}, result)
}

func TestInjectPlanEmptyArgs(t *testing.T) {
	result := injectPlan(nil)
	assert.Nil(t, result)
}
