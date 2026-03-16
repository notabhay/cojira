package idempotency

import (
	"bytes"
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
	Value       []byte  `json:"value,omitempty"`
}

func loadEntry(key string) (*entry, error) {
	path := storePath(key)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	if time.Now().Unix()-int64(e.Timestamp) >= ttlSeconds {
		return nil, os.ErrNotExist
	}
	return &e, nil
}

// IsDuplicate returns true if this key was already recorded within the TTL.
func IsDuplicate(key string) bool {
	_, err := loadEntry(key)
	return err == nil
}

// Record records a completed operation for the given key.
func Record(key string, description string) error {
	return RecordValue(key, description, nil)
}

// RecordValue records a completed operation for the given key together with
// optional structured value data that can be loaded on resume.
func RecordValue(key string, description string, value any) error {
	dir := storeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	var raw []byte
	if value != nil {
		data, err := json.Marshal(value)
		if err != nil {
			return err
		}
		raw = data
	}
	e := entry{
		Key:         key,
		Description: description,
		Timestamp:   float64(time.Now().Unix()),
		Value:       raw,
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(storePath(key), data, 0o600)
}

// LoadValue loads structured value data previously recorded for key. It
// returns false when the key does not exist or has expired.
func LoadValue(key string, out any) (bool, error) {
	e, err := loadEntry(key)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if out == nil || len(e.Value) == 0 {
		return true, nil
	}
	if err := json.Unmarshal(e.Value, out); err != nil {
		return false, err
	}
	return true, nil
}

// StoredValueMatches reports whether key exists within the TTL and its stored
// structured value exactly matches the provided value after JSON marshalling.
func StoredValueMatches(key string, value any) (bool, error) {
	e, err := loadEntry(key)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	if value == nil {
		return len(e.Value) == 0, nil
	}
	data, err := json.Marshal(value)
	if err != nil {
		return false, err
	}
	return bytes.Equal(e.Value, data), nil
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
