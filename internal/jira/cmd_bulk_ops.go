package jira

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/notabhay/cojira/internal/undo"
	"github.com/spf13/cobra"
)

type bulkIssueMutationResult struct {
	item    map[string]any
	failure *failureEntry
	skipped bool
	success bool
}

type bulkIssueMutationRunner func(issueID string, dryRun bool) (string, map[string]any, error)

func addStandardBulkIssueFlags(cmd *cobra.Command) {
	cmd.Flags().String("jql", "", "JQL query to select issues")
	cmd.Flags().Int("page-size", 100, "Search page size (default: 100)")
	cmd.Flags().Int("limit", 0, "Limit number of issues processed")
	cmd.Flags().Int("concurrency", 1, "Number of concurrent issue workers (default: 1, max: 10)")
	cmd.Flags().Float64("sleep", 0.0, "Delay between operations in seconds")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	_ = cmd.MarkFlagRequired("jql")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
}

func executeBulkIssueMutation(cmd *cobra.Command, operation string, target map[string]any, keys []string, dryRun bool, idemKey string, quiet bool, sleepSec float64, concurrency int, runner bulkIssueMutationRunner) error {
	mode := cli.NormalizeOutputMode(cmd)
	reqID := output.RequestID()

	if len(keys) == 0 {
		if mode == "summary" {
			fmt.Printf("Found 0 issues for JQL: %s\n", normalizeMaybeString(target["jql"]))
			return nil
		}
		fmt.Println("No issues found.")
		return nil
	}

	if idemKey != "" && !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" || mode == "ndjson" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", operation, target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Skipped %s (idempotency key already used): %s\n", operation, idemKey)
		return nil
	}

	results := cli.RunParallel(len(keys), concurrency, func(idx int) bulkIssueMutationResult {
		issueID := keys[idx]
		item := map[string]any{
			"op":     operation,
			"target": map[string]any{"issue": issueID},
			"ok":     false,
		}
		childKey := ""
		if idemKey != "" && !dryRun {
			childKey = output.IdempotencyKey(operation+".item", idemKey, idx, issueID)
			if idempotency.IsDuplicate(childKey) {
				item["ok"] = true
				item["skipped"] = true
				item["resume_key"] = childKey
				receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed %s for %s", operation, issueID)}
				item["receipt"] = receipt.Format()
				return bulkIssueMutationResult{item: item, skipped: true}
			}
		}

		desc, result, err := runner(issueID, dryRun)
		if err != nil {
			item["error"] = err.Error()
			receipt := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", issueID, err)}
			item["receipt"] = receipt.Format()
			return bulkIssueMutationResult{
				item:    item,
				failure: &failureEntry{key: stringOr(desc, issueID), err: err.Error()},
			}
		}
		for key, value := range result {
			item[key] = value
		}
		item["ok"] = true
		if childKey != "" {
			item["resume_key"] = childKey
			_ = idempotency.Record(childKey, fmt.Sprintf("%s %s", operation, issueID))
		}
		if desc != "" {
			receipt := output.Receipt{OK: true, DryRun: dryRun, Message: desc}
			item["receipt"] = receipt.Format()
		}
		if !dryRun && sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
		return bulkIssueMutationResult{item: item, success: true}
	})

	items := make([]map[string]any, 0, len(results))
	successCount := 0
	skippedCount := 0
	failures := make([]failureEntry, 0)
	for idx, result := range results {
		items = append(items, result.item)
		switch {
		case result.skipped:
			skippedCount++
		case result.failure != nil:
			failures = append(failures, *result.failure)
			output.EmitError(cerrors.OpFailed, fmt.Sprintf("%s: %s", result.failure.key, result.failure.err), map[string]any{
				"operation": operation,
				"target":    target,
			})
		case result.success:
			successCount++
		}
		status := "OK"
		if result.skipped {
			status = "SKIPPED"
		} else if result.failure != nil {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(keys), keys[idx], status)
	}

	if idemKey != "" && !dryRun && len(failures) == 0 {
		_ = idempotency.Record(idemKey, operation)
	}

	summary := map[string]any{
		"total":   len(keys),
		"ok":      successCount,
		"skipped": skippedCount,
		"failed":  len(failures),
		"dry_run": dryRun,
	}

	if mode == "json" || mode == "ndjson" {
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, fmt.Sprintf("%s: %s", f.key, f.err), "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			len(failures) == 0, "jira", operation, target,
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("%s complete: %d succeeded, %d skipped, %d failed.\n", bulkOperationLabel(operation), successCount, skippedCount, len(failures))
		if len(failures) > 0 {
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d skipped, %d failed\n", successCount, skippedCount, len(failures))
		printFailures(failures)
	}
	if len(failures) > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}

