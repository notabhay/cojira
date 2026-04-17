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
	cmd.Flags().Int("concurrency", 1, "Number of concurrent operation workers (default: 1, max: 10)")
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
	concurrency, _ := cmd.Flags().GetInt("concurrency")
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

	concurrency = cli.ClampConcurrency(concurrency)
	if concurrency > 1 {
		type batchResult struct {
			item    map[string]any
			desc    string
			failure string
			skipped bool
			success bool
		}

		results := cli.RunParallel(len(operations), concurrency, func(idx int) batchResult {
			opAny := operations[idx]
			op, ok := opAny.(map[string]any)
			if !ok {
				return batchResult{item: map[string]any{"ok": false}, desc: "invalid operation", failure: "invalid operation"}
			}
			opType, _ := op["op"].(string)
			opType = strings.ToLower(opType)
			desc := ""
			item := map[string]any{"op": opType, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("confluence.batch.item", idemKey, idx, op)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed item %d (%s)", idx+1, opType)}
					item["receipt"] = receipt.Format()
					return batchResult{item: item, desc: opType, skipped: true}
				}
			}

			opErr := executeBatchOp(client, op, opType, basePath, dryRun, &desc, item)
			if !dryRun && sleepDelay > 0 {
				time.Sleep(time.Duration(sleepDelay * float64(time.Second)))
			}

			if opErr != nil {
				item["ok"] = false
				item["error"] = opErr.Error()
				receipt := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", desc, opErr)}
				item["receipt"] = receipt.Format()
				if desc == "" {
					desc = opType
				}
				return batchResult{item: item, desc: desc, failure: opErr.Error()}
			}

			item["ok"] = true
			if childKey != "" {
				item["resume_key"] = childKey
				_ = idempotency.Record(childKey, desc)
			}
			receipt := output.Receipt{OK: true, DryRun: dryRun, Message: desc}
			item["receipt"] = receipt.Format()
			return batchResult{item: item, desc: desc, success: true}
		})

		for idx, result := range results {
			items = append(items, result.item)
			switch {
			case result.skipped:
				skippedCount++
			case result.failure != "":
				failureCount++
				failures = append(failures, result.failure)
			case result.success:
				successCount++
			}

			status := "OK"
			if result.skipped {
				status = "SKIPPED"
			} else if result.failure != "" {
				status = "FAILED"
			}
			message := result.desc
			if message == "" {
				message = fmt.Sprintf("item %d", idx+1)
			}
			output.EmitProgress(mode, quiet, idx+1, len(operations), message, status)
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
					fmt.Fprintln(os.Stderr, receipt)
				}
			}
		}
	} else {
		for idx, opAny := range operations {
			op, ok := opAny.(map[string]any)
			if !ok {
				continue
			}
			opType, _ := op["op"].(string)
			opType = strings.ToLower(opType)
			desc := ""
			item := map[string]any{"op": opType, "ok": false}
			childKey := ""
			if idemKey != "" && !dryRun {
				childKey = output.IdempotencyKey("confluence.batch.item", idemKey, idx, op)
				if idempotency.IsDuplicate(childKey) {
					item["ok"] = true
					item["skipped"] = true
					item["resume_key"] = childKey
					receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Skipped already-completed item %d (%s)", idx+1, opType)}
					item["receipt"] = receipt.Format()
					items = append(items, item)
					skippedCount++
					output.EmitProgress(mode, quiet, idx+1, len(operations), opType, "SKIPPED")
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
				if childKey != "" {
					item["resume_key"] = childKey
					_ = idempotency.Record(childKey, desc)
				}
				receipt := output.Receipt{OK: true, DryRun: dryRun, Message: desc}
				item["receipt"] = receipt.Format()
				if mode != "json" && !quiet && mode != "summary" {
					fmt.Println(receipt.Format())
				}
				successCount++
			}

			items = append(items, item)
			status := "OK"
			if !item["ok"].(bool) {
				status = "FAILED"
			}
			output.EmitProgress(mode, quiet, idx+1, len(operations), desc, status)

			if !dryRun && idx < len(operations)-1 && sleepDelay > 0 {
				time.Sleep(time.Duration(sleepDelay * float64(time.Second)))
			}
		}
	}

	summary := map[string]any{
		"total":   len(operations),
		"ok":      successCount,
		"skipped": skippedCount,
		"failed":  failureCount,
		"dry_run": dryRun,
	}

	if idemKey != "" && !dryRun && failureCount == 0 {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.batch %s", configFile))
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
			nil, errObjs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Batch complete: %d succeeded, %d skipped, %d failed.\n", successCount, skippedCount, failureCount)
		if failureCount > 0 {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch had failures", ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d skipped, %d failed\n", successCount, skippedCount, failureCount)
	}
	if failureCount > 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Batch had failures", ExitCode: 1}
	}
	return nil
}

