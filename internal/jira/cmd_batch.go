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

type jiraBatchPlan struct {
	Version    int               `json:"version"`
	Source     string            `json:"source,omitempty"`
	Operations []jiraBatchPlanOp `json:"operations"`
}

type jiraBatchPlanOp struct {
	ID          string         `json:"id"`
	Op          string         `json:"op"`
	Description string         `json:"description"`
	Target      map[string]any `json:"target,omitempty"`
	Payload     map[string]any `json:"payload,omitempty"`
	Notify      bool           `json:"notify,omitempty"`
	Capture     string         `json:"capture,omitempty"`
}

// NewBatchCmd creates the "batch" subcommand.
func NewBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch [config]",
		Short: "Run batch operations",
		Long:  "Execute a sequence of create/update/transition operations from a JSON config.",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runBatch,
	}
	cmd.Flags().Bool("stdin", false, "Read operations as newline-delimited JSON from stdin")
	cmd.Flags().Bool("dry-run", false, "Preview without changes")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("freeze-plan", false, "Persist the resolved batch plan during dry-run so it can be applied or resumed later")
	cmd.Flags().Float64("sleep", 0.0, "Delay between operations in seconds")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runBatch(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	useStdin, _ := cmd.Flags().GetBool("stdin")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	freezePlan, _ := cmd.Flags().GetBool("freeze-plan")
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")

	var configFile string
	if len(args) > 0 {
		configFile = args[0]
	}

	if useStdin && configFile != "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Cannot use both --stdin and a config file.", ExitCode: 2}
	}

	reqID := output.RequestID()
	plan, idemKey, target, err := resolveJiraBatchPlan(client, useStdin, configFile, dryRun, freezePlan, idemKey)
	if err != nil {
		return err
	}

	if len(plan.Operations) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "batch", target,
				map[string]any{
					"items":       []any{},
					"summary":     map[string]any{"total": 0, "ok": 0, "failed": 0, "skipped": 0, "dry_run": dryRun},
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Println("No operations found in config.")
		return nil
	}

	if !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "batch", target,
				map[string]any{
					"skipped":     true,
					"reason":      "idempotency_key_already_used",
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped batch (idempotency key already used): %s\n", idemKey)
		return nil
	}

	if mode != "json" && !quiet && mode != "summary" {
		fmt.Printf("Batch mode: %d operation(s)\n", len(plan.Operations))
		if dryRun {
			fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
		} else {
			fmt.Println()
		}
	}

	var items []map[string]any
	successCount := 0
	failureCount := 0
	skippedCount := 0
	var failures []failureEntry
	var completed []idempotency.ResumeItem
	var remaining []idempotency.ResumeItem
	vars := map[string]string{}

	for _, op := range plan.Operations {
		if op.Capture == "" {
			continue
		}
		var captured string
		found, err := idempotency.LoadValue(batchCaptureStoreKey(idemKey, op.ID, op.Capture), &captured)
		if err != nil {
			return err
		}
		if found && strings.TrimSpace(captured) != "" {
			vars[op.Capture] = captured
		}
	}

	for idx, op := range plan.Operations {
		item := map[string]any{
			"op":          op.Op,
			"ok":          false,
			"target":      op.Target,
			"description": op.Description,
		}
		opKey := fmt.Sprintf("%s.op.%s", idemKey, op.ID)
		if !dryRun && idempotency.IsDuplicate(opKey) {
			item["ok"] = true
			item["skipped"] = true
			item["reason"] = "idempotency_checkpoint_already_used"
			r := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped %s (already completed in a prior batch attempt)", op.Description)}
			item["receipt"] = r.Format()
			items = append(items, item)
			completed = append(completed, idempotency.ResumeItem{
				ID:          op.ID,
				Description: op.Description,
				Target:      op.Target,
			})
			skippedCount++
			output.EmitProgress(mode, quiet, idx+1, len(plan.Operations), op.Description, "SKIPPED")
			continue
		}

		var opErr error
		switch op.Op {
		case "update":
			rawIssueID, _ := op.Target["issue"].(string)
			issueID := ResolveIssueIdentifier(substituteVarsString(rawIssueID, vars))
			effectivePayload := substituteVars(op.Payload, vars)
			if dryRun {
				fieldKeys := make([]string, 0)
				if flds, ok := effectivePayload["fields"].(map[string]any); ok {
					for k := range flds {
						fieldKeys = append(fieldKeys, k)
					}
				}
				issue, e := client.GetIssue(issueID, joinComma(fieldKeys), "")
				if e != nil {
					opErr = e
				} else {
					item["diffs"] = previewPayloadDiff(issueID, issue, effectivePayload, mode == "json" || quiet)
				}
			} else if e := client.UpdateIssue(issueID, effectivePayload, op.Notify); e != nil {
				opErr = e
			}

		case "transition":
			rawIssueID, _ := op.Target["issue"].(string)
			issueID := ResolveIssueIdentifier(substituteVarsString(rawIssueID, vars))
			effectivePayload := substituteVars(op.Payload, vars)
			if !dryRun {
				if e := client.TransitionIssue(issueID, effectivePayload, op.Notify); e != nil {
					opErr = e
				}
			}

		case "create":
			effectivePayload := substituteVars(op.Payload, vars)
			if !dryRun {
				created, e := client.CreateIssue(effectivePayload, op.Notify)
				if e != nil {
					opErr = e
				} else {
					item["result"] = created
					if op.Capture != "" {
						capturedKey := strings.TrimSpace(fmt.Sprintf("%v", created["key"]))
						if capturedKey == "" {
							opErr = &cerrors.CojiraError{
								Code:     cerrors.CreateFailed,
								Message:  fmt.Sprintf("Create operation %s did not return a key to capture into %s.", op.Description, op.Capture),
								ExitCode: 1,
							}
						} else {
							vars[op.Capture] = capturedKey
							item["captured"] = map[string]any{"variable": op.Capture, "value": capturedKey}
							if recErr := idempotency.RecordKindValue(batchCaptureStoreKey(idemKey, op.ID, op.Capture), "capture", "jira.batch capture", capturedKey); recErr != nil {
								opErr = &cerrors.CojiraError{
									Code:     cerrors.OpFailed,
									Message:  fmt.Sprintf("%s succeeded, but the capture value could not be saved: %v", op.Description, recErr),
									ExitCode: 1,
								}
								item["capture_error"] = recErr.Error()
							}
						}
					}
				}
			} else {
				item["dry_run"] = true
				item["payload"] = effectivePayload
				if op.Capture != "" {
					item["capture"] = map[string]any{"variable": op.Capture}
				}
			}

		default:
			opErr = &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unknown operation: %s", op.Op), ExitCode: 1}
		}

		if opErr == nil && !dryRun {
			checkpointValue := map[string]any{"target": op.Target}
			if resultValue, ok := item["result"]; ok {
				checkpointValue["result"] = resultValue
			}
			if capturedValue, ok := item["captured"]; ok {
				checkpointValue["captured"] = capturedValue
			}
			if recErr := idempotency.RecordKindValue(opKey, "checkpoint", op.Description, checkpointValue); recErr != nil {
				opErr = &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  fmt.Sprintf("%s succeeded, but the resume checkpoint could not be saved: %v", op.Description, recErr),
					ExitCode: 1,
				}
				item["checkpoint_error"] = recErr.Error()
			}
		}

		if opErr != nil {
			item["ok"] = false
			item["error"] = opErr.Error()
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", op.Description, opErr)}
			item["receipt"] = r.Format()
			if mode != "json" && !quiet && mode != "summary" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), r.Format())
			}
			failureCount++
			failures = append(failures, failureEntry{key: op.Description, err: opErr.Error()})
			remaining = append(remaining, idempotency.ResumeItem{
				ID:          op.ID,
				Description: op.Description,
				Target:      op.Target,
			})
		} else {
			item["ok"] = true
			r := output.Receipt{OK: true, DryRun: dryRun, Message: op.Description}
			item["receipt"] = r.Format()
			if mode != "json" && !quiet && mode != "summary" {
				fmt.Println(r.Format())
			}
			successCount++
			completed = append(completed, idempotency.ResumeItem{
				ID:          op.ID,
				Description: op.Description,
				Target:      op.Target,
			})
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(plan.Operations), op.Description, status)

		if !dryRun && idx < len(plan.Operations)-1 && sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	if !dryRun && failureCount == 0 {
		if recErr := idempotency.Record(idemKey, "jira.batch"); recErr != nil {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Batch completed, but the completion marker could not be saved: %v", recErr),
				ExitCode: 1,
			}
		}
	}

	summary := map[string]any{
		"total":   len(plan.Operations),
		"ok":      successCount,
		"failed":  failureCount,
		"skipped": skippedCount,
		"dry_run": dryRun,
	}

	var resumable any
	if !dryRun && failureCount > 0 {
		state := idempotency.NewResumeState("jira.batch", idemKey, reqID, target, plan)
		state.Completed = completed
		state.Remaining = remaining
		resumable = state
	}

	if mode == "json" {
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, f.err, "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			failureCount == 0, "jira", "batch", target,
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
		fmt.Printf("Batch complete: %d succeeded, %d failed.\n", successCount, failureCount)
		if failureCount > 0 {
			fmt.Printf("Resume with the same command and --idempotency-key %s.\n", idemKey)
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d failed\n", successCount, failureCount)
		printFailures(failures)
		if failureCount > 0 {
			fmt.Printf("Resume with the same command and --idempotency-key %s.\n", idemKey)
		}
	}
	if failureCount > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}

func targetForBatchSource(source string) map[string]any {
	if source == "stdin" {
		return map[string]any{"stdin": true}
	}
	return map[string]any{"config": source}
}

func joinComma(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return strings.Join(ss, ",")
}
