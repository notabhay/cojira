package jira

import (
	"fmt"
	"strings"
	"time"

	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/output"
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
	cmd.Flags().Float64("sleep", 0.0, "Delay between updates in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	_ = cmd.MarkFlagRequired("jql")
	_ = cmd.MarkFlagRequired("payload")
	cli.AddOutputFlags(cmd, true)
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
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")

	jql := applyDefaultScope(cmd, jqlFlag)
	reqID := output.RequestID()

	payload, err := readJSONFile(payloadFile)
	if err != nil {
		return err
	}

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

	success := 0
	var failures []failureEntry
	var items []map[string]any

	if dryRun && mode != "json" && !quiet && mode != "summary" {
		fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
	}

	for idx, key := range keys {
		item := map[string]any{"op": "update", "target": map[string]any{"issue": key}, "ok": false}
		var opErr error

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
		} else {
			if e := client.UpdateIssue(key, payload, !noNotify); e != nil {
				opErr = e
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
		} else {
			item["ok"] = true
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

	summary := map[string]any{
		"total":   len(keys),
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
			len(failures) == 0, "jira", "bulk-update",
			map[string]any{"jql": jql, "payload": payloadFile},
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Bulk update complete: %d succeeded, %d failed.\n", success, len(failures))
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
