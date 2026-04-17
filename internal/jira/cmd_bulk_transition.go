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

// NewBulkTransitionCmd creates the "bulk-transition" subcommand.
func NewBulkTransitionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bulk-transition",
		Short: "Transition multiple issues matched by JQL",
		Long:  "Transition each issue returned by a JQL search to a target status.",
		RunE:  runBulkTransition,
	}
	cmd.Flags().String("jql", "", "JQL query to select issues")
	cmd.Flags().String("to", "", "Target status name (case-insensitive)")
	cmd.Flags().String("payload", "", "JSON file with extra fields/update payload")
	cmd.Flags().Int("page-size", 100, "Search page size (default: 100)")
	cmd.Flags().Int("limit", 0, "Limit number of issues processed")
	cmd.Flags().Int("concurrency", 1, "Number of concurrent issue workers (default: 1, max: 10)")
	cmd.Flags().Float64("sleep", 0.0, "Delay between transitions in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	_ = cmd.MarkFlagRequired("jql")
	_ = cmd.MarkFlagRequired("to")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runBulkTransition(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	jqlFlag, _ := cmd.Flags().GetString("jql")
	toFlag, _ := cmd.Flags().GetString("to")
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
				true, "jira", "bulk-transition",
				map[string]any{"jql": jql, "to": toFlag},
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "Skipped bulk transition (idempotency key already used): %s\n", idemKey)
		return nil
	}

	var extraPayload map[string]any
	if payloadFile != "" {
		extraPayload, err = readJSONFile(payloadFile)
		if err != nil {
			return err
		}
	}
	undoGroupID := ""
	if !dryRun {
		undoGroupID = undo.NewGroupID("jira.bulk-transition")
	}
	fieldNames := payloadFieldNames(extraPayload)

	keys, err := collectIssueKeys(client, jql, pageSize)
	if err != nil {
		return err
	}
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	if len(keys) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-transition",
				map[string]any{"jql": jql, "to": toFlag},
				map[string]any{"items": []any{}, "summary": map[string]any{"total": 0, "ok": 0, "failed": 0, "dry_run": dryRun}},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Found 0 issues for JQL: %s\n", jql)
			return nil
		}
		fmt.Println("No issues found.")
		return nil
	}

	if dryRun && mode != "json" && !quiet && mode != "summary" {
		fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
	}

	targetStatus := strings.TrimSpace(strings.ToLower(toFlag))
	_ = targetStatus // used below via filterTransitionsByStatus

	success := 0
	skipped := 0
	var failures []failureEntry
	var items []map[string]any
	var warnings []any

	concurrency = cli.ClampConcurrency(concurrency)
	if concurrency > 1 {
		type bulkTransitionResult struct {
			item    map[string]any
			failure *failureEntry
			warns   []any
			skipped bool
			success bool
		}

		results := cli.RunParallel(len(keys), concurrency, func(idx int) bulkTransitionResult {
			key := keys[idx]
			item := map[string]any{"op": "transition", "target": map[string]any{"issue": key, "to": toFlag}, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("jira.bulk-transition.item", idemKey, idx, key, toFlag, extraPayload)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed transition for %s", key)}
					item["receipt"] = receipt.Format()
					return bulkTransitionResult{item: item, skipped: true}
				}
			}

			var opErr error
			var resultWarnings []any
			requestedFields := []string{"status"}
			requestedFields = append(requestedFields, fieldNames...)
			issue, e := client.GetIssue(key, strings.Join(requestedFields, ","), "")
			if e != nil {
				opErr = e
			}

			if opErr == nil {
				fd, _ := issue["fields"].(map[string]any)
				fromStatus := safeString(fd, "status", "name")
				undoFields := snapshotFieldValues(fd, fieldNames)

				data, e := client.ListTransitions(key)
				if e != nil {
					opErr = e
				} else {
					transitions, _ := data["transitions"].([]any)
					matches := filterTransitionsByStatus(transitions, toFlag)
					if len(matches) == 0 {
						opErr = &cerrors.CojiraError{
							Code:    cerrors.TransitionNotFound,
							Message: fmt.Sprintf("No transitions to status %q found", toFlag),
						}
					} else {
						if len(matches) > 1 {
							first := matches[0].(map[string]any)
							warnMsg := fmt.Sprintf("Multiple transitions match status '%s'; using first: %v", toFlag, first["id"])
							item["warning"] = warnMsg
							warnObj, _ := output.ErrorObj(cerrors.AmbiguousTransition, warnMsg, "", "", map[string]any{
								"action": "run", "command": fmt.Sprintf("cojira jira transitions %s --output-mode json", key),
								"requires_user": false,
							})
							resultWarnings = append(resultWarnings, warnObj)
						}

						first := matches[0].(map[string]any)
						transitionID := fmt.Sprintf("%v", first["id"])
						toName := safeString(first, "to", "name")
						if toName == "" {
							toName = toFlag
						}
						item["transition_id"] = transitionID
						item["from_status"] = fromStatus
						item["to_status"] = toName

						if dryRun {
							r := output.Receipt{
								OK: true, DryRun: true,
								Message: fmt.Sprintf("Would transition %s: %s -> %s (transition %s)", key, fromStatus, toName, transitionID),
							}
							item["dry_run"] = true
							item["receipt"] = r.Format()
							item["ok"] = true
						} else {
							payload := map[string]any{"transition": map[string]any{"id": transitionID}}
							if extraPayload != nil {
								for k, v := range extraPayload {
									payload[k] = v
								}
								payload["transition"] = map[string]any{"id": transitionID}
							}
							if e := client.TransitionIssue(key, payload, !noNotify); e != nil {
								opErr = e
							} else {
								issue2, e2 := client.GetIssue(key, "status", "")
								newStatus := toName
								if e2 == nil {
									fd2, _ := issue2["fields"].(map[string]any)
									if ns := safeString(fd2, "status", "name"); ns != "" {
										newStatus = ns
									}
								}
								r := output.Receipt{OK: true, Message: fmt.Sprintf("Transitioned %s: %s -> %s (transition %s)", key, fromStatus, newStatus, transitionID)}
								item["receipt"] = r.Format()
								item["ok"] = true
								item["to_status"] = newStatus
								recordUndoEntry(undoGroupID, key, "jira.bulk-transition", undoFields, fromStatus, newStatus)
								if childKey != "" {
									item["resume_key"] = childKey
									_ = idempotency.Record(childKey, fmt.Sprintf("jira.bulk-transition %s", key))
								}
							}
						}
					}
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
				return bulkTransitionResult{item: item, failure: &failureEntry{key: key, err: opErr.Error()}, warns: resultWarnings}
			}

			if item["ok"] == nil {
				item["ok"] = true
			}
			return bulkTransitionResult{item: item, success: true, warns: resultWarnings}
		})

		for idx, result := range results {
			items = append(items, result.item)
			warnings = append(warnings, result.warns...)
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
			output.EmitProgress(mode, quiet, idx+1, len(keys), fmt.Sprintf("%s -> %s", keys[idx], toFlag), status)
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
			item := map[string]any{"op": "transition", "target": map[string]any{"issue": key, "to": toFlag}, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("jira.bulk-transition.item", idemKey, idx, key, toFlag, extraPayload)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed transition for %s", key)}
					item["receipt"] = receipt.Format()
					items = append(items, item)
					skipped++
					output.EmitProgress(mode, quiet, idx+1, len(keys), fmt.Sprintf("%s -> %s", key, toFlag), "SKIPPED")
					continue
				}
			}
			var opErr error

			// Get current status.
			requestedFields := []string{"status"}
			requestedFields = append(requestedFields, fieldNames...)
			issue, e := client.GetIssue(key, strings.Join(requestedFields, ","), "")
			if e != nil {
				opErr = e
			}

			if opErr == nil {
				fd, _ := issue["fields"].(map[string]any)
				fromStatus := safeString(fd, "status", "name")
				undoFields := snapshotFieldValues(fd, fieldNames)

				// Find transition.
				data, e := client.ListTransitions(key)
				if e != nil {
					opErr = e
				} else {
					transitions, _ := data["transitions"].([]any)
					matches := filterTransitionsByStatus(transitions, toFlag)
					if len(matches) == 0 {
						opErr = &cerrors.CojiraError{
							Code:    cerrors.TransitionNotFound,
							Message: fmt.Sprintf("No transitions to status %q found", toFlag),
						}
					} else {
						if len(matches) > 1 {
							first := matches[0].(map[string]any)
							warnMsg := fmt.Sprintf("Multiple transitions match status '%s'; using first: %v", toFlag, first["id"])
							item["warning"] = warnMsg
							warnObj, _ := output.ErrorObj(cerrors.AmbiguousTransition, warnMsg, "", "", map[string]any{
								"action": "run", "command": fmt.Sprintf("cojira jira transitions %s --output-mode json", key),
								"requires_user": false,
							})
							warnings = append(warnings, warnObj)
						}

						first := matches[0].(map[string]any)
						transitionID := fmt.Sprintf("%v", first["id"])
						toName := safeString(first, "to", "name")
						if toName == "" {
							toName = toFlag
						}
						item["transition_id"] = transitionID
						item["from_status"] = fromStatus
						item["to_status"] = toName

						if dryRun {
							r := output.Receipt{
								OK: true, DryRun: true,
								Message: fmt.Sprintf("Would transition %s: %s -> %s (transition %s)", key, fromStatus, toName, transitionID),
							}
							item["dry_run"] = true
							item["receipt"] = r.Format()
							item["ok"] = true
							if mode != "json" && !quiet && mode != "summary" {
								fmt.Println(r.Format())
							}
							success++
						} else {
							payload := map[string]any{"transition": map[string]any{"id": transitionID}}
							if extraPayload != nil {
								for k, v := range extraPayload {
									payload[k] = v
								}
								payload["transition"] = map[string]any{"id": transitionID}
							}
							if e := client.TransitionIssue(key, payload, !noNotify); e != nil {
								opErr = e
							} else {
								issue2, e2 := client.GetIssue(key, "status", "")
								newStatus := toName
								if e2 == nil {
									fd2, _ := issue2["fields"].(map[string]any)
									if ns := safeString(fd2, "status", "name"); ns != "" {
										newStatus = ns
									}
								}
								r := output.Receipt{OK: true, Message: fmt.Sprintf("Transitioned %s: %s -> %s (transition %s)", key, fromStatus, newStatus, transitionID)}
								item["receipt"] = r.Format()
								item["ok"] = true
								item["to_status"] = newStatus
								recordUndoEntry(undoGroupID, key, "jira.bulk-transition", undoFields, fromStatus, newStatus)
								if mode != "json" && !quiet && mode != "summary" {
									fmt.Println(r.Format())
								}
								if childKey != "" {
									item["resume_key"] = childKey
									_ = idempotency.Record(childKey, fmt.Sprintf("jira.bulk-transition %s", key))
								}
								success++
							}
						}
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
			}

			items = append(items, item)
			status := "OK"
			if !item["ok"].(bool) {
				status = "FAILED"
			}
			output.EmitProgress(mode, quiet, idx+1, len(keys), fmt.Sprintf("%s -> %s", key, toFlag), status)

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
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.bulk-transition %s", toFlag))
	}

	if mode == "json" {
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, fmt.Sprintf("%s: %s", f.key, f.err), "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			len(failures) == 0, "jira", "bulk-transition",
			map[string]any{"jql": jql, "to": toFlag},
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			warnings, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Bulk transition complete: %d succeeded, %d skipped, %d failed.\n", success, skipped, len(failures))
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
