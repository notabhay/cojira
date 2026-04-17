package events

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppendAndReadAll(t *testing.T) {
	t.Setenv("COJIRA_EVENT_DIR", t.TempDir())

	path, err := Append("stream-1", map[string]any{"type": "progress", "index": 1})
	require.NoError(t, err)
	assert.Equal(t, filepath.Join(DefaultDir(), "stream-1.ndjson"), path)

	_, err = Append("stream-1", map[string]any{"type": "error", "message": "boom"})
	require.NoError(t, err)

	lines, err := ReadAll("stream-1")
	require.NoError(t, err)
	assert.Len(t, lines, 2)
	assert.Contains(t, lines[0], `"progress"`)
	assert.Contains(t, lines[1], `"boom"`)
}

func TestLatestStreamID(t *testing.T) {
	t.Setenv("COJIRA_EVENT_DIR", t.TempDir())

	_, err := Append("older", map[string]any{"type": "progress"})
	require.NoError(t, err)
	time.Sleep(10 * time.Millisecond)
	_, err = Append("newer", map[string]any{"type": "progress"})
	require.NoError(t, err)

	id, err := LatestStreamID()
	require.NoError(t, err)
	assert.Equal(t, "newer", id)
}

func TestPruneRemovesOldFiles(t *testing.T) {
	t.Setenv("COJIRA_EVENT_DIR", t.TempDir())
	oldPath := filepath.Join(DefaultDir(), "old.ndjson")
	require.NoError(t, os.MkdirAll(DefaultDir(), 0o755))
	require.NoError(t, os.WriteFile(oldPath, []byte("{}\n"), 0o644))
	oldTime := time.Now().Add(-48 * time.Hour)
	require.NoError(t, os.Chtimes(oldPath, oldTime, oldTime))

	_, err := Append("fresh", map[string]any{"type": "progress"})
	require.NoError(t, err)

	_, err = os.Stat(oldPath)
	assert.True(t, os.IsNotExist(err))
}
