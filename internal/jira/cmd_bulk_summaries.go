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
	cmd.Flags().Int("concurrency", 1, "Number of concurrent issue workers (default: 1, max: 10)")
	cmd.Flags().Float64("sleep", 0.0, "Delay between updates in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	_ = cmd.MarkFlagRequired("file")
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
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")

	reqID := output.RequestID()
	if idemKey != "" && !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-update-summaries",
				map[string]any{"file": fileFlag},
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Skipped bulk summary update (idempotency key already used): %s\n", idemKey)
		return nil
	}

	mappings, err := loadSummaryMap(fileFlag)
	if err != nil {
		return err
	}

	if limit > 0 && len(mappings) > limit {
		mappings = mappings[:limit]
	}

	if len(mappings) == 0 {
		if mode == "summary" {
			fmt.Println("No summaries to update.")
			return nil
		}
		fmt.Println("No summaries to update.")
		return nil
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
		type bulkSummaryResult struct {
			item    map[string]any
			failure *failureEntry
			skipped bool
			success bool
		}

		results := cli.RunParallel(len(mappings), concurrency, func(idx int) bulkSummaryResult {
			m := mappings[idx]
			payload := map[string]any{"fields": map[string]any{"summary": m.Summary}}
			item := map[string]any{"op": "update", "target": map[string]any{"issue": m.Key}, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("jira.bulk-update-summaries.item", idemKey, idx, m.Key, m.Summary)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed summary update for %s", m.Key)}
					item["receipt"] = receipt.Format()
					return bulkSummaryResult{item: item, skipped: true}
				}
			}

			var opErr error
			if dryRun {
				issue, e := client.GetIssue(m.Key, "summary", "")
				if e != nil {
					opErr = e
				} else {
					diffs := previewPayloadDiff(m.Key, issue, payload, true)
					item["diffs"] = diffs
				}
			} else {
				if e := client.UpdateIssue(m.Key, payload, !noNotify); e != nil {
					opErr = e
				} else {
					r := output.Receipt{OK: true, Message: fmt.Sprintf("Updated %s summary", m.Key)}
					item["receipt"] = r.Format()
				}
			}

			if !dryRun && sleepSec > 0 {
				time.Sleep(time.Duration(sleepSec * float64(time.Second)))
			}

			if opErr != nil {
				item["ok"] = false
				item["error"] = opErr.Error()
				r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", m.Key, opErr)}
				item["receipt"] = r.Format()
				return bulkSummaryResult{item: item, failure: &failureEntry{key: m.Key, err: opErr.Error()}}
			}

			item["ok"] = true
			if childKey != "" {
				item["resume_key"] = childKey
				_ = idempotency.Record(childKey, fmt.Sprintf("jira.bulk-update-summaries %s", m.Key))
			}
			return bulkSummaryResult{item: item, success: true}
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
			output.EmitProgress(mode, quiet, idx+1, len(mappings), fmt.Sprintf("%s summary", mappings[idx].Key), status)
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

		for idx, m := range mappings {
			payload := map[string]any{"fields": map[string]any{"summary": m.Summary}}
			item := map[string]any{"op": "update", "target": map[string]any{"issue": m.Key}, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("jira.bulk-update-summaries.item", idemKey, idx, m.Key, m.Summary)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed summary update for %s", m.Key)}
					item["receipt"] = receipt.Format()
					items = append(items, item)
					skipped++
					output.EmitProgress(mode, quiet, idx+1, len(mappings), fmt.Sprintf("%s summary", m.Key), "SKIPPED")
					continue
				}
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
				if e := client.UpdateIssue(m.Key, payload, !noNotify); e != nil {
					opErr = e
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
			} else {
				item["ok"] = true
				if childKey != "" {
					item["resume_key"] = childKey
					_ = idempotency.Record(childKey, fmt.Sprintf("jira.bulk-update-summaries %s", m.Key))
				}
				success++
			}

			items = append(items, item)
			status := "OK"
			if !item["ok"].(bool) {
				status = "FAILED"
			}
			output.EmitProgress(mode, quiet, idx+1, len(mappings), fmt.Sprintf("%s summary", m.Key), status)

			if !dryRun && sleepSec > 0 {
				time.Sleep(time.Duration(sleepSec * float64(time.Second)))
			}
		}
	}

	summary := map[string]any{
		"total":   len(mappings),
		"ok":      success,
		"skipped": skipped,
		"failed":  len(failures),
		"dry_run": dryRun,
	}
	if idemKey != "" && !dryRun && len(failures) == 0 {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.bulk-update-summaries %s", fileFlag))
	}

	if mode == "json" {
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, fmt.Sprintf("%s: %s", f.key, f.err), "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			len(failures) == 0, "jira", "bulk-update-summaries",
			map[string]any{"file": fileFlag},
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Bulk summary update complete: %d succeeded, %d skipped, %d failed.\n", success, skipped, len(failures))
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
