package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	defaultTTLSeconds    = 3600 * 24     // 24 hours
	resultTTLSeconds     = 3600 * 24 * 7 // 7 days
	checkpointTTLSeconds = 3600 * 24 * 7 // 7 days
	planTTLSeconds       = 3600 * 24 * 7 // 7 days
	captureTTLSeconds    = 3600 * 24 * 7 // 7 days
)

const (
	kindDefault    = "default"
	kindResult     = "result"
	kindCheckpoint = "checkpoint"
	kindPlan       = "plan"
	kindCapture    = "capture"
)

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

func storePath(key string) (string, error) {
	trimmed := strings.TrimSpace(key)
	if trimmed == "" {
		return "", fmt.Errorf("idempotency key cannot be empty")
	}
	sum := sha256.Sum256([]byte(trimmed))
	name := hex.EncodeToString(sum[:]) + ".json"
	return filepath.Join(storeDir(), name), nil
}

func ttlForKind(kind string) int {
	switch kind {
	case kindResult:
		return resultTTLSeconds
	case kindCheckpoint:
		return checkpointTTLSeconds
	case kindPlan:
		return planTTLSeconds
	case kindCapture:
		return captureTTLSeconds
	default:
		return defaultTTLSeconds
	}
}

type entry struct {
	Key         string  `json:"key"`
	Kind        string  `json:"kind,omitempty"`
	Description string  `json:"description,omitempty"`
	Timestamp   float64 `json:"timestamp"`
	TTLSeconds  int     `json:"ttl_seconds,omitempty"`
	Value       []byte  `json:"value,omitempty"`
}

func loadEntry(key string) (*entry, error) {
	path, err := storePath(key)
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var e entry
	if err := json.Unmarshal(data, &e); err != nil {
		return nil, err
	}
	ttl := e.TTLSeconds
	if ttl <= 0 {
		ttl = ttlForKind(e.Kind)
	}
	if time.Now().Unix()-int64(e.Timestamp) >= int64(ttl) {
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
	return RecordKindValue(key, kindDefault, description, nil)
}

// RecordValue records a completed operation for the given key together with
// optional structured value data that can be loaded on resume.
func RecordValue(key string, description string, value any) error {
	return RecordKindValue(key, kindDefault, description, value)
}

// RecordKind records a completed operation for the given key and kind.
func RecordKind(key string, kind string, description string) error {
	return RecordKindValue(key, kind, description, nil)
}

// RecordKindValue records a completed operation for the given key and kind together with
// optional structured value data that can be loaded on resume.
func RecordKindValue(key string, kind string, description string, value any) error {
	dir := storeDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path, err := storePath(key)
	if err != nil {
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
		Kind:        kind,
		Description: description,
		Timestamp:   float64(time.Now().Unix()),
		TTLSeconds:  ttlForKind(kind),
		Value:       raw,
	}
	data, err := json.Marshal(e)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o600)
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
		ttl := e.TTLSeconds
		if ttl <= 0 {
			ttl = ttlForKind(e.Kind)
		}
		if now-int64(e.Timestamp) >= int64(ttl) {
			_ = os.Remove(path)
			count++
		}
	}
	return count
}