func bulkOperationLabel(operation string) string {
	switch operation {
	case "bulk-assign":
		return "Bulk assign"
	case "bulk-comment":
		return "Bulk comment"
	case "bulk-label":
		return "Bulk label"
	case "bulk-delete":
		return "Bulk delete"
	case "bulk-attachment":
		return "Bulk attachment"
	case "bulk-link":
		return "Bulk link"
	case "bulk-watch":
		return "Bulk watch"
	case "bulk-worklog":
		return "Bulk worklog"
	default:
		return strings.ReplaceAll(operation, "-", " ")
	}
}

// NewBulkAssignCmd creates the "bulk-assign" subcommand.
func NewBulkAssignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-assign <user>",
		Short: "Assign multiple issues to the same Jira user",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")

			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}
			user, err := resolveUserReference(client, args[0])
			if err != nil {
				return err
			}
			payload := jiraUserAssignmentPayload(user)
			display := formatUserDisplay(user)
			undoGroupID := ""
			snapshots := map[string]map[string]any{}
			if !dryRun {
				undoGroupID = undo.NewGroupID("jira.bulk-assign")
				if data, err := prefetchBulkUpdateSnapshots(client, keys, []string{"assignee"}); err == nil {
					snapshots = data
				}
			}

			return executeBulkIssueMutation(cmd, "bulk-assign", map[string]any{"jql": jql, "user": args[0]}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				desc := fmt.Sprintf("assign %s to %s", issueID, display)
				if dryRun {
					return desc, map[string]any{"dry_run": true, "user": user, "assignment": payload}, nil
				}
				if err := client.AssignIssue(issueID, payload); err != nil {
					return desc, nil, err
				}
				recordUndoEntry(undoGroupID, issueID, "jira.bulk-assign", snapshots[issueID], "", "")
				return desc, map[string]any{"updated": true, "user": user}, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	return cmd
}

// NewBulkCommentCmd creates the "bulk-comment" subcommand.
func NewBulkCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-comment",
		Short: "Add the same comment to multiple Jira issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")
			addBody, _ := cmd.Flags().GetString("add")
			bodyFile, _ := cmd.Flags().GetString("file")
			format, _ := cmd.Flags().GetString("format")

			if addBody != "" && bodyFile != "" {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --add or --file, not both.", ExitCode: 2}
			}
			if bodyFile != "" {
				content, err := readTextFile(bodyFile)
				if err != nil {
					return err
				}
				addBody = content
			}
			bodyText := strings.TrimSpace(addBody)
			if bodyText == "" {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Comment body cannot be empty.", ExitCode: 2}
			}
			bodyValue, err := normalizeJiraRichTextValue(bodyText, format, jiraUsesADF())
			if err != nil {
				return err
			}
			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}
			undoGroupID := ""
			if !dryRun {
				undoGroupID = undo.NewGroupID("jira.bulk-comment")
			}

			return executeBulkIssueMutation(cmd, "bulk-comment", map[string]any{"jql": jql}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				desc := fmt.Sprintf("comment on %s", issueID)
				if dryRun {
					return desc, map[string]any{"dry_run": true, "body": bodyValue}, nil
				}
				comment, err := client.AddComment(issueID, bodyValue)
				if err != nil {
					return desc, nil, err
				}
				recordUndoAction(undoGroupID, issueID, "jira.bulk-comment", "comment.delete", map[string]any{
					"comment_id": normalizeMaybeString(comment["id"]),
				})
				return desc, map[string]any{"comment": comment}, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	cmd.Flags().String("add", "", "Comment body to add")
	cmd.Flags().String("file", "", "Read comment body from a file")
	cmd.Flags().String("format", "raw", "Comment body format: raw or markdown")
	return cmd
}

