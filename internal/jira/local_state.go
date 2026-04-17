package jira

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/config"
	cerrors "github.com/notabhay/cojira/internal/errors"
)

const maxRecentIssues = 100

type recentIssue struct {
	Key        string    `json:"key"`
	Summary    string    `json:"summary,omitempty"`
	Status     string    `json:"status,omitempty"`
	Assignee   string    `json:"assignee,omitempty"`
	Source     string    `json:"source,omitempty"`
	URL        string    `json:"url,omitempty"`
	SeenAt     time.Time `json:"seen_at"`
	OfflineRef string    `json:"offline_ref,omitempty"`
}

func jiraStateDir() string {
	if dir := strings.TrimSpace(os.Getenv("COJIRA_JIRA_STATE_DIR")); dir != "" {
		return dir
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); xdg != "" {
		return filepath.Join(xdg, "cojira", "jira")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".cache", "cojira", "jira")
	}
	return filepath.Join(home, ".cache", "cojira", "jira")
}

func ensureJiraStateDir() (string, error) {
	dir := jiraStateDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func jiraStatePath(name string) string {
	return filepath.Join(jiraStateDir(), name)
}

func recentsPath() string {
	return jiraStatePath("recent_issues.json")
}

func pollStatePath(scope string) string {
	sum := sha256.Sum256([]byte(scope))
	return jiraStatePath("poll_" + hex.EncodeToString(sum[:8]) + ".json")
}

func readRecentIssues() ([]recentIssue, error) {
	data, err := os.ReadFile(recentsPath())
	if err != nil {
		if os.IsNotExist(err) {
			return []recentIssue{}, nil
		}
		return nil, err
	}
	var items []recentIssue
	if err := json.Unmarshal(data, &items); err != nil {
		return []recentIssue{}, nil
	}
	return items, nil
}

func writeRecentIssues(items []recentIssue) error {
	if _, err := ensureJiraStateDir(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(items, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(recentsPath(), data, 0o644)
}

func recentIssueFromIssue(client *Client, issue map[string]any, source string) recentIssue {
	fields, _ := issue["fields"].(map[string]any)
	key := normalizeMaybeString(issue["key"])
	item := recentIssue{
		Key:      key,
		Summary:  normalizeMaybeString(fields["summary"]),
		Status:   safeString(fields, "status", "name"),
		Assignee: safeString(fields, "assignee", "displayName"),
		Source:   source,
		SeenAt:   time.Now().UTC(),
	}
	if key != "" && client != nil {
		item.URL = fmt.Sprintf("%s/browse/%s", client.BaseURL(), key)
	}
	return item
}

func recordRecentIssues(items ...recentIssue) error {
	if len(items) == 0 {
		return nil
	}
	existing, err := readRecentIssues()
	if err != nil {
		return err
	}

	byKey := make(map[string]recentIssue, len(existing)+len(items))
	order := make([]string, 0, len(existing)+len(items))
	for _, item := range existing {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		byKey[key] = item
		order = append(order, key)
	}
	for _, item := range items {
		key := strings.TrimSpace(item.Key)
		if key == "" {
			continue
		}
		if item.SeenAt.IsZero() {
			item.SeenAt = time.Now().UTC()
		}
		byKey[key] = item
		order = append(order, key)
	}

	seen := make(map[string]bool, len(order))
	deduped := make([]recentIssue, 0, len(order))
	for i := len(order) - 1; i >= 0; i-- {
		key := order[i]
		if seen[key] {
			continue
		}
		seen[key] = true
		deduped = append(deduped, byKey[key])
	}
	sort.SliceStable(deduped, func(i, j int) bool {
		return deduped[i].SeenAt.After(deduped[j].SeenAt)
	})
	if len(deduped) > maxRecentIssues {
		deduped = deduped[:maxRecentIssues]
	}
	return writeRecentIssues(deduped)
}

func listRecentIssues(limit int) ([]recentIssue, error) {
	items, err := readRecentIssues()
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool {
		return items[i].SeenAt.After(items[j].SeenAt)
	})
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}
	return items, nil
}

func offlineBaseDir() string {
	cfg, err := config.LoadProjectConfig(nil)
	if err == nil && cfg != nil {
		if base, ok := cfg.GetValue([]string{"jira", "offline", "base_dir"}, "").(string); ok && strings.TrimSpace(base) != "" {
			return strings.TrimSpace(base)
		}
	}
	return "0-JIRA"
}

func offlineIssueFiles(baseDir string) ([]string, error) {
	root := strings.TrimSpace(baseDir)
	if root == "" {
		root = offlineBaseDir()
	}
	var paths []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		if strings.EqualFold(d.Name(), "issue.json") {
			paths = append(paths, path)
		}
		return nil
	})
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func loadOfflineIssue(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var issue map[string]any
	if err := json.Unmarshal(data, &issue); err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  fmt.Sprintf("Invalid offline issue JSON in %s: %v", path, err),
			ExitCode: 1,
		}
	}
	return issue, nil
}

func savedQueriesFromConfig(cfg *config.ProjectConfig) map[string]string {
	if cfg == nil {
		return map[string]string{}
	}
	return cfg.GetStringMap([]string{"jira", "saved_queries"})
}

func jiraTemplateFromConfig(cfg *config.ProjectConfig, name string) (map[string]any, bool) {
	if cfg == nil {
		return nil, false
	}
	templates := cfg.GetObject([]string{"jira", "templates"})
	value, ok := templates[name]
	if !ok {
		return nil, false
	}
	tpl, ok := value.(map[string]any)
	if !ok {
		return nil, false
	}
	return tpl, true
}

func gitBranchTemplateFromConfig() string {
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil || cfg == nil {
		return "feature/{issue}-{slug}"
	}
	if value, ok := cfg.GetValue([]string{"jira", "git", "branch_template"}, "").(string); ok && strings.TrimSpace(value) != "" {
		return strings.TrimSpace(value)
	}
	return "feature/{issue}-{slug}"
}
