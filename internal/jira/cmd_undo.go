package jira

import (
	"encoding/json"
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/notabhay/cojira/internal/undo"
	"github.com/spf13/cobra"
)

// NewUndoCmd creates the "undo" command group.
func NewUndoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "undo",
		Short: "List and revert recent Jira updates or transitions",
	}
	cmd.AddCommand(newUndoListCmd(), newUndoApplyCmd())
	return cmd
}

func newUndoListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List recent undo entries",
		RunE: func(cmd *cobra.Command, _ []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			limit, _ := cmd.Flags().GetInt("limit")
			entries, err := undo.ListIssues(limit)
			if err != nil {
				return err
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "undo.list", map[string]any{}, map[string]any{"entries": entries}, nil, nil, "", "", "", nil))
			}
			if len(entries) == 0 {
				fmt.Println("No undo entries found.")
				return nil
			}
			if mode == "summary" {
				fmt.Printf("Found %d undo entr%s.\n", len(entries), pluralSuffix(len(entries), "y", "ies"))
				return nil
			}
			for _, entry := range entries {
				fmt.Printf("%s  %-18s %-14s %-12s %s\n", entry.Timestamp.Format("2006-01-02 15:04:05"), entry.GroupID, entry.Operation, entry.Issue, entry.FromStatus)
			}
			return nil
		},
	}
	cmd.Flags().Int("limit", 20, "Maximum undo entries to list")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newUndoApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "apply [issue]",
		Short: "Apply the latest undo entry for an issue or the latest group",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runUndoApply,
	}
	cmd.Flags().Bool("last-group", false, "Undo the most recent recorded group")
	cmd.Flags().Bool("dry-run", false, "Preview the undo without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runUndoApply(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	lastGroup, _ := cmd.Flags().GetBool("last-group")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	var entries []undo.IssueEntry
	switch {
	case lastGroup || len(args) == 0:
		entries, err = undo.LatestGroup()
	case len(args) > 0:
		var entry *undo.IssueEntry
		entry, err = undo.LatestIssue(ResolveIssueIdentifier(args[0]))
		if entry != nil {
			entries = []undo.IssueEntry{*entry}
		}
	}
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "No matching undo entry found.", ExitCode: 1}
	}

	results := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		item := map[string]any{
			"issue":       entry.Issue,
			"operation":   entry.Operation,
			"group_id":    entry.GroupID,
			"from_status": entry.FromStatus,
			"to_status":   entry.ToStatus,
		}
		if dryRun {
			if entry.UndoAction != "" {
				item["undo_action"] = entry.UndoAction
				item["payload"] = cloneAnyMap(entry.Payload)
			}
			item["dry_run"] = true
			results = append(results, item)
			continue
		}

		if entry.UndoAction != "" {
			if err := applyUndoAction(client, entry); err != nil {
				return err
			}
		}
		if entry.FromStatus != "" && entry.ToStatus != "" {
			if err := revertIssueStatus(client, entry.Issue, entry.FromStatus); err != nil {
				return err
			}
		}
		if len(entry.Fields) > 0 {
			payload := map[string]any{"fields": cloneAnyMap(entry.Fields)}
			if err := client.UpdateIssue(entry.Issue, payload, true); err != nil {
				return err
			}
		}
		item["reverted"] = true
		results = append(results, item)
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "undo.apply", map[string]any{}, map[string]any{"items": results}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		if dryRun {
			fmt.Printf("Would undo %d Jira entr%s.\n", len(results), pluralSuffix(len(results), "y", "ies"))
		} else {
			fmt.Printf("Undid %d Jira entr%s.\n", len(results), pluralSuffix(len(results), "y", "ies"))
		}
		return nil
	}
	for _, result := range results {
		text, _ := json.Marshal(result)
		fmt.Println(string(text))
	}
	return nil
}

func applyUndoAction(client *Client, entry undo.IssueEntry) error {
	switch entry.UndoAction {
	case "comment.delete":
		return client.DeleteComment(entry.Issue, normalizeMaybeString(entry.Payload["comment_id"]))
	case "comment.add":
		_, err := client.AddComment(entry.Issue, entry.Payload["body"])
		return err
	case "comment.update":
		_, err := client.UpdateComment(entry.Issue, normalizeMaybeString(entry.Payload["comment_id"]), entry.Payload["body"])
		return err
	case "watcher.add":
		return client.AddWatcher(entry.Issue, normalizeMaybeString(entry.Payload["value"]))
	case "watcher.remove":
		return client.RemoveWatcher(entry.Issue, normalizeMaybeString(entry.Payload["param_key"]), normalizeMaybeString(entry.Payload["value"]))
	case "worklog.delete":
		return client.DeleteWorklog(entry.Issue, normalizeMaybeString(entry.Payload["worklog_id"]))
	case "worklog.add":
		payload := cloneAnyMap(mapFromAny(entry.Payload["payload"]))
		_, err := client.AddWorklog(entry.Issue, payload)
		return err
	case "worklog.update":
		payload := cloneAnyMap(mapFromAny(entry.Payload["payload"]))
		_, err := client.UpdateWorklog(entry.Issue, normalizeMaybeString(entry.Payload["worklog_id"]), payload)
		return err
	case "attachment.delete":
		return client.DeleteAttachment(normalizeMaybeString(entry.Payload["attachment_id"]))
	default:
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported undo action %q for %s.", entry.UndoAction, entry.Issue),
			ExitCode: 1,
		}
	}
}

func revertIssueStatus(client *Client, issueID, targetStatus string) error {
	transitions, err := client.ListTransitions(issueID)
	if err != nil {
		return err
	}
	raw, _ := transitions["transitions"].([]any)
	matches := filterTransitionsByStatus(raw, targetStatus)
	if len(matches) == 0 {
		return &cerrors.CojiraError{
			Code:     cerrors.TransitionNotFound,
			Message:  fmt.Sprintf("No transition back to %q found for %s.", targetStatus, issueID),
			ExitCode: 1,
		}
	}
	match, _ := matches[0].(map[string]any)
	return client.TransitionIssue(issueID, map[string]any{
		"transition": map[string]any{"id": fmt.Sprintf("%v", match["id"])},
	}, true)
}

func cloneAnyMap(input map[string]any) map[string]any {
	if input == nil {
		return nil
	}
	data, err := json.Marshal(input)
	if err != nil {
		return input
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return input
	}
	return out
}

func mapFromAny(value any) map[string]any {
	if value == nil {
		return nil
	}
	if typed, ok := value.(map[string]any); ok {
		return typed
	}
	data, err := json.Marshal(value)
	if err != nil {
		return nil
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return nil
	}
	return out
}