// NewBulkWatchCmd creates the "bulk-watch" subcommand.
func NewBulkWatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-watch <user>",
		Short: "Add or remove the same watcher across multiple Jira issues",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")
			remove, _ := cmd.Flags().GetBool("remove")

			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}
			user, err := resolveUserReference(client, args[0])
			if err != nil {
				return err
			}
			watcherValue, removeKey := watcherReferenceForAPI(user)
			action := "add"
			if remove {
				action = "remove"
			}
			undoGroupID := ""
			if !dryRun {
				undoGroupID = undo.NewGroupID("jira.bulk-watch")
			}

			return executeBulkIssueMutation(cmd, "bulk-watch", map[string]any{"jql": jql, "user": args[0], "action": action}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				desc := fmt.Sprintf("%s watcher on %s", action, issueID)
				if dryRun {
					return desc, map[string]any{"dry_run": true, "action": action, "watcher": user}, nil
				}
				var err error
				if remove {
					err = client.RemoveWatcher(issueID, removeKey, watcherValue)
				} else {
					err = client.AddWatcher(issueID, watcherValue)
				}
				if err != nil {
					return desc, nil, err
				}
				undoAction := "watcher.remove"
				if remove {
					undoAction = "watcher.add"
				}
				recordUndoAction(undoGroupID, issueID, "jira.bulk-watch", undoAction, map[string]any{
					"param_key": removeKey,
					"value":     watcherValue,
				})
				return desc, map[string]any{"updated": true, "action": action, "watcher": user}, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	cmd.Flags().Bool("remove", false, "Remove the watcher instead of adding it")
	return cmd
}

// NewBulkDeleteCmd creates the "bulk-delete" subcommand.
func NewBulkDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-delete",
		Short: "Delete multiple Jira issues selected by JQL",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")
			deleteSubtasks, _ := cmd.Flags().GetBool("delete-subtasks")
			yes, _ := cmd.Flags().GetBool("yes")
			if !dryRun && !yes {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Bulk delete is destructive. Preview with --dry-run first, then rerun with --yes to confirm.",
					ExitCode: 2,
				}
			}

			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}

			return executeBulkIssueMutation(cmd, "bulk-delete", map[string]any{"jql": jql, "delete_subtasks": deleteSubtasks}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				desc := fmt.Sprintf("delete %s", issueID)
				if dryRun {
					return desc, map[string]any{"dry_run": true, "delete_subtasks": deleteSubtasks}, nil
				}
				if err := client.DeleteIssue(issueID, deleteSubtasks); err != nil {
					return desc, nil, err
				}
				return desc, map[string]any{"deleted": true}, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	cmd.Flags().Bool("delete-subtasks", false, "Also delete subtasks")
	cmd.Flags().Bool("yes", false, "Confirm destructive deletion")
	return cmd
}

// NewBulkLinkCmd creates the "bulk-link" subcommand.
func NewBulkLinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-link <target-issue>",
		Short: "Create the same issue link from many Jira issues to one target issue",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")
			linkType, _ := cmd.Flags().GetString("type")
			commentText, _ := cmd.Flags().GetString("comment")
			commentFile, _ := cmd.Flags().GetString("comment-file")

			if commentText != "" && commentFile != "" {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --comment or --comment-file, not both.", ExitCode: 2}
			}
			if commentFile != "" {
				content, err := readTextFile(commentFile)
				if err != nil {
					return err
				}
				commentText = strings.TrimSpace(content)
			}
			targetIssue := ResolveIssueIdentifier(args[0])
			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}

			return executeBulkIssueMutation(cmd, "bulk-link", map[string]any{"jql": jql, "target_issue": targetIssue, "type": linkType}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				desc := fmt.Sprintf("link %s to %s", issueID, targetIssue)
				if issueID == targetIssue {
					return desc, nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Source and target issue cannot be the same.", ExitCode: 1}
				}
				payload := map[string]any{
					"type":         map[string]any{"name": linkType},
					"outwardIssue": map[string]any{"key": issueID},
					"inwardIssue":  map[string]any{"key": targetIssue},
				}
				if commentText != "" {
					payload["comment"] = map[string]any{"body": commentText}
				}
				if dryRun {
					return desc, map[string]any{"dry_run": true, "payload": payload}, nil
				}
				if err := client.CreateIssueLink(payload); err != nil {
					return desc, nil, err
				}
				return desc, map[string]any{"linked": true, "type": linkType, "target_issue": targetIssue}, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	cmd.Flags().String("type", "Relates", "Link type name (for example: Relates, Blocks, Duplicates)")
	cmd.Flags().String("comment", "", "Optional comment to include with the link")
	cmd.Flags().String("comment-file", "", "Read the optional comment from a file")
	return cmd
}

