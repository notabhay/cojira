package jira

import (
	"fmt"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewWorklogCmd creates the "worklog" subcommand.
func NewWorklogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "worklog <issue>",
		Short: "List, add, update, or delete Jira worklogs",
		Args:  cobra.ExactArgs(1),
		RunE:  runWorklog,
	}
	cmd.Flags().Bool("add", false, "Add a new worklog")
	cmd.Flags().String("update", "", "Worklog ID to update")
	cmd.Flags().String("delete", "", "Worklog ID to delete")
	cmd.Flags().String("comment", "", "Worklog comment text")
	cmd.Flags().String("comment-file", "", "Read worklog comment from a file")
	cmd.Flags().String("time-spent", "", "Time spent, for example 1h 30m")
	cmd.Flags().String("started", "", "Started timestamp (default: now)")
	cmd.Flags().Bool("all", false, "Fetch all worklogs")
	cmd.Flags().Int("limit", 20, "Maximum worklogs to fetch")
	cmd.Flags().Int("start", 0, "Start offset for listing")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cmd.Flags().Bool("dry-run", false, "Preview worklog changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runWorklog(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	addMode, _ := cmd.Flags().GetBool("add")
	updateID, _ := cmd.Flags().GetString("update")
	deleteID, _ := cmd.Flags().GetString("delete")
	commentText, _ := cmd.Flags().GetString("comment")
	commentFile, _ := cmd.Flags().GetString("comment-file")
	timeSpent, _ := cmd.Flags().GetString("time-spent")
	started, _ := cmd.Flags().GetString("started")
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	actionCount := 0
	if addMode {
		actionCount++
	}
	if updateID != "" {
		actionCount++
	}
	if deleteID != "" {
		actionCount++
	}
	if actionCount > 1 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use only one of --add, --update, or --delete.", ExitCode: 2}
	}

	if commentText != "" && commentFile != "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --comment or --comment-file, not both.", ExitCode: 2}
	}
	if commentFile != "" {
		content, err := readTextFile(commentFile)
		if err != nil {
			return err
		}
		commentText = content
	}
	if strings.TrimSpace(started) == "" {
		started = time.Now().Format("2006-01-02T15:04:05.000-0700")
	}

	switch {
	case deleteID != "":
		return runDeleteWorklog(cmd, client, issueID, deleteID, dryRun, idemKey, mode)
	case addMode || updateID != "":
		if strings.TrimSpace(timeSpent) == "" {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--time-spent is required when adding or updating a worklog.", ExitCode: 2}
		}
		payload := map[string]any{
			"timeSpent": strings.TrimSpace(timeSpent),
			"started":   started,
			"comment":   strings.TrimSpace(commentText),
		}
		if addMode {
			return runAddWorklog(cmd, client, issueID, payload, dryRun, idemKey, mode)
		}
		return runUpdateWorklog(cmd, client, issueID, updateID, payload, dryRun, idemKey, mode)
	default:
		return runListWorklogs(cmd, client, issueID, all, limit, start, pageSize, mode)
	}
}

func runListWorklogs(cmd *cobra.Command, client *Client, issueID string, all bool, limit, start, pageSize int, mode string) error {
	items := make([]map[string]any, 0)
	total := 0
	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.ListWorklogs(issueID, pageSize, offset)
			if err != nil {
				return err
			}
			raw, _ := data["worklogs"].([]any)
			pageItems := coerceJSONArray(raw)
			total = intFromAny(data["total"], total)
			items = append(items, pageItems...)
			offset += len(pageItems)
			if len(pageItems) == 0 || (total > 0 && offset >= total) {
				break
			}
		}
	} else {
		data, err := client.ListWorklogs(issueID, limit, start)
		if err != nil {
			return err
		}
		raw, _ := data["worklogs"].([]any)
		items = coerceJSONArray(raw)
		total = intFromAny(data["total"], len(items))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "worklog",
			map[string]any{"issue": issueID},
			map[string]any{"worklogs": items, "summary": map[string]any{"count": len(items), "total": total}},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		if all {
			fmt.Printf("Found %d worklog(s) on %s.\n", len(items), issueID)
		} else {
			fmt.Printf("Fetched %d of %d worklog(s) on %s.\n", len(items), total, issueID)
		}
		return nil
	}
	if len(items) == 0 {
		fmt.Println("No worklogs found.")
		return nil
	}
	fmt.Printf("Worklogs for %s:\n\n", issueID)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		author, _ := item["author"].(map[string]any)
		rows = append(rows, []string{
			normalizeMaybeString(item["id"]),
			output.Truncate(formatUserDisplay(author), 24),
			formatHumanTimestamp(normalizeMaybeString(item["started"])),
			normalizeMaybeString(item["timeSpent"]),
			output.Truncate(compactWhitespace(normalizeMaybeString(item["comment"])), 56),
		})
	}
	fmt.Println(output.TableString([]string{"ID", "AUTHOR", "STARTED", "SPENT", "COMMENT"}, rows))
	return nil
}

