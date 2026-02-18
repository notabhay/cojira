package idempotency

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"
)

const ttlSeconds = 3600 * 24 // 24 hours

func storeDir() string {
	if dir := os.Getenv("COJIRA_IDEMPOTENCY_DIR"); dir != "" {
		return dir
	}
	if xdg := os.Getenv("XDG_CACHE_HOME"); xdg != "" {
		return filepath.Join(xdg, "cojira", "idempotency")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cache", "cojira", "idempotency")
}

func storePath(key string) string {
	return filepath.Join(storeDir(), key+".json")
}

type entry struct {
	Key         string  `json:"key"`
	Description string  `json:"description,omitempty"`
	Timestamp   float64 `json:"timestamp"`
}

// IsDuplicate returns true if this key was already recorded within the TTL.
func IsDuplicate(key string) bool {
	path := storePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return false
	}
	return time.Now().Unix()-int64(e.Timestamp) < ttlSeconds
}

// Record records a completed operation for the given key.
func Record(key string, description string) error {
	dir := storeDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	e := entry{
		Key:         key,
		Description: description,
		Timestamp:   float64(time.Now().Unix()),
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(storePath(key), data, 0o644)
}

// CheckAndRecord returns true if this key is a duplicate (already recorded
// within TTL). If the key is new, it records it and returns false.
func CheckAndRecord(key string, description string) bool {
	if IsDuplicate(key) {
		return true
	}
	_ = Record(key, description)
	return false
}

// ClearStore removes expired entries from the store. Returns the count removed.
func ClearStore() int {
	dir := storeDir()
	entries, err := filepath.Glob(filepath.Join(dir, "*.json"))
	if err != nil {
		return 0
	}
	count := 0
	now := time.Now().Unix()
	for _, path := range entries {
		data, err := os.ReadFile(path)
		if err != nil {
			_ = os.Remove(path)
			count++
			continue
		}
		var e entry
		if err := json.Unmarshal(data, &e); err != nil {
			_ = os.Remove(path)
			count++
			continue
		}
		if now-int64(e.Timestamp) >= ttlSeconds {
			_ = os.Remove(path)
			count++
		}
	}
	return count
}
