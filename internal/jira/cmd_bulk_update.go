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

type bulkUpdatePlan struct {
	Version     int            `json:"version"`
	JQL         string         `json:"jql"`
	PayloadFile string         `json:"payload_file,omitempty"`
	Payload     map[string]any `json:"payload"`
	Keys        []string       `json:"keys"`
	Notify      bool           `json:"notify"`
}

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
	cmd.Flags().Float64("sleep", 0.0, "Delay between updates in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
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
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	reqID := output.RequestID()

	plan, idemKey, err := resolveBulkUpdatePlan(cmd, client, jqlFlag, payloadFile, pageSize, limit, dryRun, idemKey)
	if err != nil {
		return err
	}

	target := map[string]any{"jql": plan.JQL, "payload": plan.PayloadFile}

	if len(plan.Keys) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-update", target,
				map[string]any{
					"items":       []any{},
					"summary":     map[string]any{"total": 0, "ok": 0, "failed": 0, "skipped": 0, "dry_run": dryRun},
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Found 0 issues for JQL: %s\n", plan.JQL)
			return nil
		}
		fmt.Println("No issues found.")
		return nil
	}

	if !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-update", target,
				map[string]any{
					"skipped":     true,
					"reason":      "idempotency_key_already_used",
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped bulk update (idempotency key already used): %s\n", idemKey)
		return nil
	}

	success := 0
	skipped := 0
	var failures []failureEntry
	var items []map[string]any
	var completed []idempotency.ResumeItem
	var remaining []idempotency.ResumeItem

	if dryRun && mode != "json" && !quiet && mode != "summary" {
		fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
	}

	for idx, key := range plan.Keys {
		item := map[string]any{"op": "update", "target": map[string]any{"issue": key}, "ok": false}
		checkpointKey := fmt.Sprintf("%s.issue.%04d.%s", idemKey, idx, key)
		if !dryRun && idempotency.IsDuplicate(checkpointKey) {
			item["ok"] = true
			item["skipped"] = true
			item["reason"] = "idempotency_checkpoint_already_used"
			r := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped %s (already completed in a prior bulk update attempt)", key)}
			item["receipt"] = r.Format()
			items = append(items, item)
			completed = append(completed, idempotency.ResumeItem{
				ID:          key,
				Description: "already completed in a prior attempt",
				Target:      map[string]any{"issue": key},
			})
			skipped++
			output.EmitProgress(mode, quiet, idx+1, len(plan.Keys), key, "SKIPPED")
			continue
		}

		var opErr error
		if dryRun {
			fieldKeys := make([]string, 0)
			if flds, ok := plan.Payload["fields"].(map[string]any); ok {
				for k := range flds {
					fieldKeys = append(fieldKeys, k)
				}
			}
			issue, e := client.GetIssue(key, strings.Join(fieldKeys, ","), "")
			if e != nil {
				opErr = e
			} else {
				diffs := previewPayloadDiff(key, issue, plan.Payload, mode == "json" || quiet)
				item["diffs"] = diffs
			}
		} else {
			if e := client.UpdateIssue(key, plan.Payload, plan.Notify); e != nil {
				opErr = e
			} else if recErr := idempotency.RecordValue(checkpointKey, "jira.bulk-update issue", map[string]any{"issue": key}); recErr != nil {
				opErr = &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  fmt.Sprintf("Updated %s, but the resume checkpoint could not be saved: %v", key, recErr),
					ExitCode: 1,
				}
				item["checkpoint_error"] = recErr.Error()
			} else {
				r := output.Receipt{OK: true, Message: fmt.Sprintf("Updated %s", key)}
				item["receipt"] = r.Format()
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
			remaining = append(remaining, idempotency.ResumeItem{
				ID:          key,
				Description: "retry this issue update",
				Target:      map[string]any{"issue": key},
			})
		} else {
			item["ok"] = true
			success++
			completed = append(completed, idempotency.ResumeItem{
				ID:          key,
				Description: "updated successfully",
				Target:      map[string]any{"issue": key},
			})
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(plan.Keys), key, status)

		if sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	if !dryRun && len(failures) == 0 {
		if recErr := idempotency.Record(idemKey, "jira.bulk-update"); recErr != nil {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Bulk update completed, but the completion marker could not be saved: %v", recErr),
				ExitCode: 1,
			}
		}
	}

	summary := map[string]any{
		"total":   len(plan.Keys),
		"ok":      success,
		"failed":  len(failures),
		"skipped": skipped,
		"dry_run": dryRun,
	}

	var resumable any
	if !dryRun && len(failures) > 0 {
		state := idempotency.NewResumeState("jira.bulk-update", idemKey, reqID, target, plan)
		state.Completed = completed
		state.Remaining = remaining
		resumable = state
	}

	if mode == "json" {
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, fmt.Sprintf("%s: %s", f.key, f.err), "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			len(failures) == 0, "jira", "bulk-update", target,
			map[string]any{
				"items":           items,
				"summary":         summary,
				"request_id":      reqID,
				"idempotency":     map[string]any{"key": idemKey},
				"resumable_state": resumable,
			},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Bulk update complete: %d succeeded, %d failed.\n", success, len(failures))
		if len(failures) > 0 {
			fmt.Printf("Resume with the same command and --idempotency-key %s.\n", idemKey)
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d failed\n", success, len(failures))
		printFailures(failures)
		if len(failures) > 0 {
			fmt.Printf("Resume with the same command and --idempotency-key %s.\n", idemKey)
		}
	}
	if len(failures) > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}

func resolveBulkUpdatePlan(cmd *cobra.Command, client *Client, jqlFlag, payloadFile string, pageSize, limit int, dryRun bool, requestedKey string) (bulkUpdatePlan, string, error) {
	if requestedKey != "" {
		var stored bulkUpdatePlan
		found, err := idempotency.LoadValue(requestedKey+".plan", &stored)
		if err != nil {
			return bulkUpdatePlan{}, "", err
		}
		if found {
			return stored, requestedKey, nil
		}
	}

	if strings.TrimSpace(jqlFlag) == "" {
		return bulkUpdatePlan{}, "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing --jql (or provide --idempotency-key for a saved resumable run).",
			ExitCode: 2,
		}
	}
	if strings.TrimSpace(payloadFile) == "" {
		return bulkUpdatePlan{}, "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing --payload (or provide --idempotency-key for a saved resumable run).",
			ExitCode: 2,
		}
	}

	payload, err := readJSONFile(payloadFile)
	if err != nil {
		return bulkUpdatePlan{}, "", err
	}
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	jql := applyDefaultScope(cmd, jqlFlag)
	keys, err := collectIssueKeys(client, jql, pageSize)
	if err != nil {
		return bulkUpdatePlan{}, "", err
	}
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	plan := bulkUpdatePlan{
		Version:     1,
		JQL:         jql,
		PayloadFile: payloadFile,
		Payload:     payload,
		Keys:        keys,
		Notify:      !noNotify,
	}

	idemKey := requestedKey
	if idemKey == "" {
		idemKey = output.IdempotencyKey("jira.bulk-update", plan)
	}

	var stored bulkUpdatePlan
	found, err := idempotency.LoadValue(idemKey+".plan", &stored)
	if err != nil {
		return bulkUpdatePlan{}, "", err
	}
	if found {
		return stored, idemKey, nil
	}
	if !dryRun {
		if err := idempotency.RecordValue(idemKey+".plan", "jira.bulk-update plan", plan); err != nil {
			return bulkUpdatePlan{}, "", err
		}
	}
	return plan, idemKey, nil
}
