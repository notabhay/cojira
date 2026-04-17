package meta

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type recordedPlan struct {
	ID                 string              `json:"id"`
	RecordedAt         string              `json:"recorded_at"`
	Args               []string            `json:"args"`
	PlannedArgs        []string            `json:"planned_args"`
	ApplyArgs          []string            `json:"apply_args"`
	CommandPath        []string            `json:"command_path,omitempty"`
	Profile            string              `json:"profile,omitempty"`
	ContextFingerprint string              `json:"context_fingerprint,omitempty"`
	Executable         string              `json:"executable,omitempty"`
	Preview            recordedPlanPreview `json:"preview"`
}

type recordedPlanPreview struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
}

func defaultPlanDir() string {
	if dir := strings.TrimSpace(os.Getenv("COJIRA_PLAN_DIR")); dir != "" {
		return dir
	}
	if xdg := strings.TrimSpace(os.Getenv("XDG_CONFIG_HOME")); xdg != "" {
		return filepath.Join(xdg, "cojira", "plans")
	}
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return filepath.Join(".", ".config", "cojira", "plans")
	}
	return filepath.Join(home, ".config", "cojira", "plans")
}

func planPath(id string) string {
	return filepath.Join(defaultPlanDir(), strings.TrimSpace(id)+".json")
}

func saveRecordedPlan(plan recordedPlan) (string, error) {
	if err := os.MkdirAll(defaultPlanDir(), 0o755); err != nil {
		return "", err
	}
	path := planPath(plan.ID)
	data, err := json.MarshalIndent(plan, "", "  ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func loadRecordedPlan(id string) (*recordedPlan, string, error) {
	path := planPath(id)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, path, err
	}
	var plan recordedPlan
	if err := json.Unmarshal(data, &plan); err != nil {
		return nil, path, err
	}
	return &plan, path, nil
}

func currentContextFingerprint(profile string) string {
	raw := map[string]any{
		"profile":             strings.TrimSpace(profile),
		"jira_base_url":       strings.TrimSpace(os.Getenv("JIRA_BASE_URL")),
		"confluence_base_url": strings.TrimSpace(os.Getenv("CONFLUENCE_BASE_URL")),
		"jira_cloud_id":       strings.TrimSpace(os.Getenv("JIRA_CLOUD_ID")),
		"confluence_cloud_id": strings.TrimSpace(os.Getenv("CONFLUENCE_CLOUD_ID")),
	}
	data, _ := json.Marshal(raw)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func newRecordedPlanID(args []string) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s::%v", time.Now().UTC().Format(time.RFC3339Nano), args)))
	return hex.EncodeToString(sum[:])[:16]
}
