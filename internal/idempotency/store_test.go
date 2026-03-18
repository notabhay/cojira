package idempotency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewKeyReturnsFalse(t *testing.T) {
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", t.TempDir())
	assert.False(t, CheckAndRecord("test-key-1", "test"))
}

func TestDuplicateKeyReturnsTrue(t *testing.T) {
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", t.TempDir())
	assert.False(t, CheckAndRecord("test-key-2", "first"))
	assert.True(t, CheckAndRecord("test-key-2", "second"))
}

func TestDifferentKeysAreIndependent(t *testing.T) {
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", t.TempDir())
	assert.False(t, CheckAndRecord("key-a", ""))
	assert.False(t, CheckAndRecord("key-b", ""))
	assert.True(t, CheckAndRecord("key-a", ""))
}

func TestExpiredKeyTreatedAsNew(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", dir)

	// Write a backdated entry
	storeFile, err := storePath("expired-key")
	require.NoError(t, err)
	e := entry{Key: "expired-key", Kind: kindDefault, TTLSeconds: ttlForKind(kindDefault), Timestamp: float64(time.Now().Unix() - 100_000)}
	data, err := json.Marshal(e)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(storeFile, data, 0o644))

	assert.False(t, CheckAndRecord("expired-key", ""))
}

func TestClearStore(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", dir)

	// Write a fresh entry
	fresh, err := storePath("fresh")
	require.NoError(t, err)
	freshData, _ := json.Marshal(entry{Key: "fresh", Kind: kindDefault, TTLSeconds: ttlForKind(kindDefault), Timestamp: float64(time.Now().Unix())})
	require.NoError(t, os.WriteFile(fresh, freshData, 0o644))

	// Write an expired entry
	expired, err := storePath("expired")
	require.NoError(t, err)
	expiredData, _ := json.Marshal(entry{Key: "expired", Kind: kindDefault, TTLSeconds: ttlForKind(kindDefault), Timestamp: float64(time.Now().Unix() - 100_000)})
	require.NoError(t, os.WriteFile(expired, expiredData, 0o644))

	removed := ClearStore()
	assert.Equal(t, 1, removed)
	assert.FileExists(t, fresh)
	assert.NoFileExists(t, expired)
}

func TestRecordAndIsDuplicate(t *testing.T) {
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", t.TempDir())

	assert.False(t, IsDuplicate("abc"))
	require.NoError(t, Record("abc", "test"))
	assert.True(t, IsDuplicate("abc"))
}

func TestClearStoreEmptyDir(t *testing.T) {
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", t.TempDir())
	assert.Equal(t, 0, ClearStore())
}

func TestClearStoreCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", dir)

	corrupt, err := storePath("bad")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(corrupt, []byte("not json"), 0o644))

	removed := ClearStore()
	assert.Equal(t, 1, removed)
	assert.NoFileExists(t, corrupt)
}

func TestIsDuplicateCorruptFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", dir)

	corrupt, err := storePath("bad")
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(corrupt, []byte("not json"), 0o644))

	assert.False(t, IsDuplicate("bad"))
}

func TestXDGCacheHome(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", "")
	t.Setenv("XDG_CACHE_HOME", dir)

	expected := filepath.Join(dir, "cojira", "idempotency")
	assert.Equal(t, expected, storeDir())
}

func TestStorePathHashesKeyInsideStoreDir(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("COJIRA_IDEMPOTENCY_DIR", dir)

	path, err := storePath("../unsafe/key")
	require.NoError(t, err)
	assert.True(t, strings.HasPrefix(path, dir+string(filepath.Separator)))
	assert.NotContains(t, path, "..")
	assert.NotContains(t, path, "unsafe")
}