func executeBatchOp(client *Client, op map[string]any, opType, basePath string, dryRun bool, desc *string, item map[string]any) error {
	switch opType {
	case "create":
		title := strings.TrimSpace(fmt.Sprintf("%v", op["title"]))
		if title == "" || title == "<nil>" {
			return fmt.Errorf("create operation requires title")
		}
		space := strings.TrimSpace(fmt.Sprintf("%v", op["space"]))
		if space == "" || space == "<nil>" {
			return fmt.Errorf("create operation requires space")
		}
		parentRef := strings.TrimSpace(fmt.Sprintf("%v", op["parent"]))
		fileRel, _ := op["file"].(string)
		format := strings.TrimSpace(fmt.Sprintf("%v", op["format"]))
		if format == "" || format == "<nil>" {
			format = "storage"
		}

		body := ""
		if fileRel != "" {
			filePath := filepath.Join(basePath, fileRel)
			content, err := readTextFile(filePath)
			if err != nil {
				return err
			}
			body, err = convertStorageBody(content, format)
			if err != nil {
				return err
			}
			item["file"] = filePath
		}

		var parentID string
		if parentRef != "" && parentRef != "<nil>" {
			resolved, err := ResolvePageID(client, parentRef, "")
			if err != nil {
				return err
			}
			parentID = resolved
		}

		*desc = fmt.Sprintf("create page '%s' in %s", title, space)
		item["target"] = map[string]any{"title": title, "space": space, "parent_id": parentID}
		if dryRun {
			return nil
		}

		payload := map[string]any{
			"type":  "page",
			"title": title,
			"space": map[string]any{"key": space},
			"body": map[string]any{
				"storage": map[string]any{
					"value":          body,
					"representation": "storage",
				},
			},
		}
		if parentID != "" {
			payload["ancestors"] = []map[string]any{{"id": parentID}}
		}
		result, err := client.CreatePage(payload)
		if err != nil {
			return err
		}
		item["page"] = map[string]any{"id": result["id"], "title": result["title"]}
		return nil

	case "blog-create":
		title := strings.TrimSpace(fmt.Sprintf("%v", op["title"]))
		if title == "" || title == "<nil>" {
			return fmt.Errorf("blog-create operation requires title")
		}
		space := strings.TrimSpace(fmt.Sprintf("%v", op["space"]))
		if space == "" || space == "<nil>" {
			return fmt.Errorf("blog-create operation requires space")
		}
		fileRel, _ := op["file"].(string)
		format := strings.TrimSpace(fmt.Sprintf("%v", op["format"]))
		if format == "" || format == "<nil>" {
			format = "storage"
		}

		body := ""
		if fileRel != "" {
			filePath := filepath.Join(basePath, fileRel)
			content, err := readTextFile(filePath)
			if err != nil {
				return err
			}
			body, err = convertStorageBody(content, format)
			if err != nil {
				return err
			}
			item["file"] = filePath
		}

		*desc = fmt.Sprintf("create blog post '%s' in %s", title, space)
		item["target"] = map[string]any{"title": title, "space": space}
		if dryRun {
			return nil
		}

		payload := map[string]any{
			"type":  "blogpost",
			"title": title,
			"space": map[string]any{"key": space},
			"body": map[string]any{
				"storage": map[string]any{
					"value":          body,
					"representation": "storage",
				},
			},
		}
		result, err := client.CreatePage(payload)
		if err != nil {
			return err
		}
		item["blog"] = map[string]any{"id": result["id"], "title": result["title"]}
		return nil

	case "blog-update":
		blogStr := fmt.Sprintf("%v", op["blog"])
		if strings.TrimSpace(blogStr) == "" || strings.TrimSpace(blogStr) == "<nil>" {
			blogStr = fmt.Sprintf("%v", op["page"])
		}
		blogID, err := ResolvePageID(client, blogStr, "")
		if err != nil {
			return err
		}
		fileRel, _ := op["file"].(string)
		if strings.TrimSpace(fileRel) == "" {
			return fmt.Errorf("blog-update operation requires file")
		}
		filePath := filepath.Join(basePath, fileRel)
		content, err := readTextFile(filePath)
		if err != nil {
			return err
		}
		format := strings.TrimSpace(fmt.Sprintf("%v", op["format"]))
		if format == "" || format == "<nil>" {
			format = "storage"
		}
		content, err = convertStorageBody(content, format)
		if err != nil {
			return err
		}
		title := strings.TrimSpace(fmt.Sprintf("%v", op["title"]))
		if title == "<nil>" {
			title = ""
		}
		minor, _ := op["minor"].(bool)

		*desc = fmt.Sprintf("update blog %s from %s", blogID, fileRel)
		item["target"] = map[string]any{"blog_id": blogID, "file": filePath, "minor": minor}
		if title != "" {
			item["target"].(map[string]any)["title"] = title
		}
		if dryRun {
			return nil
		}

		page, err := client.GetPageByID(blogID, "version")
		if err != nil {
			return err
		}
		if title == "" {
			title, _ = page["title"].(string)
		}
		version := int(getNestedFloat(page, "version", "number"))
		payload := map[string]any{
			"type":  "blogpost",
			"title": title,
			"version": map[string]any{
				"number":    version + 1,
				"minorEdit": minor,
			},
			"body": map[string]any{
				"storage": map[string]any{
					"value":          content,
					"representation": "storage",
				},
			},
		}
		if _, err := client.UpdatePage(blogID, payload); err != nil {
			return err
		}
		return nil

	case "blog-delete":
		blogStr := fmt.Sprintf("%v", op["blog"])
		if strings.TrimSpace(blogStr) == "" || strings.TrimSpace(blogStr) == "<nil>" {
			blogStr = fmt.Sprintf("%v", op["page"])
		}
		blogID, err := ResolvePageID(client, blogStr, "")
		if err != nil {
			return err
		}
		*desc = fmt.Sprintf("delete blog %s", blogID)
		item["target"] = map[string]any{"blog_id": blogID}
		if dryRun {
			return nil
		}
		return client.DeleteContent(blogID)

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

	case "comment":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		body := strings.TrimSpace(fmt.Sprintf("%v", op["body"]))
		fileRel, _ := op["file"].(string)
		if (body == "" || body == "<nil>") && fileRel == "" {
			return fmt.Errorf("comment operation requires body or file")
		}
		if fileRel != "" {
			filePath := filepath.Join(basePath, fileRel)
			content, err := readTextFile(filePath)
			if err != nil {
				return err
			}
			body = content
			item["file"] = filePath
		}
		format := strings.TrimSpace(fmt.Sprintf("%v", op["format"]))
		if format == "" || format == "<nil>" {
			format = "storage"
		}
		body, err = convertStorageBody(body, format)
		if err != nil {
			return err
		}
		*desc = fmt.Sprintf("comment on %s", pageID)
		item["target"] = map[string]any{"page_id": pageID}
		item["body"] = body
		if dryRun {
			return nil
		}
		comment, err := client.AddPageComment(pageID, body)
		if err != nil {
			return err
		}
		item["comment"] = map[string]any{"id": comment["id"]}
		return nil

	case "attachment-upload":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		files := batchStringList(op["files"])
		if file := strings.TrimSpace(fmt.Sprintf("%v", op["file"])); file != "" && file != "<nil>" {
			files = append(files, file)
		}
		if len(files) == 0 {
			return fmt.Errorf("attachment-upload operation requires file or files")
		}
		resolved := make([]string, 0, len(files))
		for _, file := range files {
			resolved = append(resolved, filepath.Join(basePath, file))
		}
		*desc = fmt.Sprintf("upload %d attachment(s) to %s", len(resolved), pageID)
		item["target"] = map[string]any{"page_id": pageID, "files": resolved}
		if dryRun {
			return nil
		}
		uploaded := make([]map[string]any, 0, len(resolved))
		for _, filePath := range resolved {
			result, err := client.UploadAttachment(pageID, filePath)
			if err != nil {
				return err
			}
			uploaded = append(uploaded, extractResults(result)...)
		}
		item["attachments"] = uploaded
		return nil

	case "attachment-download":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		attachmentID := strings.TrimSpace(fmt.Sprintf("%v", op["attachment"]))
		if attachmentID == "" || attachmentID == "<nil>" {
			attachmentID = strings.TrimSpace(fmt.Sprintf("%v", op["attachment_id"]))
		}
		if attachmentID == "" || attachmentID == "<nil>" {
			return fmt.Errorf("attachment-download operation requires attachment or attachment_id")
		}
		outputPath := strings.TrimSpace(fmt.Sprintf("%v", op["output"]))
		if outputPath == "<nil>" {
			outputPath = ""
		}
		*desc = fmt.Sprintf("download attachment %s from %s", attachmentID, pageID)
		item["target"] = map[string]any{"page_id": pageID, "attachment_id": attachmentID, "output": outputPath}
		if dryRun {
			return nil
		}
		data, err := client.ListAttachments(pageID, 200, 0)
		if err != nil {
			return err
		}
		var selected map[string]any
		for _, candidate := range extractResults(data) {
			if normalizeMaybeString(candidate["id"]) == attachmentID {
				selected = candidate
				break
			}
		}
		if selected == nil {
			return fmt.Errorf("attachment %s was not found on page %s", attachmentID, pageID)
		}
		if outputPath == "" {
			outputPath = filepath.Base(normalizeMaybeString(selected["title"]))
		} else if !filepath.IsAbs(outputPath) {
			outputPath = filepath.Join(basePath, outputPath)
		}
		downloadURL := getNestedString(selected, "_links", "download")
		if downloadURL == "" {
			return fmt.Errorf("attachment %s does not expose a download URL", attachmentID)
		}
		if err := client.DownloadAttachment(downloadURL, outputPath); err != nil {
			return err
		}
		item["saved_to"] = outputPath
		return nil

	case "labels":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		addList := batchStringList(op["add"])
		removeList := batchStringList(op["remove"])
		*desc = fmt.Sprintf("update labels on %s", pageID)
		item["target"] = map[string]any{"page_id": pageID, "add": addList, "remove": removeList}
		if dryRun {
			return nil
		}
		for _, label := range addList {
			if err := client.SetPageLabel(pageID, label); err != nil {
				return err
			}
		}
		for _, label := range removeList {
			if err := client.DeletePageLabel(pageID, label); err != nil {
				return err
			}
		}
		return nil

	case "delete":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		*desc = fmt.Sprintf("delete %s", pageID)
		item["target"] = map[string]any{"page_id": pageID}
		if dryRun {
			return nil
		}
		return client.DeleteContent(pageID)

	case "archive":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		toParent := strings.TrimSpace(fmt.Sprintf("%v", op["to_parent"]))
		if toParent == "" || toParent == "<nil>" {
			return fmt.Errorf("archive operation requires to_parent")
		}
		parentID, err := ResolvePageID(client, toParent, "")
		if err != nil {
			return err
		}
		toSpace := strings.TrimSpace(fmt.Sprintf("%v", op["to_space"]))
		label := strings.TrimSpace(fmt.Sprintf("%v", op["label"]))
		if label == "" || label == "<nil>" {
			label = "archived"
		}
		labelTree, _ := op["label_tree"].(bool)
		*desc = fmt.Sprintf("archive %s under %s", pageID, parentID)
		item["target"] = map[string]any{"page_id": pageID, "parent_id": parentID, "label": label, "label_tree": labelTree}
		if dryRun {
			return nil
		}

		page, err := client.GetPageByID(pageID, "version,space,body.storage")
		if err != nil {
			return err
		}
		payload := map[string]any{
			"id":    pageID,
			"type":  "page",
			"title": page["title"],
			"version": map[string]any{
				"number": int(getNestedFloat(page, "version", "number")) + 1,
			},
			"ancestors": []map[string]any{{"id": parentID}},
			"body": map[string]any{
				"storage": map[string]any{
					"value":          getNestedString(page, "body", "storage", "value"),
					"representation": "storage",
				},
			},
		}
		if toSpace != "" && toSpace != "<nil>" {
			payload["space"] = map[string]any{"key": toSpace}
		}
		if _, err := client.UpdatePage(pageID, payload); err != nil {
			return err
		}
		labelTargets := []string{pageID}
		if labelTree {
			labelTargets = append(labelTargets, listDescendants(client, pageID)...)
		}
		for _, pid := range labelTargets {
			if err := client.SetPageLabel(pid, label); err != nil {
				return err
			}
		}
		return nil

	case "copy-tree":
		pageStr := fmt.Sprintf("%v", op["page"])
		pageID, err := ResolvePageID(client, pageStr, "")
		if err != nil {
			return err
		}
		parentRef := strings.TrimSpace(fmt.Sprintf("%v", op["parent"]))
		if parentRef == "" || parentRef == "<nil>" {
			return fmt.Errorf("copy-tree operation requires parent")
		}
		parentID, err := ResolvePageID(client, parentRef, "")
		if err != nil {
			return err
		}
		titlePrefix := strings.TrimSpace(fmt.Sprintf("%v", op["title_prefix"]))
		if titlePrefix == "<nil>" {
			titlePrefix = ""
		}
		strict, _ := op["strict"].(bool)
		opConcurrency := cli.ClampConcurrency(intFromAny(op["concurrency"], 1))
		*desc = fmt.Sprintf("copy tree %s under %s", pageID, parentID)
		item["target"] = map[string]any{"page_id": pageID, "parent_id": parentID, "title_prefix": titlePrefix, "strict": strict, "concurrency": opConcurrency}
		sem := newConfluenceSemaphore(opConcurrency)

		if dryRun {
			count := countPageTree(client, pageID, sem)
			item["summary"] = map[string]any{"pages": count}
			return nil
		}

		copyPath := fmt.Sprintf("/content/%s/pagehierarchy/copy", pageID)
		copyBody, _ := json.Marshal(map[string]any{
			"destinationPageId": parentID,
			"copyDescendants":   true,
		})

		resp, nativeErr := client.Request("POST", copyPath, copyBody, nil)
		if nativeErr == nil {
			defer func() { _ = resp.Body.Close() }()
			if resp.StatusCode == 404 || resp.StatusCode == 405 {
				if strict {
					return fmt.Errorf("copy-tree API endpoint is unavailable")
				}
			} else if resp.StatusCode >= 400 {
				return fmt.Errorf("copy-tree request failed: HTTP %d", resp.StatusCode)
			} else {
				var data map[string]any
				if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
					data = map[string]any{"status_code": resp.StatusCode}
				}
				item["method"] = "api"
				item["response"] = data
				return nil
			}
		} else if strict {
			return nativeErr
		}

		parentPage, err := client.GetPageByID(parentID, "space")
		if err != nil {
			return err
		}
		destSpace := getNestedString(parentPage, "space", "key")
		if destSpace == "" {
			return fmt.Errorf("unable to determine destination space from parent page")
		}

		type createdPage struct {
			SrcID string `json:"src_id"`
			DstID string `json:"dst_id"`
			Title string `json:"title"`
		}

		var copyRecursive func(srcID, dstParent string) (string, []createdPage, error)
		copyRecursive = func(srcID, dstParent string) (string, []createdPage, error) {
			var (
				page     map[string]any
				fetchErr error
			)
			withConfluenceSemaphore(sem, func() {
				page, fetchErr = client.GetPageByID(srcID, "body.storage")
			})
			if fetchErr != nil {
				return "", nil, fetchErr
			}
			pageTitle, _ := page["title"].(string)
			pageBody := getNestedString(page, "body", "storage", "value")

			newID, newTitle, createErr := createUniqueTitle(client, destSpace, titlePrefix+pageTitle, pageBody, dstParent, sem)
			if createErr != nil {
				return "", nil, createErr
			}
			subtreeCreated := []createdPage{{SrcID: srcID, DstID: newID, Title: newTitle}}

			children, err := getChildrenConcurrent(client, srcID, sem)
			if err != nil {
				return "", nil, err
			}
			for _, child := range children {
				childID, _ := child["id"].(string)
				if childID != "" {
					_, childCreated, copyErr := copyRecursive(childID, newID)
					if copyErr != nil {
						return "", nil, copyErr
					}
					subtreeCreated = append(subtreeCreated, childCreated...)
				}
			}
			return newID, subtreeCreated, nil
		}

		newRootID, created, err := copyRecursive(pageID, parentID)
		if err != nil {
			return err
		}
		item["method"] = "manual"
		item["new_root_id"] = newRootID
		item["created"] = created
		item["warning"] = "Manual copy does not include attachments or page restrictions."
		return nil

	default:
		return fmt.Errorf("unknown operation: %s", opType)
	}
}

func batchStringList(raw any) []string {
	switch v := raw.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			s := strings.TrimSpace(fmt.Sprintf("%v", item))
			if s != "" && s != "<nil>" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}
