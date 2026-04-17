package confluence

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestApplyUserRestrictionChanges(t *testing.T) {
	current := []map[string]any{
		{"username": "alice", "displayName": "Alice"},
	}

	next, summary := applyUserRestrictionChanges(current, []string{"bob"}, []string{"alice"})
	assert.Len(t, next, 1)
	assert.Equal(t, "bob", next[0]["username"])
	assert.Equal(t, []string{"bob"}, summary["added"])
	assert.Equal(t, []string{"Alice"}, summary["removed"])
}

func TestApplyGroupRestrictionChanges(t *testing.T) {
	current := []map[string]any{
		{"name": "team-alpha"},
	}

	next, summary := applyGroupRestrictionChanges(current, []string{"team-beta"}, []string{"team-alpha"})
	assert.Len(t, next, 1)
	assert.Equal(t, "team-beta", next[0]["name"])
	assert.Equal(t, []string{"team-beta"}, summary["added"])
	assert.Equal(t, []string{"team-alpha"}, summary["removed"])
}
