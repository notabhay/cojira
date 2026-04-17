package undo

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

type IssueEntry struct {
	ID         string         `json:"id"`
	GroupID    string         `json:"group_id"`
	Issue      string         `json:"issue"`
	Operation  string         `json:"operation"`
	Timestamp  time.Time      `json:"timestamp"`
	Fields     map[string]any `json:"fields,omitempty"`
	FromStatus string         `json:"from_status,omitempty"`
	ToStatus   string         `json:"to_status,omitempty"`
}

func storeDir() string {
	if dir := strings.TrimSpace(os.Getenv("COJIRA_UNDO_DIR")); dir != "" {
		return dir
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		return filepath.Join(xdg, "cojira", "undo")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".cache", "cojira", "undo")
}

func NewGroupID(prefix string) string {
	var buf [6]byte
	_, _ = rand.Read(buf[:])
	return prefix + "-" + time.Now().UTC().Format("20060102T150405.000000000") + "-" + hex.EncodeToString(buf[:])
}

func undoTTL() time.Duration {
	raw := strings.TrimSpace(os.Getenv("COJIRA_UNDO_TTL_HOURS"))
	if raw == "" {
		return 30 * 24 * time.Hour
	}
	hours, err := strconv.Atoi(raw)
	if err != nil || hours <= 0 {
		return 30 * 24 * time.Hour
	}
	return time.Duration(hours) * time.Hour
}

func cleanupExpiredEntries() {
	ttl := undoTTL()
	if ttl <= 0 {
		return
	}
	paths, err := filepath.Glob(filepath.Join(storeDir(), "*.json"))
	if err != nil {
		return
	}
	cutoff := time.Now().UTC().Add(-ttl)
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			_ = os.Remove(path)
			continue
		}
		var entry IssueEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			_ = os.Remove(path)
			continue
		}
		if !entry.Timestamp.IsZero() && entry.Timestamp.Before(cutoff) {
			_ = os.Remove(path)
		}
	}
}

func RecordIssue(entry IssueEntry) error {
	if entry.ID == "" {
		entry.ID = NewGroupID("entry")
	}
	if entry.GroupID == "" {
		entry.GroupID = NewGroupID(entry.Operation)
	}
	if entry.Timestamp.IsZero() {
		entry.Timestamp = time.Now().UTC()
	}
	if err := os.MkdirAll(storeDir(), 0o755); err != nil {
		return err
	}
	cleanupExpiredEntries()
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(storeDir(), entry.ID+".json"), data, 0o600)
}

func ListIssues(limit int) ([]IssueEntry, error) {
	cleanupExpiredEntries()
	paths, err := filepath.Glob(filepath.Join(storeDir(), "*.json"))
	if err != nil {
		return nil, err
	}
	entries := make([]IssueEntry, 0, len(paths))
	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var entry IssueEntry
		if err := json.Unmarshal(data, &entry); err != nil {
			continue
		}
		entries = append(entries, entry)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Timestamp.After(entries[j].Timestamp)
	})
	if limit > 0 && len(entries) > limit {
		entries = entries[:limit]
	}
	return entries, nil
}

func LatestIssue(issue string) (*IssueEntry, error) {
	entries, err := ListIssues(0)
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if strings.EqualFold(entry.Issue, issue) {
			item := entry
			return &item, nil
		}
	}
	return nil, nil
}

func LatestGroup() ([]IssueEntry, error) {
	entries, err := ListIssues(0)
	if err != nil {
		return nil, err
	}
	if len(entries) == 0 {
		return nil, nil
	}
	return Group(entries[0].GroupID)
}

func Group(groupID string) ([]IssueEntry, error) {
	entries, err := ListIssues(0)
	if err != nil {
		return nil, err
	}
	filtered := make([]IssueEntry, 0)
	for _, entry := range entries {
		if entry.GroupID == groupID {
			filtered = append(filtered, entry)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		return filtered[i].Timestamp.After(filtered[j].Timestamp)
	})
	return filtered, nil
}
