package board

import (
	"testing"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func intPtr(n int) *int { return &n }

func TestBuildApplyPlanCreateUpdateAndExtras(t *testing.T) {
	current := &SwimlanesConfig{
		Strategy: "custom",
		Swimlanes: []Swimlane{
			{ID: intPtr(1), Name: "A", Query: "q1", Description: "", IsDefault: false},
			{ID: intPtr(2), Name: "B", Query: "", Description: "", IsDefault: true},
		},
	}
	desired := &SwimlanesConfig{
		Strategy: "custom",
		Swimlanes: []Swimlane{
			{ID: intPtr(2), Name: "B", Query: "", Description: "", IsDefault: false},
			{ID: nil, Name: "C", Query: "q3", Description: "", IsDefault: true},
		},
	}

	plan, err := BuildApplyPlan(current, desired, false)
	require.NoError(t, err)

	assert.Equal(t, 1, plan.Summary.Create)
	assert.Equal(t, 1, plan.Summary.Update)
	assert.Equal(t, 0, plan.Summary.Delete)
	assert.False(t, plan.Strategy.Changed)
	assert.Equal(t, []int{1}, plan.Reorder.Extras)

	// Check ops contain a create for "C".
	hasCreate := false
	for _, op := range plan.Ops {
		if op["action"] == "create" {
			if lane, ok := op["lane"].(map[string]any); ok {
				if lane["name"] == "C" {
					hasCreate = true
				}
			}
		}
	}
	assert.True(t, hasCreate)

	// Check ops contain an update for id=2 with isDefault field.
	hasUpdate := false
	for _, op := range plan.Ops {
		if op["action"] == "update" {
			id, _ := op["id"].(int)
			if id == 2 {
				if fields, ok := op["fields"].(map[string]any); ok {
					if _, ok := fields["isDefault"]; ok {
						hasUpdate = true
					}
				}
			}
		}
	}
	assert.True(t, hasUpdate)
}

func TestBuildApplyPlanErrorsWhenIDMissingOnBoard(t *testing.T) {
	current := &SwimlanesConfig{
		Strategy:  "custom",
		Swimlanes: []Swimlane{{ID: intPtr(1), Name: "A", Query: "", Description: "", IsDefault: true}},
	}
	desired := &SwimlanesConfig{
		Strategy:  "custom",
		Swimlanes: []Swimlane{{ID: intPtr(999), Name: "X", Query: "", Description: "", IsDefault: true}},
	}
	_, err := BuildApplyPlan(current, desired, false)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.ErrorAs(t, err, &ce)
	assert.Equal(t, 2, ce.ExitCode)
}

func TestBuildApplyPlanRequiresExactlyOneDefault(t *testing.T) {
	current := &SwimlanesConfig{
		Strategy:  "custom",
		Swimlanes: []Swimlane{{ID: intPtr(1), Name: "A", Query: "", Description: "", IsDefault: true}},
	}
	desired := &SwimlanesConfig{
		Strategy: "custom",
		Swimlanes: []Swimlane{
			{ID: intPtr(1), Name: "A", Query: "", Description: "", IsDefault: false},
		},
	}
	_, err := BuildApplyPlan(current, desired, false)
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.ErrorAs(t, err, &ce)
}

func TestBuildApplyPlanNonCustomStrategyEmptyLanes(t *testing.T) {
	current := &SwimlanesConfig{Strategy: "assignee", Swimlanes: nil}
	desired := &SwimlanesConfig{Strategy: "assignee", Swimlanes: nil}

	plan, err := BuildApplyPlan(current, desired, false)
	require.NoError(t, err)
	assert.Equal(t, 0, plan.Summary.Create)
	assert.Equal(t, 0, plan.Summary.Update)
	assert.Equal(t, 0, plan.Summary.Delete)
}

func TestComputeMoveOpsNoChange(t *testing.T) {
	ops := ComputeMoveOps([]int{1, 2, 3}, []int{1, 2, 3})
	assert.Nil(t, ops)
}

func TestComputeMoveOpsEmpty(t *testing.T) {
	ops := ComputeMoveOps([]int{1, 2}, nil)
	assert.Nil(t, ops)
}

func TestComputeMoveOpsReorder(t *testing.T) {
	ops := ComputeMoveOps([]int{1, 2, 3}, []int{3, 1, 2})
	require.Len(t, ops, 3)
	assert.Equal(t, "First", ops[0].Position)
	assert.Equal(t, 3, ops[0].ID)
	assert.Equal(t, 1, ops[1].ID)
	assert.NotNil(t, ops[1].AfterID)
	assert.Equal(t, 3, *ops[1].AfterID)
}

func TestExtractSwimlanesConfig(t *testing.T) {
	editModel := map[string]any{
		"swimlanesConfig": map[string]any{
			"swimlaneStrategy": "custom",
			"swimlanes": []any{
				map[string]any{"id": 1.0, "name": "P0", "query": "priority = Highest", "description": "", "isDefault": false},
				map[string]any{"id": 2.0, "name": "Default", "query": "", "description": "", "isDefault": true},
			},
			"canEdit": true,
		},
	}

	cfg, err := ExtractSwimlanesConfig(editModel)
	require.NoError(t, err)
	assert.Equal(t, "custom", cfg.Strategy)
	assert.True(t, cfg.CanEdit)
	assert.Len(t, cfg.Swimlanes, 2)
	assert.Equal(t, "P0", cfg.Swimlanes[0].Name)
	assert.NotNil(t, cfg.Swimlanes[0].ID)
	assert.Equal(t, 1, *cfg.Swimlanes[0].ID)
}

func TestExtractSwimlanesConfigMissing(t *testing.T) {
	_, err := ExtractSwimlanesConfig(map[string]any{})
	require.Error(t, err)
}

func TestValidateDesiredSwimlanesHappy(t *testing.T) {
	lanes := []Swimlane{
		{ID: intPtr(1), Name: "A", IsDefault: false},
		{ID: intPtr(2), Name: "B", IsDefault: true},
	}
	assert.NoError(t, ValidateDesiredSwimlanes(lanes))
}

func TestValidateDesiredSwimlanesNoneDefault(t *testing.T) {
	lanes := []Swimlane{
		{Name: "A", IsDefault: false},
		{Name: "B", IsDefault: false},
	}
	assert.Error(t, ValidateDesiredSwimlanes(lanes))
}

func TestValidateDesiredSwimlanesDuplicateNames(t *testing.T) {
	lanes := []Swimlane{
		{Name: "A", IsDefault: true},
		{Name: "A", IsDefault: false},
	}
	assert.Error(t, ValidateDesiredSwimlanes(lanes))
}

func TestValidateDesiredSwimlanesDuplicateIDs(t *testing.T) {
	lanes := []Swimlane{
		{ID: intPtr(1), Name: "A", IsDefault: true},
		{ID: intPtr(1), Name: "B", IsDefault: false},
	}
	assert.Error(t, ValidateDesiredSwimlanes(lanes))
}

func TestSwimlaneToJSON(t *testing.T) {
	lane := Swimlane{ID: intPtr(5), Name: "Test", Query: "q", Description: "d", IsDefault: true}
	j := lane.ToJSON()
	assert.Equal(t, "Test", j["name"])
	assert.Equal(t, 5, j["id"])
	assert.Equal(t, true, j["isDefault"])
}

func TestSwimlaneToJSONNoID(t *testing.T) {
	lane := Swimlane{Name: "Test", Query: "q"}
	j := lane.ToJSON()
	_, hasID := j["id"]
	assert.False(t, hasID)
}
