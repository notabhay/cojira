package confluence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

const batchDelayDefault = 1.5

// NewBatchCmd creates the "batch" subcommand.
func NewBatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "batch [config]",
		Short: "Run batch operations from a config file",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runBatch,
	}
	cmd.Flags().Bool("stdin", false, "Read operations as newline-delimited JSON from stdin")
	cmd.Flags().Bool("dry-run", false, "Preview without changes")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Float64("sleep", batchDelayDefault, "Delay between operations in seconds")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
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
	sleepDelay, _ := cmd.Flags().GetFloat64("sleep")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")

	var configFile string
	if len(args) > 0 {
		configFile = args[0]
	}

	reqID := output.RequestID()

	if useStdin && configFile != "" {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Cannot use both --stdin and a config file.",
			ExitCode: 2,
		}
	}

	var config map[string]any
	var rootDir string

	if useStdin {
		// Read operations from stdin (newline-delimited JSON).
		var operations []any
		decoder := json.NewDecoder(os.Stdin)
		for decoder.More() {
			var op any
			if decErr := decoder.Decode(&op); decErr != nil {
				return &cerrors.CojiraError{
					Code:     cerrors.InvalidJSON,
					Message:  fmt.Sprintf("Invalid JSON on stdin: %v", decErr),
					ExitCode: 1,
				}
			}
			operations = append(operations, op)
		}
		config = map[string]any{"operations": operations}
		cwd, _ := os.Getwd()
		rootDir = cwd
	} else if configFile != "" {
		configData, readErr := readJSONFile(configFile)
		if readErr != nil {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.FileNotFound, fmt.Sprintf("Config file not found: %s", configFile), "", "", nil)
				return output.PrintJSON(output.BuildEnvelope(
					false, "confluence", "batch",
					map[string]any{"config": configFile},
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Error reading config: %v\n", readErr)
			return readErr
		}
		config = configData
		rootDir = filepath.Dir(configFile)
	} else {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Provide a config file or --stdin.",
			ExitCode: 2,
		}
	}

	operations, _ := config["operations"].([]any)
	if len(operations) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "batch",
				map[string]any{"config": configFile},
				map[string]any{
					"items":   []any{},
					"summary": map[string]any{"total": 0, "ok": 0, "failed": 0, "dry_run": dryRun},
				},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Println("No operations found in config.")
		return nil
	}

	// Idempotency check.
	if idemKey != "" && !dryRun {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				target := map[string]any{"config": configFile}
				if useStdin {
					target = map[string]any{"stdin": true}
				}
				return output.PrintJSON(output.BuildEnvelope(
					true, "confluence", "batch", target,
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Skipped batch (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	baseDir, _ := config["base_dir"].(string)
	basePath := rootDir
	if baseDir != "" {
		if filepath.IsAbs(baseDir) {
			basePath = baseDir
		} else {
			basePath = filepath.Join(rootDir, baseDir)
		}
	}

	if mode != "json" && !quiet && mode != "summary" {
		fmt.Printf("Batch mode: %d operation(s)\n", len(operations))
		if dryRun {
			fmt.Println("[DRY-RUN MODE - no changes will be made]")
			fmt.Println()
		} else {
			fmt.Println()
		}
	}

	var items []map[string]any
	successCount := 0
	failureCount := 0
	skippedCount := 0
	var failures []string
	var warningObjs []any

	for idx, opAny := range operations {
		op, ok := opAny.(map[string]any)
		if !ok {
			continue
		}
		opType, _ := op["op"].(string)
		opType = strings.ToLower(opType)
		desc := ""
		item := map[string]any{"op": opType, "ok": false}
		opKey := ""
		if idemKey != "" && !dryRun {
			opKey = fmt.Sprintf("%s.op.%04d", idemKey, idx)
			if idempotency.IsDuplicate(opKey) {
				item["ok"] = true
				item["skipped"] = true
				item["reason"] = "idempotency_checkpoint_already_used"
				receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped %s (already completed in a prior batch attempt)", stringOr(desc, opType))}
				item["receipt"] = receipt.Format()
				items = append(items, item)
				skippedCount++
				output.EmitProgress(mode, quiet, idx+1, len(operations), stringOr(desc, opType), "SKIPPED")
				continue
			}
		}

		opErr := executeBatchOp(client, op, opType, basePath, dryRun, &desc, item)
		if opErr != nil {
			item["ok"] = false
			item["error"] = opErr.Error()
			receipt := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", desc, opErr)}
			item["receipt"] = receipt.Format()
			if mode != "json" && !quiet && mode != "summary" {
				fmt.Fprintln(os.Stderr, receipt.Format())
			}
			failureCount++
			failures = append(failures, opErr.Error())
		} else {
			item["ok"] = true
			receipt := output.Receipt{OK: true, DryRun: dryRun, Message: desc}
			item["receipt"] = receipt.Format()
			if mode != "json" && !quiet && mode != "summary" {
				fmt.Println(receipt.Format())
			}
			if opKey != "" {
				if recErr := idempotency.Record(opKey, desc); recErr != nil {
					warnMsg := fmt.Sprintf("%s: operation succeeded, but the idempotency checkpoint could not be saved: %v", desc, recErr)
					item["idempotency_warning"] = warnMsg
					warningObjs = append(warningObjs, warnMsg)
					if mode != "json" && !quiet && mode != "summary" {
						_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", warnMsg)
					}
				}
			}
			successCount++
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(operations), desc, status)

		// Throttle between operations.
		if !dryRun && idx < len(operations)-1 && sleepDelay > 0 {
			time.Sleep(time.Duration(sleepDelay * float64(time.Second)))
		}
	}

	summary := map[string]any{
		"total":   len(operations),
		"ok":      successCount,
		"failed":  failureCount,
		"skipped": skippedCount,
		"dry_run": dryRun,
	}

	if idemKey != "" && !dryRun && failureCount == 0 {
		if recErr := idempotency.Record(idemKey, fmt.Sprintf("confluence.batch %s", configFile)); recErr != nil {
			warnMsg := fmt.Sprintf("Batch completed, but the idempotency completion marker could not be saved: %v", recErr)
			warningObjs = append(warningObjs, warnMsg)
			if mode != "json" && !quiet && mode != "summary" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", warnMsg)
			}
		}
	}

	if mode == "json" {
		target := map[string]any{"config": configFile}
		if useStdin {
			target = map[string]any{"stdin": true}
		}
		var errObjs []any
		for _, msg := range failures {
			obj, _ := output.ErrorObj(cerrors.OpFailed, msg, "", "", nil)
			errObjs = append(errObjs, obj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			failureCount == 0, "confluence", "batch", target,
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			warningObjs, errObjs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Batch complete: %d succeeded, %d failed.\n", successCount, failureCount)
		if failureCount > 0 {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch had failures", ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d failed\n", successCount, failureCount)
	}
	if failureCount > 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch had failures", ExitCode: 1}
	}
	return nil
}

func executeBatchOp(client *Client, op map[string]any, opType, basePath string, dryRun bool, desc *string, item map[string]any) error {
	switch opType {
	case "move":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}

		parentVal := op["parent"]
		var parentID string
		if parentVal == nil || parentVal == float64(0) || fmt.Sprintf("%v", parentVal) == "0" || fmt.Sprintf("%v", parentVal) == "root" {
			*desc = fmt.Sprintf("move %s -> root", pageID)
		} else {
			parentID, err = ResolvePageID(client, fmt.Sprintf("%v", parentVal), "")
			if err != nil {
				return err
			}
			*desc = fmt.Sprintf("move %s -> %s", pageID, parentID)
		}
		item["target"] = map[string]any{"page_id": pageID, "parent_id": parentID}

		if !dryRun {
			page, fetchErr := client.GetPageByID(pageID, "version,body.storage")
			if fetchErr != nil {
				return fetchErr
			}
			body := getNestedString(page, "body", "storage", "value")
			version := int(getNestedFloat(page, "version", "number"))
			title, _ := page["title"].(string)

			ancestors := []map[string]any{}
			if parentID != "" {
				ancestors = []map[string]any{{"id": parentID}}
			}
			payload := map[string]any{
				"id":        pageID,
				"type":      "page",
				"title":     title,
				"version":   map[string]any{"number": version + 1},
				"ancestors": ancestors,
				"body": map[string]any{
					"storage": map[string]any{
						"value":          body,
						"representation": "storage",
					},
				},
			}
			_, err = client.UpdatePage(pageID, payload)
			if err != nil {
				return err
			}
		}
		return nil

	case "rename":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		newTitle, _ := op["title"].(string)
		*desc = fmt.Sprintf("rename %s -> '%s'", pageID, newTitle)
		item["target"] = map[string]any{"page_id": pageID, "title": newTitle}

		if !dryRun {
			page, fetchErr := client.GetPageByID(pageID, "version,body.storage")
			if fetchErr != nil {
				return fetchErr
			}
			body := getNestedString(page, "body", "storage", "value")
			version := int(getNestedFloat(page, "version", "number"))
			payload := map[string]any{
				"type":    "page",
				"title":   newTitle,
				"version": map[string]any{"number": version + 1},
				"body": map[string]any{
					"storage": map[string]any{
						"value":          body,
						"representation": "storage",
					},
				},
			}
			_, err = client.UpdatePage(pageID, payload)
			if err != nil {
				return err
			}
		}
		return nil

	case "update":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		fileRel, _ := op["file"].(string)
		filePath := filepath.Join(basePath, fileRel)
		*desc = fmt.Sprintf("update %s from %s", pageID, fileRel)
		item["target"] = map[string]any{"page_id": pageID, "file": filePath}

		content, readErr := readTextFile(filePath)
		if readErr != nil {
			return readErr
		}

		if !dryRun {
			page, fetchErr := client.GetPageByID(pageID, "version")
			if fetchErr != nil {
				return fetchErr
			}
			title, _ := page["title"].(string)
			if opTitle, ok := op["title"].(string); ok && opTitle != "" {
				title = opTitle
			}
			version := int(getNestedFloat(page, "version", "number"))
			payload := map[string]any{
				"type":    "page",
				"title":   title,
				"version": map[string]any{"number": version + 1},
				"body": map[string]any{
					"storage": map[string]any{
						"value":          content,
						"representation": "storage",
					},
				},
			}
			_, err = client.UpdatePage(pageID, payload)
			if err != nil {
				return err
			}
		}
		return nil

	default:
		return fmt.Errorf("unknown operation: %s", opType)
	}
}
