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
	_ = cmd.MarkFlagRequired("jql")
	_ = cmd.MarkFlagRequired("to")
	cli.AddOutputFlags(cmd, true)
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
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")

	jql := applyDefaultScope(cmd, jqlFlag)
	reqID := output.RequestID()

	var extraPayload map[string]any
	if payloadFile != "" {
		extraPayload, err = readJSONFile(payloadFile)
		if err != nil {
			return err
		}
	}

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
	var failures []failureEntry
	var items []map[string]any
	var warnings []any

	for idx, key := range keys {
		item := map[string]any{"op": "transition", "target": map[string]any{"issue": key, "to": toFlag}, "ok": false}
		var opErr error

		// Get current status.
		issue, e := client.GetIssue(key, "status", "")
		if e != nil {
			opErr = e
		}

		if opErr == nil {
			fd, _ := issue["fields"].(map[string]any)
			fromStatus := safeString(fd, "status", "name")

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
							if mode != "json" && !quiet && mode != "summary" {
								fmt.Println(r.Format())
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
				fmt.Fprintln(cmd.ErrOrStderr(), r.Format())
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
			len(failures) == 0, "jira", "bulk-transition",
			map[string]any{"jql": jql, "to": toFlag},
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			warnings, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Bulk transition complete: %d succeeded, %d failed.\n", success, len(failures))
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