// NewBulkLabelCmd creates the "bulk-label" subcommand.
func NewBulkLabelCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-label",
		Short: "Add, remove, or replace labels across many Jira issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")
			addLabels, _ := cmd.Flags().GetStringSlice("add")
			removeLabels, _ := cmd.Flags().GetStringSlice("remove")
			setLabels, _ := cmd.Flags().GetStringSlice("set")

			addLabels = normalizeStringSlice(addLabels)
			removeLabels = normalizeStringSlice(removeLabels)
			setLabels = normalizeStringSlice(setLabels)
			if len(setLabels) == 0 && len(addLabels) == 0 && len(removeLabels) == 0 {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Provide at least one of --add, --remove, or --set.",
					ExitCode: 2,
				}
			}

			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}
			snapshots, _ := prefetchBulkUpdateSnapshots(client, keys, []string{"labels"})
			undoGroupID := ""
			if !dryRun {
				undoGroupID = undo.NewGroupID("jira.bulk-label")
			}

			return executeBulkIssueMutation(cmd, "bulk-label", map[string]any{
				"jql":    jql,
				"add":    addLabels,
				"remove": removeLabels,
				"set":    setLabels,
			}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				current := []string{}
				if snapshot, ok := snapshots[issueID]; ok {
					current = normalizeLabels(snapshot["labels"])
				}
				next := current
				if len(setLabels) > 0 {
					next = append([]string{}, setLabels...)
				}
				for _, label := range addLabels {
					merged, err := MergeListOfStrings(next, OpListAppend, label)
					if err != nil {
						return "", nil, err
					}
					next = merged
				}
				for _, label := range removeLabels {
					merged, err := MergeListOfStrings(next, OpListRemove, label)
					if err != nil {
						return "", nil, err
					}
					next = merged
				}

				desc := fmt.Sprintf("label %s", issueID)
				result := map[string]any{
					"labels_before": current,
					"labels_after":  next,
					"changed":       strings.Join(current, "\x00") != strings.Join(next, "\x00"),
				}
				if dryRun {
					result["dry_run"] = true
					return desc, result, nil
				}
				if changed, _ := result["changed"].(bool); !changed {
					return desc, result, nil
				}
				payload := map[string]any{"fields": map[string]any{"labels": next}}
				if err := client.UpdateIssue(issueID, payload, true); err != nil {
					return desc, nil, err
				}
				recordUndoEntry(undoGroupID, issueID, "jira.bulk-label", snapshots[issueID], "", "")
				return desc, result, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	cmd.Flags().StringSlice("add", nil, "Label to add (repeatable)")
	cmd.Flags().StringSlice("remove", nil, "Label to remove (repeatable)")
	cmd.Flags().StringSlice("set", nil, "Replace labels with this exact list (repeatable)")
	return cmd
}

// NewBulkAttachmentCmd creates the "bulk-attachment" subcommand.
func NewBulkAttachmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-attachment",
		Short: "Upload the same attachments across many Jira issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")
			uploads, _ := cmd.Flags().GetStringArray("upload")
			useStdin, _ := cmd.Flags().GetBool("stdin")
			stdinFilename, _ := cmd.Flags().GetString("filename")

			if len(uploads) == 0 && !useStdin {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Provide at least one --upload file or use --stdin.",
					ExitCode: 2,
				}
			}
			if len(uploads) > 0 && useStdin {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Use either --upload or --stdin, not both.",
					ExitCode: 2,
				}
			}
			var stdinData []byte
			if useStdin {
				if strings.TrimSpace(stdinFilename) == "" {
					return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--filename is required with --stdin.", ExitCode: 2}
				}
				if !dryRun {
					data, err := io.ReadAll(cmd.InOrStdin())
					if err != nil {
						return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Failed to read attachment from stdin: %v", err), ExitCode: 1}
					}
					stdinData = data
				}
			}

			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}
			undoGroupID := ""
			if !dryRun {
				undoGroupID = undo.NewGroupID("jira.bulk-attachment")
			}

			return executeBulkIssueMutation(cmd, "bulk-attachment", map[string]any{
				"jql":      jql,
				"files":    uploads,
				"stdin":    useStdin,
				"filename": stdinFilename,
			}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				desc := fmt.Sprintf("attach files to %s", issueID)
				if dryRun {
					result := map[string]any{"dry_run": true}
					if useStdin {
						result["stdin"] = true
						result["filename"] = stdinFilename
					} else {
						result["files"] = uploads
					}
					return desc, result, nil
				}
				items := make([]map[string]any, 0, len(uploads))
				if useStdin {
					item, err := client.UploadAttachmentBytes(issueID, stdinFilename, stdinData)
					if err != nil {
						return desc, nil, err
					}
					items = append(items, item)
					recordUndoAction(undoGroupID, issueID, "jira.bulk-attachment", "attachment.delete", map[string]any{
						"attachment_id": normalizeMaybeString(item["id"]),
						"filename":      normalizeMaybeString(item["filename"]),
					})
				} else {
					for _, filePath := range uploads {
						item, err := client.UploadAttachment(issueID, filePath)
						if err != nil {
							return desc, nil, err
						}
						items = append(items, item)
						recordUndoAction(undoGroupID, issueID, "jira.bulk-attachment", "attachment.delete", map[string]any{
							"attachment_id": normalizeMaybeString(item["id"]),
							"filename":      normalizeMaybeString(item["filename"]),
						})
					}
				}
				return desc, map[string]any{"attachments": items, "uploaded": len(items)}, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	cmd.Flags().StringArray("upload", nil, "File to upload to each issue (repeatable)")
	cmd.Flags().Bool("stdin", false, "Read one attachment body from stdin and upload it to each issue")
	cmd.Flags().String("filename", "", "Attachment filename to use with --stdin")
	return cmd
}

