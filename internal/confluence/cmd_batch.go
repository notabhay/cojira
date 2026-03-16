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

type confluenceBatchPlan struct {
	Version    int                     `json:"version"`
	Source     string                  `json:"source,omitempty"`
	Operations []confluenceBatchPlanOp `json:"operations"`
}

type confluenceBatchPlanOp struct {
	ID          string         `json:"id"`
	Op          string         `json:"op"`
	Description string         `json:"description"`
	Target      map[string]any `json:"target,omitempty"`
	Title       string         `json:"title,omitempty"`
	Body        string         `json:"body,omitempty"`
}

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
	plan, idemKey, target, err := resolveConfluenceBatchPlan(client, useStdin, configFile, dryRun, idemKey)
	if err != nil {
		return err
	}

	if len(plan.Operations) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "batch", target,
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
				true, "confluence", "batch", target,
				map[string]any{
					"skipped":     true,
					"reason":      "idempotency_key_already_used",
					"request_id":  reqID,
					"idempotency": map[string]any{"key": idemKey},
				},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Skipped batch (idempotency key already used): %s\n", idemKey)
		return nil
	}

	if mode != "json" && !quiet && mode != "summary" {
		fmt.Printf("Batch mode: %d operation(s)\n", len(plan.Operations))
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
	var completed []idempotency.ResumeItem
	var remaining []idempotency.ResumeItem

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
			receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped %s (already completed in a prior batch attempt)", op.Description)}
			item["receipt"] = receipt.Format()
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

		opErr := executeConfluenceBatchPlanOp(client, op, dryRun)
		if opErr == nil && !dryRun {
			if recErr := idempotency.RecordValue(opKey, op.Description, op.Target); recErr != nil {
				opErr = fmt.Errorf("%s succeeded, but the resume checkpoint could not be saved: %w", op.Description, recErr)
				item["checkpoint_error"] = recErr.Error()
			}
		}

		if opErr != nil {
			item["ok"] = false
			item["error"] = opErr.Error()
			receipt := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", op.Description, opErr)}
			item["receipt"] = receipt.Format()
			if mode != "json" && !quiet && mode != "summary" {
				fmt.Fprintln(os.Stderr, receipt.Format())
			}
			failureCount++
			failures = append(failures, opErr.Error())
			remaining = append(remaining, idempotency.ResumeItem{
				ID:          op.ID,
				Description: op.Description,
				Target:      op.Target,
			})
		} else {
			item["ok"] = true
			receipt := output.Receipt{OK: true, DryRun: dryRun, Message: op.Description}
			item["receipt"] = receipt.Format()
			if mode != "json" && !quiet && mode != "summary" {
				fmt.Println(receipt.Format())
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

		if !dryRun && idx < len(plan.Operations)-1 && sleepDelay > 0 {
			time.Sleep(time.Duration(sleepDelay * float64(time.Second)))
		}
	}

	if !dryRun && failureCount == 0 {
		if recErr := idempotency.Record(idemKey, "confluence.batch"); recErr != nil {
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
		state := idempotency.NewResumeState("confluence.batch", idemKey, reqID, target, plan)
		state.Completed = completed
		state.Remaining = remaining
		resumable = state
	}

	if mode == "json" {
		var errObjs []any
		for _, msg := range failures {
			obj, _ := output.ErrorObj(cerrors.OpFailed, msg, "", "", nil)
			errObjs = append(errObjs, obj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			failureCount == 0, "confluence", "batch", target,
			map[string]any{
				"items":           items,
				"summary":         summary,
				"request_id":      reqID,
				"idempotency":     map[string]any{"key": idemKey},
				"resumable_state": resumable,
			},
			nil, errObjs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Batch complete: %d succeeded, %d failed.\n", successCount, failureCount)
		if failureCount > 0 {
			fmt.Printf("Resume with the same command and --idempotency-key %s.\n", idemKey)
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch had failures", ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d failed\n", successCount, failureCount)
		if failureCount > 0 {
			fmt.Printf("Resume with the same command and --idempotency-key %s.\n", idemKey)
		}
	}
	if failureCount > 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch had failures", ExitCode: 1}
	}
	return nil
}

func resolveConfluenceBatchPlan(client *Client, useStdin bool, configFile string, dryRun bool, requestedKey string) (confluenceBatchPlan, string, map[string]any, error) {
	if requestedKey != "" {
		var stored confluenceBatchPlan
		found, err := idempotency.LoadValue(requestedKey+".plan", &stored)
		if err != nil {
			return confluenceBatchPlan{}, "", nil, err
		}
		if found {
			return stored, requestedKey, targetForConfluenceBatchSource(stored.Source), nil
		}
	}

	if useStdin && configFile != "" {
		return confluenceBatchPlan{}, "", nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Cannot use both --stdin and a config file.",
			ExitCode: 2,
		}
	}

	var config map[string]any
	var rootDir string
	source := ""

	if useStdin {
		var operations []any
		decoder := json.NewDecoder(os.Stdin)
		for decoder.More() {
			var op any
			if decErr := decoder.Decode(&op); decErr != nil {
				return confluenceBatchPlan{}, "", nil, &cerrors.CojiraError{
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
		source = "stdin"
	} else if configFile != "" {
		configData, readErr := readJSONFile(configFile)
		if readErr != nil {
			return confluenceBatchPlan{}, "", nil, readErr
		}
		config = configData
		rootDir = filepath.Dir(configFile)
		source = configFile
	} else {
		return confluenceBatchPlan{}, "", nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Provide a config file, --stdin, or --idempotency-key for a saved resumable run.",
			ExitCode: 2,
		}
	}

	operations, _ := config["operations"].([]any)
	baseDir, _ := config["base_dir"].(string)
	basePath := rootDir
	if baseDir != "" {
		if filepath.IsAbs(baseDir) {
			basePath = baseDir
		} else {
			basePath = filepath.Join(rootDir, baseDir)
		}
	}

	planOps := make([]confluenceBatchPlanOp, 0, len(operations))
	for idx, opAny := range operations {
		op, ok := opAny.(map[string]any)
		if !ok {
			continue
		}
		opType, _ := op["op"].(string)
		opType = strings.ToLower(strings.TrimSpace(opType))
		switch opType {
		case "move":
			pageID, err := ResolvePageID(client, fmt.Sprintf("%v", op["page"]), "")
			if err != nil {
				return confluenceBatchPlan{}, "", nil, err
			}
			var parentID string
			parentVal := op["parent"]
			if parentVal != nil && parentVal != float64(0) && fmt.Sprintf("%v", parentVal) != "0" && fmt.Sprintf("%v", parentVal) != "root" {
				parentID, err = ResolvePageID(client, fmt.Sprintf("%v", parentVal), "")
				if err != nil {
					return confluenceBatchPlan{}, "", nil, err
				}
			}
			desc := fmt.Sprintf("move %s -> root", pageID)
			target := map[string]any{"page_id": pageID, "parent_id": parentID}
			if parentID != "" {
				desc = fmt.Sprintf("move %s -> %s", pageID, parentID)
			}
			planOps = append(planOps, confluenceBatchPlanOp{
				ID:          fmt.Sprintf("%04d-%s", idx, output.IdempotencyKey(opType, target)[:12]),
				Op:          opType,
				Description: desc,
				Target:      target,
			})

		case "rename":
			pageID, err := ResolvePageID(client, fmt.Sprintf("%v", op["page"]), "")
			if err != nil {
				return confluenceBatchPlan{}, "", nil, err
			}
			newTitle, _ := op["title"].(string)
			target := map[string]any{"page_id": pageID, "title": newTitle}
			planOps = append(planOps, confluenceBatchPlanOp{
				ID:          fmt.Sprintf("%04d-%s", idx, output.IdempotencyKey(opType, target)[:12]),
				Op:          opType,
				Description: fmt.Sprintf("rename %s -> '%s'", pageID, newTitle),
				Target:      target,
				Title:       newTitle,
			})

		case "update":
			pageID, err := ResolvePageID(client, fmt.Sprintf("%v", op["page"]), "")
			if err != nil {
				return confluenceBatchPlan{}, "", nil, err
			}
			fileRel, _ := op["file"].(string)
			content, readErr := readTextFile(filepath.Join(basePath, fileRel))
			if readErr != nil {
				return confluenceBatchPlan{}, "", nil, readErr
			}
			title, _ := op["title"].(string)
			target := map[string]any{"page_id": pageID, "file": fileRel}
			planOps = append(planOps, confluenceBatchPlanOp{
				ID:          fmt.Sprintf("%04d-%s", idx, output.IdempotencyKey(opType, target, title, content)[:12]),
				Op:          opType,
				Description: fmt.Sprintf("update %s from %s", pageID, fileRel),
				Target:      target,
				Title:       title,
				Body:        content,
			})

		default:
			return confluenceBatchPlan{}, "", nil, fmt.Errorf("unknown operation: %s", opType)
		}
	}

	plan := confluenceBatchPlan{
		Version:    1,
		Source:     source,
		Operations: planOps,
	}

	idemKey := requestedKey
	if idemKey == "" {
		idemKey = output.IdempotencyKey("confluence.batch", plan)
	}

	var stored confluenceBatchPlan
	found, err := idempotency.LoadValue(idemKey+".plan", &stored)
	if err != nil {
		return confluenceBatchPlan{}, "", nil, err
	}
	if found {
		return stored, idemKey, targetForConfluenceBatchSource(stored.Source), nil
	}
	if !dryRun {
		if err := idempotency.RecordValue(idemKey+".plan", "confluence.batch plan", plan); err != nil {
			return confluenceBatchPlan{}, "", nil, err
		}
	}
	return plan, idemKey, targetForConfluenceBatchSource(source), nil
}

func executeConfluenceBatchPlanOp(client *Client, op confluenceBatchPlanOp, dryRun bool) error {
	switch op.Op {
	case "move":
		pageID, _ := op.Target["page_id"].(string)
		parentID, _ := op.Target["parent_id"].(string)
		if dryRun {
			return nil
		}
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
		_, err := client.UpdatePage(pageID, payload)
		return err

	case "rename":
		pageID, _ := op.Target["page_id"].(string)
		if dryRun {
			return nil
		}
		page, fetchErr := client.GetPageByID(pageID, "version,body.storage")
		if fetchErr != nil {
			return fetchErr
		}
		body := getNestedString(page, "body", "storage", "value")
		version := int(getNestedFloat(page, "version", "number"))
		payload := map[string]any{
			"type":    "page",
			"title":   op.Title,
			"version": map[string]any{"number": version + 1},
			"body": map[string]any{
				"storage": map[string]any{
					"value":          body,
					"representation": "storage",
				},
			},
		}
		_, err := client.UpdatePage(pageID, payload)
		return err

	case "update":
		pageID, _ := op.Target["page_id"].(string)
		if dryRun {
			return nil
		}
		page, fetchErr := client.GetPageByID(pageID, "version")
		if fetchErr != nil {
			return fetchErr
		}
		title, _ := page["title"].(string)
		if op.Title != "" {
			title = op.Title
		}
		version := int(getNestedFloat(page, "version", "number"))
		payload := map[string]any{
			"type":    "page",
			"title":   title,
			"version": map[string]any{"number": version + 1},
			"body": map[string]any{
				"storage": map[string]any{
					"value":          op.Body,
					"representation": "storage",
				},
			},
		}
		_, err := client.UpdatePage(pageID, payload)
		return err
	}
	return fmt.Errorf("unknown operation: %s", op.Op)
}

func targetForConfluenceBatchSource(source string) map[string]any {
	if source == "stdin" {
		return map[string]any{"stdin": true}
	}
	return map[string]any{"config": source}
}
