package jira

import (
	"fmt"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

type bulkSummaryPlan struct {
	Version  int              `json:"version"`
	File     string           `json:"file,omitempty"`
	Mappings []summaryMapping `json:"mappings"`
	Notify   bool             `json:"notify"`
}

// NewBulkUpdateSummariesCmd creates the "bulk-update-summaries" subcommand.
func NewBulkUpdateSummariesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-update-summaries",
		Short: "Bulk update summaries from CSV/JSON mapping",
		Long:  "Update issue summaries using a key->summary mapping file.",
		RunE:  runBulkUpdateSummaries,
	}
	cmd.Flags().String("file", "", "CSV/JSON file with key/summary mapping")
	cmd.Flags().Int("limit", 0, "Limit number of issues processed")
	cmd.Flags().Float64("sleep", 0.0, "Delay between updates in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runBulkUpdateSummaries(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	fileFlag, _ := cmd.Flags().GetString("file")
	limit, _ := cmd.Flags().GetInt("limit")
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	reqID := output.RequestID()

	plan, idemKey, err := resolveBulkSummaryPlan(cmd, fileFlag, limit, dryRun, idemKey)
	if err != nil {
		return err
	}
	target := map[string]any{"file": plan.File}

	if len(plan.Mappings) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-update-summaries", target,
				map[string]any{
					"items":       []any{},
					"summary":     map[string]any{"total": 0, "ok": 0, "failed": 0, "skipped": 0, "dry_run": dryRun},
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Println("No summaries to update.")
		return nil
	}

	if !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-update-summaries", target,
				map[string]any{
					"skipped":     true,
					"reason":      "idempotency_key_already_used",
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped bulk summary update (idempotency key already used): %s\n", idemKey)
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

	for idx, m := range plan.Mappings {
		payload := map[string]any{"fields": map[string]any{"summary": m.Summary}}
		item := map[string]any{"op": "update", "target": map[string]any{"issue": m.Key}, "ok": false}
		checkpointKey := fmt.Sprintf("%s.issue.%04d.%s", idemKey, idx, m.Key)
		if !dryRun && idempotency.IsDuplicate(checkpointKey) {
			item["ok"] = true
			item["skipped"] = true
			item["reason"] = "idempotency_checkpoint_already_used"
			r := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped %s summary (already completed in a prior attempt)", m.Key)}
			item["receipt"] = r.Format()
			items = append(items, item)
			completed = append(completed, idempotency.ResumeItem{
				ID:          m.Key,
				Description: "already completed in a prior attempt",
				Target:      map[string]any{"issue": m.Key, "summary": m.Summary},
			})
			skipped++
			output.EmitProgress(mode, quiet, idx+1, len(plan.Mappings), fmt.Sprintf("%s summary", m.Key), "SKIPPED")
			continue
		}

		var opErr error
		if dryRun {
			issue, e := client.GetIssue(m.Key, "summary", "")
			if e != nil {
				opErr = e
			} else {
				diffs := previewPayloadDiff(m.Key, issue, payload, mode == "json" || quiet)
				item["diffs"] = diffs
			}
		} else {
			if e := client.UpdateIssue(m.Key, payload, plan.Notify); e != nil {
				opErr = e
			} else if recErr := idempotency.RecordValue(checkpointKey, "jira.bulk-update-summaries issue", map[string]any{"issue": m.Key, "summary": m.Summary}); recErr != nil {
				opErr = &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  fmt.Sprintf("Updated %s summary, but the resume checkpoint could not be saved: %v", m.Key, recErr),
					ExitCode: 1,
				}
				item["checkpoint_error"] = recErr.Error()
			} else {
				r := output.Receipt{OK: true, Message: fmt.Sprintf("Updated %s summary", m.Key)}
				item["receipt"] = r.Format()
				if mode != "json" && !quiet && mode != "summary" {
					fmt.Println(r.Format())
				}
			}
		}

		if opErr != nil {
			item["ok"] = false
			item["error"] = opErr.Error()
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", m.Key, opErr)}
			item["receipt"] = r.Format()
			if mode != "json" && !quiet && mode != "summary" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), r.Format())
			}
			failures = append(failures, failureEntry{key: m.Key, err: opErr.Error()})
			remaining = append(remaining, idempotency.ResumeItem{
				ID:          m.Key,
				Description: "retry this summary update",
				Target:      map[string]any{"issue": m.Key, "summary": m.Summary},
			})
		} else {
			item["ok"] = true
			success++
			completed = append(completed, idempotency.ResumeItem{
				ID:          m.Key,
				Description: "summary updated successfully",
				Target:      map[string]any{"issue": m.Key, "summary": m.Summary},
			})
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(plan.Mappings), fmt.Sprintf("%s summary", m.Key), status)

		if sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	if !dryRun && len(failures) == 0 {
		if recErr := idempotency.Record(idemKey, "jira.bulk-update-summaries"); recErr != nil {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Bulk summary update completed, but the completion marker could not be saved: %v", recErr),
				ExitCode: 1,
			}
		}
	}

	summary := map[string]any{
		"total":   len(plan.Mappings),
		"ok":      success,
		"failed":  len(failures),
		"skipped": skipped,
		"dry_run": dryRun,
	}

	var resumable any
	if !dryRun && len(failures) > 0 {
		state := idempotency.NewResumeState("jira.bulk-update-summaries", idemKey, reqID, target, plan)
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
			len(failures) == 0, "jira", "bulk-update-summaries", target,
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
		fmt.Printf("Bulk summary update complete: %d succeeded, %d failed.\n", success, len(failures))
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

func resolveBulkSummaryPlan(cmd *cobra.Command, fileFlag string, limit int, dryRun bool, requestedKey string) (bulkSummaryPlan, string, error) {
	if requestedKey != "" {
		var stored bulkSummaryPlan
		found, err := idempotency.LoadValue(requestedKey+".plan", &stored)
		if err != nil {
			return bulkSummaryPlan{}, "", err
		}
		if found {
			return stored, requestedKey, nil
		}
	}

	if fileFlag == "" {
		return bulkSummaryPlan{}, "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing --file (or provide --idempotency-key for a saved resumable run).",
			ExitCode: 2,
		}
	}

	mappings, err := loadSummaryMap(fileFlag)
	if err != nil {
		return bulkSummaryPlan{}, "", err
	}
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	if limit > 0 && len(mappings) > limit {
		mappings = mappings[:limit]
	}

	plan := bulkSummaryPlan{
		Version:  1,
		File:     fileFlag,
		Mappings: mappings,
		Notify:   !noNotify,
	}

	idemKey := requestedKey
	if idemKey == "" {
		idemKey = output.IdempotencyKey("jira.bulk-update-summaries", plan)
	}

	var stored bulkSummaryPlan
	found, err := idempotency.LoadValue(idemKey+".plan", &stored)
	if err != nil {
		return bulkSummaryPlan{}, "", err
	}
	if found {
		return stored, idemKey, nil
	}
	if !dryRun {
		if err := idempotency.RecordValue(idemKey+".plan", "jira.bulk-update-summaries plan", plan); err != nil {
			return bulkSummaryPlan{}, "", err
		}
	}
	return plan, idemKey, nil
}