// NewBulkWorklogCmd creates the "bulk-worklog" subcommand.
func NewBulkWorklogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-worklog",
		Short: "Add the same worklog to multiple Jira issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jqlFlag, _ := cmd.Flags().GetString("jql")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			limit, _ := cmd.Flags().GetInt("limit")
			concurrency, _ := cmd.Flags().GetInt("concurrency")
			sleepSec, _ := cmd.Flags().GetFloat64("sleep")
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			quiet, _ := cmd.Flags().GetBool("quiet")
			commentText, _ := cmd.Flags().GetString("comment")
			commentFile, _ := cmd.Flags().GetString("comment-file")
			timeSpent, _ := cmd.Flags().GetString("time-spent")
			started, _ := cmd.Flags().GetString("started")

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
			if strings.TrimSpace(timeSpent) == "" {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--time-spent is required.", ExitCode: 2}
			}
			if strings.TrimSpace(started) == "" {
				started = time.Now().Format("2006-01-02T15:04:05.000-0700")
			}

			payload := map[string]any{
				"timeSpent": strings.TrimSpace(timeSpent),
				"started":   strings.TrimSpace(started),
			}
			if trimmed := strings.TrimSpace(commentText); trimmed != "" {
				payload["comment"] = trimmed
			}

			jql := applyDefaultScope(cmd, jqlFlag)
			keys, err := collectIssueKeys(client, jql, pageSize)
			if err != nil {
				return err
			}
			if limit > 0 && len(keys) > limit {
				keys = keys[:limit]
			}
			undoGroupID := ""
			if !dryRun {
				undoGroupID = undo.NewGroupID("jira.bulk-worklog")
			}

			return executeBulkIssueMutation(cmd, "bulk-worklog", map[string]any{
				"jql": jql,
			}, keys, dryRun, idemKey, quiet, sleepSec, concurrency, func(issueID string, dryRun bool) (string, map[string]any, error) {
				desc := fmt.Sprintf("add worklog to %s", issueID)
				if dryRun {
					return desc, map[string]any{"dry_run": true, "payload": payload}, nil
				}
				result, err := client.AddWorklog(issueID, payload)
				if err != nil {
					return desc, nil, err
				}
				recordUndoAction(undoGroupID, issueID, "jira.bulk-worklog", "worklog.delete", map[string]any{
					"worklog_id": normalizeMaybeString(result["id"]),
				})
				return desc, map[string]any{"worklog": result}, nil
			})
		},
	}
	addStandardBulkIssueFlags(cmd)
	cmd.Flags().String("comment", "", "Worklog comment text")
	cmd.Flags().String("comment-file", "", "Read worklog comment from a file")
	cmd.Flags().String("time-spent", "", "Time spent, for example 1h 30m")
	cmd.Flags().String("started", "", "Started timestamp (default: now)")
	return cmd
}

func normalizeLabels(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return normalizeStringSlice(typed)
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := normalizeMaybeString(item); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return []string{}
	}
}
