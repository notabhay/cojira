package jira

import (
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewWatchCmd creates the "watch" command group.
func NewWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watch",
		Short: "Watch Jira issues or JQL results for changes (polling-first)",
	}
	cmd.AddCommand(
		newWatchIssueCmd(),
		newWatchJQLCmd(),
	)
	return cmd
}

func newWatchIssueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "issue <issue>",
		Short: "Watch a Jira issue for changes",
		Args:  cobra.ExactArgs(1),
		RunE:  runWatchIssue,
	}
	addWatchFlags(cmd)
	return cmd
}

func newWatchJQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jql <jql>",
		Short: "Watch a JQL result set for changes",
		Args:  cobra.ExactArgs(1),
		RunE:  runWatchJQL,
	}
	addWatchFlags(cmd)
	cmd.Flags().Int("limit", 20, "Max issues to include in the JQL watch snapshot")
	cmd.Flags().Int("page-size", 100, "Page size when fetching --all results")
	cmd.Flags().Bool("all", false, "Fetch all pages for the JQL snapshot")
	return cmd
}

func addWatchFlags(cmd *cobra.Command) {
	cmd.Flags().Duration("interval", 30*time.Second, "Polling interval")
	cmd.Flags().Int("cycles", 0, "Number of watch cycles before exit (0 means run forever)")
	cmd.Flags().String("state-file", "", "Path to the local watch state file")
	cmd.Flags().String("on-change", "", "Shell command to run after a detected change")
	cmd.Flags().Bool("notify", false, "Send a best-effort desktop notification on change")
	cmd.Flags().String("fields", "summary,status,assignee,priority,updated", "Fields to include in issue snapshots")
	cmd.Flags().String("transport", "auto", "Watch transport: auto, polling, webhook (webhook currently falls back to polling)")
	cli.AddOutputFlags(cmd, true)
}

func runWatchIssue(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	transport, err := normalizeWatchTransport(cmd)
	if err != nil {
		return err
	}
	issueID := ResolveIssueIdentifier(args[0])
	fields, _ := cmd.Flags().GetString("fields")
	scope := "issue:" + issueID
	return runWatchLoop(cmd, mode, transport, scope, func() (any, error) {
		issue, err := client.GetIssue(issueID, fields, "")
		if err != nil {
			return nil, err
		}
		recordSearchRecents(client, []map[string]any{issue}, "watch")
		return summarizeIssueInfo(client, issue), nil
	})
}

func runWatchJQL(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	transport, err := normalizeWatchTransport(cmd)
	if err != nil {
		return err
	}
	jql := applyDefaultScope(cmd, args[0])
	fields, _ := cmd.Flags().GetString("fields")
	limit, _ := cmd.Flags().GetInt("limit")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	all, _ := cmd.Flags().GetBool("all")
	scope := "jql:" + jql
	return runWatchLoop(cmd, mode, transport, scope, func() (any, error) {
		issues, total, err := searchAllIssues(client, jql, limit, 0, pageSize, fields, "", all)
		if err != nil {
			return nil, err
		}
		recordSearchRecents(client, issues, "watch")
		rows := make([]map[string]any, 0, len(issues))
		for _, issue := range issues {
			rows = append(rows, summarizeIssueInfo(client, issue)["summary_info"].(map[string]any))
		}
		return map[string]any{"jql": jql, "total": total, "issues": rows}, nil
	})
}

func normalizeWatchTransport(cmd *cobra.Command) (string, error) {
	transport, _ := cmd.Flags().GetString("transport")
	transport = strings.ToLower(strings.TrimSpace(transport))
	switch transport {
	case "", "auto", "polling":
		return "polling", nil
	case "webhook":
		output.EmitEvent("warning", map[string]any{
			"message": "webhook transport is not yet implemented; falling back to polling",
		})
		return "polling", nil
	default:
		return "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported transport %q. Use auto, polling, or webhook.", transport),
			ExitCode: 2,
		}
	}
}

func runWatchLoop(cmd *cobra.Command, mode, transport, scope string, fetch func() (any, error)) error {
	interval, _ := cmd.Flags().GetDuration("interval")
	cycles, _ := cmd.Flags().GetInt("cycles")
	stateFile, _ := cmd.Flags().GetString("state-file")
	onChange, _ := cmd.Flags().GetString("on-change")
	notify, _ := cmd.Flags().GetBool("notify")
	if strings.TrimSpace(stateFile) == "" {
		stateFile = pollStatePath("watch:" + scope)
	}

	type pollSnapshot struct {
		Hash      string `json:"hash"`
		UpdatedAt string `json:"updated_at"`
	}

	runOnce := func(iteration int) (bool, any, error) {
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
		eventPayload := map[string]any{
			"scope":      scope,
			"transport":  transport,
			"changed":    changed,
			"iteration":  iteration,
			"state_file": stateFile,
		}
		if changed {
			output.EmitEvent("watch.change", eventPayload)
			if notify {
				sendBestEffortNotification("cojira detected a Jira change", scope)
			}
			if strings.TrimSpace(onChange) != "" {
				_ = runJiraShellHook(cmd, onChange)
			}
		} else {
			output.EmitEvent("watch.tick", eventPayload)
		}
		return changed, payload, nil
	}

	iterations := 0
	for {
		iterations++
		changed, payload, err := runOnce(iterations)
		if err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "watch", map[string]any{"scope": scope}, map[string]any{
				"scope":      scope,
				"state_file": stateFile,
				"changed":    changed,
				"payload":    payload,
				"iterations": iterations,
				"transport":  transport,
			}, nil, nil, "", "", "", nil))
		}
		if mode == "ndjson" {
			if err := output.PrintJSON(map[string]any{
				"type":       "watch",
				"tool":       "jira",
				"scope":      scope,
				"state_file": stateFile,
				"changed":    changed,
				"payload":    payload,
				"iteration":  iterations,
				"transport":  transport,
			}); err != nil {
				return err
			}
		} else if changed {
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
		fmt.Printf("Completed %d watch cycle(s) for %s using %s.\n", iterations, scope, transport)
	}
	return nil
}

func runJiraShellHook(cmd *cobra.Command, script string) error {
	child := exec.CommandContext(cmd.Context(), "/bin/sh", "-lc", script)
	child.Stdout = cmd.OutOrStdout()
	child.Stderr = cmd.ErrOrStderr()
	return child.Run()
}
