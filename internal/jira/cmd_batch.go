package jira

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/idempotency"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

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

	var operations []map[string]any
	var basePath string

	if useStdin {
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			line := scanner.Text()
			if line == "" {
				continue
			}
			var op map[string]any
			if err := json.Unmarshal([]byte(line), &op); err != nil {
				return &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: fmt.Sprintf("Invalid JSON on stdin: %v", err), ExitCode: 1}
			}
			operations = append(operations, op)
		}
		basePath, _ = os.Getwd()
	} else if configFile != "" {
		data, err := os.ReadFile(configFile)
		if err != nil {
			return &cerrors.CojiraError{Code: cerrors.FileNotFound, Message: fmt.Sprintf("Config file not found: %s", configFile), ExitCode: 1}
		}
		var config map[string]any
		if err := json.Unmarshal(data, &config); err != nil {
			return &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: fmt.Sprintf("Invalid JSON in %s: %v", configFile, err), ExitCode: 1}
		}
		if ops, ok := config["operations"].([]any); ok {
			for _, o := range ops {
				if m, ok := o.(map[string]any); ok {
					operations = append(operations, m)
				}
			}
		}
		if bd, ok := config["base_dir"].(string); ok && bd != "" {
			if filepath.IsAbs(bd) {
				basePath = bd
			} else {
				basePath = filepath.Join(filepath.Dir(configFile), bd)
			}
		} else {
			basePath = filepath.Dir(configFile)
		}
	} else {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide a config file or --stdin.", ExitCode: 2}
	}

	if len(operations) == 0 {
		fmt.Println("No operations found in config.")
		return nil
	}

	reqID := output.RequestID()

	if idemKey != "" && !dryRun {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				target := map[string]any{}
				if configFile != "" {
					target["config"] = configFile
				} else {
					target["stdin"] = true
				}
				return output.PrintJSON(output.BuildEnvelope(
					true, "jira", "batch", target,
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped batch (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	var items []map[string]any
	successCount := 0
	failureCount := 0
	var failures []failureEntry

	if mode != "json" && !quiet && mode != "summary" {
		fmt.Printf("Batch mode: %d operation(s)\n", len(operations))
		if dryRun {
			fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
		} else {
			fmt.Println()
		}
	}

	for idx, op := range operations {
		opType, _ := op["op"].(string)
		desc := ""
		item := map[string]any{"op": opType, "ok": false}

		var opErr error
		switch opType {
		case "update":
			issueVal, _ := op["issue"].(string)
			issueID := ResolveIssueIdentifier(issueVal)
			fileVal, _ := op["file"].(string)
			filePath := filepath.Join(basePath, fileVal)
			desc = fmt.Sprintf("update %s from %s", issueID, fileVal)
			payload, e := readJSONFile(filePath)
			if e != nil {
				opErr = e
				break
			}
			if dryRun {
				fieldKeys := make([]string, 0)
				if flds, ok := payload["fields"].(map[string]any); ok {
					for k := range flds {
						fieldKeys = append(fieldKeys, k)
					}
				}
				issue, e := client.GetIssue(issueID, joinComma(fieldKeys), "")
				if e != nil {
					opErr = e
					break
				}
				diffs := previewPayloadDiff(issueID, issue, payload, mode == "json" || quiet)
				item["target"] = map[string]any{"issue": issueID}
				item["diffs"] = diffs
			} else {
				notify := true
				if n, ok := op["notify"].(bool); ok {
					notify = n
				}
				if e := client.UpdateIssue(issueID, payload, notify); e != nil {
					opErr = e
					break
				}
				item["target"] = map[string]any{"issue": issueID}
			}

		case "transition":
			issueVal, _ := op["issue"].(string)
			issueID := ResolveIssueIdentifier(issueVal)
			transition := op["transition"]
			fileVal, _ := op["file"].(string)
			desc = fmt.Sprintf("transition %s using %v", issueID, transition)
			payload := map[string]any{"transition": map[string]any{"id": fmt.Sprintf("%v", transition)}}
			if fileVal != "" {
				extra, e := readJSONFile(filepath.Join(basePath, fileVal))
				if e != nil {
					opErr = e
					break
				}
				for k, v := range extra {
					payload[k] = v
				}
				payload["transition"] = map[string]any{"id": fmt.Sprintf("%v", transition)}
			}
			item["target"] = map[string]any{"issue": issueID, "transition": fmt.Sprintf("%v", transition)}
			if !dryRun {
				notify := true
				if n, ok := op["notify"].(bool); ok {
					notify = n
				}
				if e := client.TransitionIssue(issueID, payload, notify); e != nil {
					opErr = e
					break
				}
			}

		case "create":
			fileVal, _ := op["file"].(string)
			desc = fmt.Sprintf("create issue from %s", fileVal)
			if !dryRun {
				payload, e := readJSONFile(filepath.Join(basePath, fileVal))
				if e != nil {
					opErr = e
					break
				}
				created, e := client.CreateIssue(payload)
				if e != nil {
					opErr = e
					break
				}
				item["result"] = created
			}

		default:
			opErr = &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unknown operation: %s", opType), ExitCode: 1}
		}

		if opErr != nil {
			item["ok"] = false
			item["error"] = opErr.Error()
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", stringOr(desc, opType), opErr)}
			item["receipt"] = r.Format()
			if mode != "json" && !quiet && mode != "summary" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), r.Format())
			}
			failureCount++
			failures = append(failures, failureEntry{key: stringOr(desc, opType), err: opErr.Error()})
		} else {
			item["ok"] = true
			r := output.Receipt{OK: true, DryRun: dryRun, Message: desc}
			item["receipt"] = r.Format()
			if mode != "json" && !quiet && mode != "summary" {
				fmt.Println(r.Format())
			}
			successCount++
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(operations), stringOr(desc, opType), status)

		if !dryRun && idx < len(operations)-1 && sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	summary := map[string]any{
		"total":   len(operations),
		"ok":      successCount,
		"failed":  failureCount,
		"dry_run": dryRun,
	}

	if idemKey != "" && !dryRun && failureCount == 0 {
		src := configFile
		if src == "" {
			src = "stdin"
		}
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.batch %s", src))
	}

	if mode == "json" {
		target := map[string]any{}
		if configFile != "" {
			target["config"] = configFile
		} else {
			target["stdin"] = true
		}
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, f.err, "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			failureCount == 0, "jira", "batch", target,
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Batch complete: %d succeeded, %d failed.\n", successCount, failureCount)
		if failureCount > 0 {
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d failed\n", successCount, failureCount)
		printFailures(failures)
	}
	if failureCount > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}

func joinComma(ss []string) string {
	if len(ss) == 0 {
		return ""
	}
	return strings.Join(ss, ",")
}

// strings import is needed above; added via the existing import block
