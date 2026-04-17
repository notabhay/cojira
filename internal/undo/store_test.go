package undo

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListIssuesPrunesExpiredEntries(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_UNDO_DIR", dir)
	t.Setenv("COJIRA_UNDO_TTL_HOURS", "1")

	expired := IssueEntry{
		ID:        "expired",
		GroupID:   "g1",
		Issue:     "PROJ-1",
		Operation: "update",
		Timestamp: time.Now().UTC().Add(-2 * time.Hour),
	}
	fresh := IssueEntry{
		ID:        "fresh",
		GroupID:   "g2",
		Issue:     "PROJ-2",
		Operation: "update",
		Timestamp: time.Now().UTC(),
	}
	require.NoError(t, RecordIssue(expired))
	require.NoError(t, RecordIssue(fresh))

	items, err := ListIssues(0)
	require.NoError(t, err)
	require.Len(t, items, 1)
	assert.Equal(t, "fresh", items[0].ID)

	_, err = os.Stat(filepath.Join(dir, "expired.json"))
	assert.True(t, os.IsNotExist(err))
}
