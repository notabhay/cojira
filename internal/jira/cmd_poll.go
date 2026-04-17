package jira

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewPollCmd creates the "poll" command group.
func NewPollCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "poll",
		Short: "Poll Jira issues or JQL results for changes",
	}
	cmd.AddCommand(
		newPollIssueCmd(),
		newPollJQLCmd(),
	)
	return cmd
}

func newPollIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue <issue>",
		Short: "Poll a Jira issue for changes",
		Args:  cobra.ExactArgs(1),
		RunE:  runPollIssue,
	}
	addPollFlags(cmd)
	return cmd
}

func newPollJQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jql <jql>",
		Short: "Poll a JQL result set for changes",
		Args:  cobra.ExactArgs(1),
		RunE:  runPollJQL,
	}
	addPollFlags(cmd)
	cmd.Flags().Int("limit", 20, "Max issues to include in the JQL poll snapshot")
	cmd.Flags().Int("page-size", 100, "Page size when fetching --all results")
	cmd.Flags().Bool("all", false, "Fetch all pages for the JQL snapshot")
	return cmd
}

func addPollFlags(cmd *cobra.Command) {
	cmd.Flags().Duration("interval", 30*time.Second, "Polling interval")
	cmd.Flags().Int("cycles", 0, "Number of polling cycles before exit (0 means run forever)")
	cmd.Flags().String("state-file", "", "Path to the local poll state file")
	cmd.Flags().String("on-change", "", "Shell command to run after a detected change")
	cmd.Flags().Bool("notify", false, "Send a best-effort desktop notification on change")
	cmd.Flags().String("fields", "summary,status,assignee,priority,updated", "Fields to include in issue snapshots")
	cli.AddOutputFlags(cmd, true)
}

func runPollIssue(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	issueID := ResolveIssueIdentifier(args[0])
	fields, _ := cmd.Flags().GetString("fields")
	scope := "issue:" + issueID
	return runPollLoop(cmd, mode, scope, func() (any, error) {
		issue, err := client.GetIssue(issueID, fields, "")
		if err != nil {
			return nil, err
		}
		recordSearchRecents(client, []map[string]any{issue}, "poll")
		return summarizeIssueInfo(client, issue), nil
	})
}

func runPollJQL(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	jql := applyDefaultScope(cmd, args[0])
	fields, _ := cmd.Flags().GetString("fields")
	limit, _ := cmd.Flags().GetInt("limit")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	all, _ := cmd.Flags().GetBool("all")
	scope := "jql:" + jql
	return runPollLoop(cmd, mode, scope, func() (any, error) {
		issues, total, err := searchAllIssues(client, jql, limit, 0, pageSize, fields, "", all)
		if err != nil {
			return nil, err
		}
		recordSearchRecents(client, issues, "poll")
		rows := make([]map[string]any, 0, len(issues))
		for _, issue := range issues {
			rows = append(rows, summarizeIssueInfo(client, issue)["summary_info"].(map[string]any))
		}
		return map[string]any{"jql": jql, "total": total, "issues": rows}, nil
	})
}

func runPollLoop(cmd *cobra.Command, mode, scope string, fetch func() (any, error)) error {
	interval, _ := cmd.Flags().GetDuration("interval")
	cycles, _ := cmd.Flags().GetInt("cycles")
	stateFile, _ := cmd.Flags().GetString("state-file")
	onChange, _ := cmd.Flags().GetString("on-change")
	notify, _ := cmd.Flags().GetBool("notify")
	if strings.TrimSpace(stateFile) == "" {
		stateFile = pollStatePath(scope)
	}

	type pollSnapshot struct {
		Hash      string `json:"hash"`
		UpdatedAt string `json:"updated_at"`
	}

	runOnce := func() (bool, any, error) {
		payload, err := fetch()
		if err != nil {
			return false, nil, err
		}
		hash, err := hashValue(payload)
		if err != nil {
			return false, nil, err
		}
		prev, _ := readPollSnapshot(stateFile)
		changed := prev.Hash != "" && prev.Hash != hash
		if err := writePollSnapshot(stateFile, pollSnapshot{Hash: hash, UpdatedAt: time.Now().UTC().Format(time.RFC3339)}); err != nil {
			return false, nil, err
		}
		if changed {
			if notify {
				sendBestEffortNotification("cojira detected a Jira change", scope)
			}
			if strings.TrimSpace(onChange) != "" {
				_ = exec.Command("/bin/sh", "-lc", onChange).Run()
			}
		}
		return changed, payload, nil
	}

	iterations := 0
	for {
		changed, payload, err := runOnce()
		if err != nil {
			return err
		}
		iterations++
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "poll", map[string]any{"scope": scope}, map[string]any{
				"scope":      scope,
				"state_file": stateFile,
				"changed":    changed,
				"payload":    payload,
				"iterations": iterations,
			}, nil, nil, "", "", "", nil))
		}
		if changed {
			fmt.Printf("Change detected for %s.\n", scope)
		} else if mode != "summary" {
			fmt.Printf("No change for %s.\n", scope)
		}
		if cycles > 0 && iterations >= cycles {
			break
		}
		time.Sleep(interval)
	}
	if mode == "summary" {
		fmt.Printf("Completed %d poll cycle(s) for %s.\n", iterations, scope)
	}
	return nil
}

func hashValue(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}

func readPollSnapshot(path string) (struct {
	Hash      string `json:"hash"`
	UpdatedAt string `json:"updated_at"`
}, error) {
	var snapshot struct {
		Hash      string `json:"hash"`
		UpdatedAt string `json:"updated_at"`
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snapshot, err
	}
	err = json.Unmarshal(data, &snapshot)
	return snapshot, err
}

func writePollSnapshot(path string, snapshot any) error {
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func filepathDir(path string) string {
	idx := strings.LastIndex(path, "/")
	if idx < 0 {
		return "."
	}
	if idx == 0 {
		return "/"
	}
	return path[:idx]
}

func sendBestEffortNotification(title, body string) {
	switch runtime.GOOS {
	case "darwin":
		_ = exec.Command("osascript", "-e", fmt.Sprintf(`display notification %q with title %q`, body, title)).Run()
	default:
		if _, err := exec.LookPath("notify-send"); err == nil {
			_ = exec.Command("notify-send", title, body).Run()
		}
	}
}
