package jira

import (
	"fmt"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/notabhay/cojira/internal/undo"
	"github.com/spf13/cobra"
)

// NewBulkUpdateCmd creates the "bulk-update" subcommand.
func NewBulkUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-update",
		Short: "Update multiple issues using a shared JSON payload",
		Long:  "Apply the same JSON payload to issues returned by a JQL search.",
		RunE:  runBulkUpdate,
	}
	cmd.Flags().String("jql", "", "JQL query to select issues")
	cmd.Flags().String("payload", "", "JSON payload file to apply")
	cmd.Flags().Int("page-size", 100, "Search page size (default: 100)")
	cmd.Flags().Int("limit", 0, "Limit number of issues processed")
	cmd.Flags().Int("concurrency", 1, "Number of concurrent issue workers (default: 1, max: 10)")
	cmd.Flags().Float64("sleep", 0.0, "Delay between updates in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	_ = cmd.MarkFlagRequired("jql")
	_ = cmd.MarkFlagRequired("payload")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runBulkUpdate(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	jqlFlag, _ := cmd.Flags().GetString("jql")
	payloadFile, _ := cmd.Flags().GetString("payload")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	limit, _ := cmd.Flags().GetInt("limit")
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")

	jql := applyDefaultScope(cmd, jqlFlag)
	reqID := output.RequestID()
	if idemKey != "" && !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-update",
				map[string]any{"jql": jql, "payload": payloadFile},
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Skipped bulk update (idempotency key already used): %s\n", idemKey)
		return nil
	}

	payload, err := readJSONFile(payloadFile)
	if err != nil {
		return err
	}
	undoGroupID := ""
	if !dryRun {
		undoGroupID = undo.NewGroupID("jira.bulk-update")
	}
	fieldNames := payloadFieldNames(payload)

	keys, err := collectIssueKeys(client, jql, pageSize)
	if err != nil {
		return err
	}
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	if len(keys) == 0 {
		if mode == "summary" {
			fmt.Printf("Found 0 issues for JQL: %s\n", jql)
			return nil
		}
		fmt.Println("No issues found.")
		return nil
	}

	currentFieldSnapshots := map[string]map[string]any{}
	if !dryRun && len(fieldNames) > 0 {
		if snapshotMap, err := prefetchBulkUpdateSnapshots(client, keys, fieldNames); err == nil {
			currentFieldSnapshots = snapshotMap
		}
	}

	success := 0
	skipped := 0
	var failures []failureEntry
	var items []map[string]any

	if dryRun && mode != "json" && !quiet && mode != "summary" {
		fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
	}

	concurrency = cli.ClampConcurrency(concurrency)
	if concurrency > 1 {
		type bulkUpdateResult struct {
			item    map[string]any
			failure *failureEntry
			skipped bool
			success bool
		}

		results := cli.RunParallel(len(keys), concurrency, func(idx int) bulkUpdateResult {
			key := keys[idx]
			item := map[string]any{"op": "update", "target": map[string]any{"issue": key}, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("jira.bulk-update.item", idemKey, idx, key, payload)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed update for %s", key)}
					item["receipt"] = receipt.Format()
					return bulkUpdateResult{item: item, skipped: true}
				}
			}

			var opErr error
			undoFields := currentFieldSnapshots[key]
			if dryRun {
				fieldKeys := make([]string, 0)
				if flds, ok := payload["fields"].(map[string]any); ok {
					for k := range flds {
						fieldKeys = append(fieldKeys, k)
					}
				}
				issue, e := client.GetIssue(key, strings.Join(fieldKeys, ","), "")
				if e != nil {
					opErr = e
				} else {
					diffs := previewPayloadDiff(key, issue, payload, true)
					item["diffs"] = diffs
				}
			}
			if !dryRun && opErr == nil {
				if e := client.UpdateIssue(key, payload, !noNotify); e != nil {
					opErr = e
				} else {
					r := output.Receipt{OK: true, Message: fmt.Sprintf("Updated %s", key)}
					item["receipt"] = r.Format()
					recordUndoEntry(undoGroupID, key, "jira.bulk-update", undoFields, "", "")
				}
			}

			if !dryRun && sleepSec > 0 {
				time.Sleep(time.Duration(sleepSec * float64(time.Second)))
			}

			if opErr != nil {
				item["ok"] = false
				item["error"] = opErr.Error()
				r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", key, opErr)}
				item["receipt"] = r.Format()
				return bulkUpdateResult{item: item, failure: &failureEntry{key: key, err: opErr.Error()}}
			}

			item["ok"] = true
			if childKey != "" {
				item["resume_key"] = childKey
				_ = idempotency.Record(childKey, fmt.Sprintf("jira.bulk-update %s", key))
			}
			return bulkUpdateResult{item: item, success: true}
		})

		for idx, result := range results {
			items = append(items, result.item)
			switch {
			case result.skipped:
				skipped++
			case result.failure != nil:
				failures = append(failures, *result.failure)
			case result.success:
				success++
			}

			status := "OK"
			if result.skipped {
				status = "SKIPPED"
			} else if result.failure != nil {
				status = "FAILED"
			}
			output.EmitProgress(mode, quiet, idx+1, len(keys), keys[idx], status)
		}

		if mode != "json" && !quiet && mode != "summary" {
			for _, item := range items {
				receipt, _ := item["receipt"].(string)
				if receipt == "" {
					continue
				}
				if ok, _ := item["ok"].(bool); ok {
					fmt.Println(receipt)
				} else {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), receipt)
				}
			}
		}
	} else {

		for idx, key := range keys {
			item := map[string]any{"op": "update", "target": map[string]any{"issue": key}, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("jira.bulk-update.item", idemKey, idx, key, payload)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed update for %s", key)}
					item["receipt"] = receipt.Format()
					items = append(items, item)
					skipped++
					output.EmitProgress(mode, quiet, idx+1, len(keys), key, "SKIPPED")
					continue
				}
			}
			var opErr error
			undoFields := currentFieldSnapshots[key]

			if dryRun {
				fieldKeys := make([]string, 0)
				if flds, ok := payload["fields"].(map[string]any); ok {
					for k := range flds {
						fieldKeys = append(fieldKeys, k)
					}
				}
				issue, e := client.GetIssue(key, strings.Join(fieldKeys, ","), "")
				if e != nil {
					opErr = e
				} else {
					diffs := previewPayloadDiff(key, issue, payload, mode == "json" || quiet)
					item["diffs"] = diffs
				}
			}
			if !dryRun && opErr == nil {
				if e := client.UpdateIssue(key, payload, !noNotify); e != nil {
					opErr = e
				} else {
					r := output.Receipt{OK: true, Message: fmt.Sprintf("Updated %s", key)}
					item["receipt"] = r.Format()
					recordUndoEntry(undoGroupID, key, "jira.bulk-update", undoFields, "", "")
					if mode != "json" && !quiet && mode != "summary" {
						fmt.Println(r.Format())
					}
				}
			}

			if opErr != nil {
				item["ok"] = false
				item["error"] = opErr.Error()
				r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", key, opErr)}
				item["receipt"] = r.Format()
				if mode != "json" && !quiet && mode != "summary" {
					_, _ = fmt.Fprintln(cmd.ErrOrStderr(), r.Format())
				}
				failures = append(failures, failureEntry{key: key, err: opErr.Error()})
			} else {
				item["ok"] = true
				if childKey != "" {
					item["resume_key"] = childKey
					_ = idempotency.Record(childKey, fmt.Sprintf("jira.bulk-update %s", key))
				}
				success++
			}

			items = append(items, item)
			status := "OK"
			if !item["ok"].(bool) {
				status = "FAILED"
			}
			output.EmitProgress(mode, quiet, idx+1, len(keys), key, status)

			if sleepSec > 0 {
				time.Sleep(time.Duration(sleepSec * float64(time.Second)))
			}
		}
	}

	summary := map[string]any{
		"total":   len(keys),
		"ok":      success,
		"skipped": skipped,
		"failed":  len(failures),
		"dry_run": dryRun,
	}
	if idemKey != "" && !dryRun && len(failures) == 0 {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.bulk-update %s", payloadFile))
	}

	if mode == "json" {
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, fmt.Sprintf("%s: %s", f.key, f.err), "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			len(failures) == 0, "jira", "bulk-update",
			map[string]any{"jql": jql, "payload": payloadFile},
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Bulk update complete: %d succeeded, %d skipped, %d failed.\n", success, skipped, len(failures))
		if len(failures) > 0 {
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d skipped, %d failed\n", success, skipped, len(failures))
		printFailures(failures)
	}
	if len(failures) > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}

func prefetchBulkUpdateSnapshots(client *Client, keys, fieldNames []string) (map[string]map[string]any, error) {
	if len(keys) == 0 || len(fieldNames) == 0 {
		return map[string]map[string]any{}, nil
	}
	quoted := make([]string, 0, len(keys))
	for _, key := range keys {
		quoted = append(quoted, JQLValue(key))
	}
	jql := fmt.Sprintf("issuekey in (%s)", strings.Join(quoted, ", "))
	data, err := client.Search(jql, len(keys), 0, strings.Join(fieldNames, ","), "")
	if err != nil {
		return nil, err
	}
	result := map[string]map[string]any{}
	rawIssues, _ := data["issues"].([]any)
	for _, raw := range rawIssues {
		issue, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		key := normalizeMaybeString(issue["key"])
		fields, _ := issue["fields"].(map[string]any)
		result[key] = snapshotFieldValues(fields, fieldNames)
	}
	return result, nil
}
