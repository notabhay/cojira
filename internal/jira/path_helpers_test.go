package jira

import (
	"os"
	"path/filepath"
	"sort"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSafeJoinUnderRejectsTraversal(t *testing.T) {
	_, err := safeJoinUnder("/tmp/base", "../escape")
	require.Error(t, err)
}

func TestFindMatchingDirsSupportsRecursiveDoublestarPattern(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(root, "one", "RAPTOR-1-thing"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "two", "nested", "CAIS-2-task"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(root, "other"), 0o755))

	dirs, err := findMatchingDirs(root, "**/*-*-*")
	require.NoError(t, err)
	for i := range dirs {
		dirs[i] = filepath.Base(dirs[i])
	}
	sort.Strings(dirs)
	assert.Equal(t, []string{"CAIS-2-task", "RAPTOR-1-thing"}, dirs)
}
