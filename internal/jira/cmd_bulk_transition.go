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

type bulkTransitionPlan struct {
	Version     int            `json:"version"`
	JQL         string         `json:"jql"`
	To          string         `json:"to"`
	PayloadFile string         `json:"payload_file,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	Keys        []string       `json:"keys"`
	Notify      bool           `json:"notify"`
}

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
	cmd.Flags().Float64("sleep", 0.0, "Delay between transitions in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
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
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	reqID := output.RequestID()

	plan, idemKey, err := resolveBulkTransitionPlan(cmd, client, jqlFlag, toFlag, payloadFile, pageSize, limit, dryRun, idemKey)
	if err != nil {
		return err
	}
	target := map[string]any{"jql": plan.JQL, "to": plan.To}

	if len(plan.Keys) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "bulk-transition", target,
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
				true, "jira", "bulk-transition", target,
				map[string]any{
					"skipped":     true,
					"reason":      "idempotency_key_already_used",
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped bulk transition (idempotency key already used): %s\n", idemKey)
		return nil
	}

	success := 0
	skipped := 0
	var failures []failureEntry
	var items []map[string]any
	var warnings []any
	var completed []idempotency.ResumeItem
	var remaining []idempotency.ResumeItem

	if dryRun && mode != "json" && !quiet && mode != "summary" {
		fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
	}

	for idx, key := range plan.Keys {
		item := map[string]any{"op": "transition", "target": map[string]any{"issue": key, "to": plan.To}, "ok": false}
		checkpointKey := fmt.Sprintf("%s.issue.%04d.%s", idemKey, idx, key)
		if !dryRun && idempotency.IsDuplicate(checkpointKey) {
			item["ok"] = true
			item["skipped"] = true
			item["reason"] = "idempotency_checkpoint_already_used"
			r := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped %s (already completed in a prior bulk transition attempt)", key)}
			item["receipt"] = r.Format()
			items = append(items, item)
			completed = append(completed, idempotency.ResumeItem{
				ID:          key,
				Description: "already completed in a prior attempt",
				Target:      map[string]any{"issue": key, "to": plan.To},
			})
			skipped++
			output.EmitProgress(mode, quiet, idx+1, len(plan.Keys), fmt.Sprintf("%s -> %s", key, plan.To), "SKIPPED")
			continue
		}

		var opErr error

		issue, e := client.GetIssue(key, "status", "")
		if e != nil {
			opErr = e
		}

		if opErr == nil {
			fd, _ := issue["fields"].(map[string]any)
			fromStatus := safeString(fd, "status", "name")

			data, e := client.ListTransitions(key)
			if e != nil {
				opErr = e
			} else {
				transitions, _ := data["transitions"].([]any)
				matches := filterTransitionsByStatus(transitions, plan.To)
				if len(matches) == 0 {
					opErr = &cerrors.CojiraError{
						Code:    cerrors.TransitionNotFound,
						Message: fmt.Sprintf("No transitions to status %q found", plan.To),
					}
				} else {
					if len(matches) > 1 {
						first := matches[0].(map[string]any)
						warnMsg := fmt.Sprintf("Multiple transitions match status '%s'; using first: %v", plan.To, first["id"])
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
						toName = plan.To
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
						completed = append(completed, idempotency.ResumeItem{
							ID:          key,
							Description: "dry-run preview generated",
							Target:      map[string]any{"issue": key, "to": plan.To},
						})
					} else {
						payload := map[string]any{"transition": map[string]any{"id": transitionID}}
						for k, v := range plan.Payload {
							payload[k] = v
						}
						payload["transition"] = map[string]any{"id": transitionID}
						if e := client.TransitionIssue(key, payload, plan.Notify); e != nil {
							opErr = e
						} else {
							issue2, e2 := client.GetIssue(key, "status", "")
							if e2 != nil {
								opErr = &cerrors.CojiraError{
									Code:     cerrors.TransitionFailed,
									Message:  fmt.Sprintf("Transition submitted for %s but the new status could not be verified: %v", key, e2),
									ExitCode: 1,
								}
							} else {
								fd2, _ := issue2["fields"].(map[string]any)
								newStatus := safeString(fd2, "status", "name")
								if newStatus == "" {
									opErr = &cerrors.CojiraError{
										Code:     cerrors.TransitionFailed,
										Message:  fmt.Sprintf("Transition submitted for %s but the issue returned no status during verification.", key),
										ExitCode: 1,
									}
								} else if !strings.EqualFold(newStatus, toName) && !strings.EqualFold(newStatus, plan.To) {
									opErr = &cerrors.CojiraError{
										Code:     cerrors.TransitionFailed,
										Message:  fmt.Sprintf("Transitioned %s using %s, but Jira still reports status %q instead of %q.", key, transitionID, newStatus, toName),
										ExitCode: 1,
									}
								} else if recErr := idempotency.RecordValue(checkpointKey, "jira.bulk-transition issue", map[string]any{"issue": key, "to": newStatus}); recErr != nil {
									opErr = &cerrors.CojiraError{
										Code:     cerrors.OpFailed,
										Message:  fmt.Sprintf("Transitioned %s, but the resume checkpoint could not be saved: %v", key, recErr),
										ExitCode: 1,
									}
									item["checkpoint_error"] = recErr.Error()
								} else {
									r := output.Receipt{OK: true, Message: fmt.Sprintf("Transitioned %s: %s -> %s (transition %s)", key, fromStatus, newStatus, transitionID)}
									item["receipt"] = r.Format()
									item["ok"] = true
									item["to_status"] = newStatus
									if mode != "json" && !quiet && mode != "summary" {
										fmt.Println(r.Format())
									}
									success++
									completed = append(completed, idempotency.ResumeItem{
										ID:          key,
										Description: "transitioned successfully",
										Target:      map[string]any{"issue": key, "to": newStatus},
									})
								}
							}
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
			remaining = append(remaining, idempotency.ResumeItem{
				ID:          key,
				Description: "retry this issue transition",
				Target:      map[string]any{"issue": key, "to": plan.To},
			})
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(plan.Keys), fmt.Sprintf("%s -> %s", key, plan.To), status)

		if sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	if !dryRun && len(failures) == 0 {
		if recErr := idempotency.Record(idemKey, "jira.bulk-transition"); recErr != nil {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Bulk transition completed, but the completion marker could not be saved: %v", recErr),
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
		state := idempotency.NewResumeState("jira.bulk-transition", idemKey, reqID, target, plan)
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
			len(failures) == 0, "jira", "bulk-transition", target,
			map[string]any{
				"items":           items,
				"summary":         summary,
				"request_id":      reqID,
				"idempotency":     map[string]any{"key": idemKey},
				"resumable_state": resumable,
			},
			warnings, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Bulk transition complete: %d succeeded, %d failed.\n", success, len(failures))
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

func resolveBulkTransitionPlan(cmd *cobra.Command, client *Client, jqlFlag, toFlag, payloadFile string, pageSize, limit int, dryRun bool, requestedKey string) (bulkTransitionPlan, string, error) {
	if requestedKey != "" {
		var stored bulkTransitionPlan
		found, err := idempotency.LoadValue(requestedKey+".plan", &stored)
		if err != nil {
			return bulkTransitionPlan{}, "", err
		}
		if found {
			return stored, requestedKey, nil
		}
	}

	if strings.TrimSpace(jqlFlag) == "" {
		return bulkTransitionPlan{}, "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing --jql (or provide --idempotency-key for a saved resumable run).",
			ExitCode: 2,
		}
	}
	if strings.TrimSpace(toFlag) == "" {
		return bulkTransitionPlan{}, "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Missing --to (or provide --idempotency-key for a saved resumable run).",
			ExitCode: 2,
		}
	}

	var payload map[string]any
	var err error
	if payloadFile != "" {
		payload, err = readJSONFile(payloadFile)
		if err != nil {
			return bulkTransitionPlan{}, "", err
		}
	}
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	jql := applyDefaultScope(cmd, jqlFlag)
	keys, err := collectIssueKeys(client, jql, pageSize)
	if err != nil {
		return bulkTransitionPlan{}, "", err
	}
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	plan := bulkTransitionPlan{
		Version:     1,
		JQL:         jql,
		To:          toFlag,
		PayloadFile: payloadFile,
		Payload:     payload,
		Keys:        keys,
		Notify:      !noNotify,
	}

	idemKey := requestedKey
	if idemKey == "" {
		idemKey = output.IdempotencyKey("jira.bulk-transition", plan)
	}

	var stored bulkTransitionPlan
	found, err := idempotency.LoadValue(idemKey+".plan", &stored)
	if err != nil {
		return bulkTransitionPlan{}, "", err
	}
	if found {
		return stored, idemKey, nil
	}
	if !dryRun {
		if err := idempotency.RecordValue(idemKey+".plan", "jira.bulk-transition plan", plan); err != nil {
			return bulkTransitionPlan{}, "", err
		}
	}
	return plan, idemKey, nil
}