func runAddWorklog(cmd *cobra.Command, client *Client, issueID string, payload map[string]any, dryRun bool, idemKey, mode string) error {
	return mutateWorklog(client, issueID, "", "add", payload, dryRun, idemKey, mode)
}

func runUpdateWorklog(cmd *cobra.Command, client *Client, issueID, worklogID string, payload map[string]any, dryRun bool, idemKey, mode string) error {
	return mutateWorklog(client, issueID, worklogID, "update", payload, dryRun, idemKey, mode)
}

func mutateWorklog(client *Client, issueID, worklogID, action string, payload map[string]any, dryRun bool, idemKey, mode string) error {
	target := map[string]any{"issue": issueID}
	if worklogID != "" {
		target["worklog"] = worklogID
	}
	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "worklog", target, map[string]any{"dry_run": true, "action": action, "payload": payload}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would %s a worklog on %s.\n", action, issueID)
			return nil
		}
		fmt.Printf("Would %s a worklog on %s.\n", action, issueID)
		return nil
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "worklog", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate worklog %s for %s.\n", action, issueID)
		return nil
	}
	var previous map[string]any
	if action == "update" {
		var lookupErr error
		previous, lookupErr = findWorklogByID(client, issueID, worklogID)
		if lookupErr != nil {
			return lookupErr
		}
	}

	var (
		result map[string]any
		err    error
	)
	switch action {
	case "add":
		result, err = client.AddWorklog(issueID, payload)
	case "update":
		result, err = client.UpdateWorklog(issueID, worklogID, payload)
	}
	if err != nil {
		return err
	}
	switch action {
	case "add":
		recordUndoAction("", issueID, "jira.worklog.add", "worklog.delete", map[string]any{
			"worklog_id": normalizeMaybeString(result["id"]),
		})
	case "update":
		recordUndoAction("", issueID, "jira.worklog.update", "worklog.update", map[string]any{
			"worklog_id": worklogID,
			"payload":    worklogUndoPayload(previous),
		})
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.worklog %s %s", action, issueID))
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "worklog", target, map[string]any{"action": action, "worklog": result}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("%sed a worklog on %s.\n", capitalize(action), issueID)
		return nil
	}
	fmt.Printf("%sed worklog %v on %s.\n", capitalize(action), result["id"], issueID)
	return nil
}

func runDeleteWorklog(cmd *cobra.Command, client *Client, issueID, worklogID string, dryRun bool, idemKey, mode string) error {
	target := map[string]any{"issue": issueID, "worklog": worklogID}
	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "worklog", target, map[string]any{"dry_run": true, "action": "delete"}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would delete worklog %s on %s.\n", worklogID, issueID)
			return nil
		}
		fmt.Printf("Would delete worklog %s on %s.\n", worklogID, issueID)
		return nil
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "worklog", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate worklog delete for %s.\n", issueID)
		return nil
	}
	previous, err := findWorklogByID(client, issueID, worklogID)
	if err != nil {
		return err
	}
	if err := client.DeleteWorklog(issueID, worklogID); err != nil {
		return err
	}
	recordUndoAction("", issueID, "jira.worklog.delete", "worklog.add", map[string]any{
		"payload": worklogUndoPayload(previous),
	})
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.worklog delete %s", issueID))
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "worklog", target, map[string]any{"deleted": true}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Deleted worklog %s on %s.\n", worklogID, issueID)
		return nil
	}
	fmt.Printf("Deleted worklog %s on %s.\n", worklogID, issueID)
	return nil
}

func findWorklogByID(client *Client, issueID, worklogID string) (map[string]any, error) {
	offset := 0
	pageSize := 50
	for {
		data, err := client.ListWorklogs(issueID, pageSize, offset)
		if err != nil {
			return nil, err
		}
		raw, _ := data["worklogs"].([]any)
		items := coerceJSONArray(raw)
		for _, item := range items {
			if normalizeMaybeString(item["id"]) == worklogID {
				return item, nil
			}
		}
		offset += len(items)
		total := intFromAny(data["total"], len(items))
		if len(items) == 0 || offset >= total {
			break
		}
	}
	return nil, &cerrors.CojiraError{
		Code:     cerrors.IdentUnresolved,
		Message:  fmt.Sprintf("Worklog %s was not found on %s.", worklogID, issueID),
		ExitCode: 1,
	}
}

func worklogUndoPayload(worklog map[string]any) map[string]any {
	if worklog == nil {
		return map[string]any{}
	}
	return map[string]any{
		"timeSpent": normalizeMaybeString(worklog["timeSpent"]),
		"started":   normalizeMaybeString(worklog["started"]),
		"comment":   worklog["comment"],
	}
}
