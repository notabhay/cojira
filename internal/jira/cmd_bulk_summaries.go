package jira

import (
	"fmt"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
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
	cmd.Flags().Float64("sleep", 0.0, "Delay between updates in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	_ = cmd.MarkFlagRequired("file")
	cli.AddOutputFlags(cmd, true)
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
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")

	reqID := output.RequestID()

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
	var failures []failureEntry
	var items []map[string]any

	if dryRun && mode != "json" && !quiet && mode != "summary" {
		fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
	}

	for idx, m := range mappings {
		payload := map[string]any{"fields": map[string]any{"summary": m.Summary}}
		item := map[string]any{"op": "update", "target": map[string]any{"issue": m.Key}, "ok": false}
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
			success++
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(mappings), fmt.Sprintf("%s summary", m.Key), status)

		if sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	summary := map[string]any{
		"total":   len(mappings),
		"ok":      success,
		"failed":  len(failures),
		"dry_run": dryRun,
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
		fmt.Printf("Bulk summary update complete: %d succeeded, %d failed.\n", success, len(failures))
		if len(failures) > 0 {
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d failed\n", success, len(failures))
		printFailures(failures)
	}
	if len(failures) > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}
