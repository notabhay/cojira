package confluence

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"

	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCopyTreeCmd creates the "copy-tree" subcommand.
func NewCopyTreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "copy-tree <page> <parent>",
		Short: "Copy a page and its descendants under a new parent",
		Args:  cobra.ExactArgs(2),
		RunE:  runCopyTree,
	}
	cmd.Flags().String("title-prefix", "", "Prefix to apply to copied page titles")
	cmd.Flags().Bool("strict", false, "Fail instead of falling back when copy API is unavailable")
	cmd.Flags().Bool("dry-run", false, "Preview actions without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runCopyTree(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	parentArg := args[1]
	titlePrefix, _ := cmd.Flags().GetString("title-prefix")
	strict, _ := cmd.Flags().GetBool("strict")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	reqID := output.RequestID()

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "copy-tree",
				map[string]any{"page": pageArg, "parent": parentArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	parentID, err := ResolvePageID(client, parentArg, "")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "copy-tree",
				map[string]any{"page": pageArg, "parent": parentArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	target := map[string]any{
		"page":      pageArg,
		"page_id":   pageID,
		"parent":    parentArg,
		"parent_id": parentID,
	}

	// Dry-run: count pages in tree.
	if dryRun {
		count := 1
		stack := []string{pageID}
		for len(stack) > 0 {
			pid := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			children, _ := client.GetChildren(pid, 100)
			for _, child := range children {
				childID, _ := child["id"].(string)
				if childID != "" {
					count++
					stack = append(stack, childID)
				}
			}
		}

		receipt := output.Receipt{
			OK:      true,
			DryRun:  true,
			Message: fmt.Sprintf("Would copy tree %s under %s (pages=%d)", pageID, parentID, count),
		}

		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "copy-tree", target,
				map[string]any{
					"dry_run":    true,
					"pages":      count,
					"receipt":    receipt.Format(),
					"request_id": reqID,
					"idempotency": map[string]any{
						"key": output.IdempotencyKey("confluence.copy_tree", pageID, parentID),
					},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would copy tree %s under %s (pages=%d).\n", pageID, parentID, count)
			return nil
		}
		quiet, _ := cmd.Flags().GetBool("quiet")
		if !quiet {
			fmt.Println(receipt.Format())
		}
		return nil
	}

	// Attempt native copy endpoint.
	copyPath := fmt.Sprintf("/content/%s/pagehierarchy/copy", pageID)
	copyBody, _ := json.Marshal(map[string]any{
		"destinationPageId": parentID,
		"copyDescendants":   true,
	})

	resp, nativeErr := client.Request("POST", copyPath, copyBody, nil)
	nativeSuccess := false

	if nativeErr != nil {
		if strict {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.HTTPError, nativeErr.Error(), "", "", nil)
				return output.PrintJSON(output.BuildEnvelope(
					false, "confluence", "copy-tree", target,
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", nativeErr)
			return nativeErr
		}
		// Fall through to manual copy.
	} else {
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode == 404 || resp.StatusCode == 405 {
			if strict {
				if mode == "json" {
					errObj, _ := output.ErrorObj(cerrors.Unsupported, "Copy-tree API endpoint is unavailable.", "Retry without --strict to use manual fallback.", "", nil)
					return output.PrintJSON(output.BuildEnvelope(
						false, "confluence", "copy-tree", target,
						nil, nil, []any{errObj}, "", "", "", nil,
					))
				}
				fmt.Fprintln(os.Stderr, "Error: Copy-tree API endpoint is unavailable (and --strict was set).")
				return &cerrors.CojiraError{Code: cerrors.Unsupported, Message: "Copy-tree API endpoint is unavailable.", ExitCode: 1}
			}
			// Fall through to manual copy.
		} else if resp.StatusCode >= 400 {
			var errMsg string
			var data map[string]any
			if decodeErr := json.NewDecoder(resp.Body).Decode(&data); decodeErr == nil {
				if msg, ok := data["message"].(string); ok {
					errMsg = msg
				}
			}
			if errMsg == "" {
				errMsg = fmt.Sprintf("HTTP %d", resp.StatusCode)
			}
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.HTTPError, errMsg, "", "", nil)
				return output.PrintJSON(output.BuildEnvelope(
					false, "confluence", "copy-tree", target,
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Error: %s\n", errMsg)
			return &cerrors.CojiraError{Code: cerrors.HTTPError, Message: errMsg, ExitCode: 1}
		} else {
			var data map[string]any
			if decodeErr := json.NewDecoder(resp.Body).Decode(&data); decodeErr != nil {
				data = map[string]any{"status_code": resp.StatusCode}
			}

			receipt := output.Receipt{
				OK:      true,
				Message: fmt.Sprintf("Requested copy tree %s under %s via API", pageID, parentID),
			}

			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "confluence", "copy-tree", target,
					map[string]any{
						"method":     "api",
						"response":   data,
						"receipt":    receipt.Format(),
						"request_id": reqID,
						"idempotency": map[string]any{
							"key": output.IdempotencyKey("confluence.copy_tree", pageID, parentID),
						},
					},
					nil, nil, "", "", "", nil,
				))
			}
			if mode == "summary" {
				fmt.Printf("Requested copy tree %s under %s via API.\n", pageID, parentID)
				return nil
			}
			quiet, _ := cmd.Flags().GetBool("quiet")
			if !quiet {
				fmt.Println(receipt.Format())
			}
			return nil
		}
	}

	if nativeSuccess {
		return nil // Already returned above; unreachable.
	}

	// Manual fallback: copy pages by creating new pages with the same storage XHTML.
	parentPage, err := client.GetPageByID(parentID, "space")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "copy-tree", target,
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error determining destination space: %v\n", err)
		return err
	}

	destSpace := getNestedString(parentPage, "space", "key")
	if destSpace == "" {
		errMsg := "Unable to determine destination space from parent page."
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, errMsg, "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "copy-tree", target,
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintln(os.Stderr, errMsg)
		return &cerrors.CojiraError{Code: cerrors.FetchFailed, Message: errMsg, ExitCode: 1}
	}

	type createdPage struct {
		SrcID string `json:"src_id"`
		DstID string `json:"dst_id"`
		Title string `json:"title"`
	}
	var created []createdPage

	var copyRecursive func(srcID, dstParent string) (string, error)
	copyRecursive = func(srcID, dstParent string) (string, error) {
		page, fetchErr := client.GetPageByID(srcID, "body.storage")
		if fetchErr != nil {
			return "", fetchErr
		}
		pageTitle, _ := page["title"].(string)
		pageBody := getNestedString(page, "body", "storage", "value")

		newID, newTitle, createErr := createUniqueTitle(client, destSpace, titlePrefix+pageTitle, pageBody, dstParent)
		if createErr != nil {
			return "", createErr
		}
		created = append(created, createdPage{SrcID: srcID, DstID: newID, Title: newTitle})

		children, _ := client.GetChildren(srcID, 100)
		for _, child := range children {
			childID, _ := child["id"].(string)
			if childID != "" {
				if _, copyErr := copyRecursive(childID, newID); copyErr != nil {
					return "", copyErr
				}
			}
		}
		return newID, nil
	}

	newRootID, err := copyRecursive(pageID, parentID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.CopyFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "copy-tree", target,
				map[string]any{"method": "manual", "created": created},
				nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error copying tree: %v\n", err)
		return err
	}

	receipt := output.Receipt{
		OK:      true,
		Message: fmt.Sprintf("Copied tree %s -> %s under %s (pages=%d)", pageID, newRootID, parentID, len(created)),
	}
	warningMsg := "Manual copy does not include attachments or page restrictions."

	if mode == "json" {
		warnObj, _ := output.ErrorObj(cerrors.CopyLimitation, warningMsg, "", "", nil)
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "copy-tree", target,
			map[string]any{
				"method":      "manual",
				"new_root_id": newRootID,
				"created":     created,
				"receipt":     receipt.Format(),
				"request_id":  reqID,
				"idempotency": map[string]any{
					"key": output.IdempotencyKey("confluence.copy_tree", pageID, parentID),
				},
			},
			[]any{warnObj}, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Copied tree %s -> %s under %s (pages=%d).\n", pageID, newRootID, parentID, len(created))
		return nil
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(receipt.Format())
		fmt.Fprintf(os.Stderr, "Warning: %s\n", warningMsg)
	}
	return nil
}

// createUniqueTitle attempts to create a page, retrying with "(Copy N)" suffix on title conflicts.
func createUniqueTitle(client *Client, space, baseTitle, body, parentID string) (string, string, error) {
	var lastErr error
	for i := 0; i < 10; i++ {
		attemptTitle := baseTitle
		if i > 0 {
			attemptTitle = fmt.Sprintf("%s (Copy %d)", baseTitle, i+1)
		}

		payload := map[string]any{
			"type":  "page",
			"title": attemptTitle,
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
		if err == nil {
			newID := fmt.Sprintf("%v", result["id"])
			return newID, attemptTitle, nil
		}

		lastErr = err
		msg := strings.ToLower(err.Error())
		if strings.Contains(msg, "already exists") || strings.Contains(msg, "duplicate") {
			continue
		}
		return "", "", err
	}
	return "", "", fmt.Errorf("unable to create page after title retries: %v", lastErr)
}

// CopyPageNativeURL builds the native copy API URL.
func CopyPageNativeURL(baseURL, pageID string) string {
	return fmt.Sprintf("%s/rest/api/content/%s/pagehierarchy/copy",
		strings.TrimRight(baseURL, "/"),
		url.PathEscape(pageID))
}
