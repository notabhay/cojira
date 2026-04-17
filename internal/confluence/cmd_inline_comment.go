package confluence

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewInlineCommentCmd creates the "inline-comment" command group.
func NewInlineCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "inline-comment",
		Short: "List, add, resolve, or delete Confluence inline comments",
	}
	cmd.AddCommand(
		newInlineCommentListCmd(),
		newInlineCommentAddCmd(),
		newInlineCommentResolveCmd(),
		newInlineCommentDeleteCmd(),
	)
	return cmd
}

func newInlineCommentListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <page>",
		Short: "List inline comments on a page",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			cfgData := loadProjectConfigData()
			pageID, err := ResolvePageID(client, args[0], defaultPageID(cfgData))
			if err != nil {
				return err
			}
			limit, _ := cmd.Flags().GetInt("limit")
			bodyFormat, _ := cmd.Flags().GetString("body-format")
			result, err := client.ListInlineComments(pageID, limit, "", bodyFormat)
			if err != nil {
				return err
			}
			return printInlineCommentResult(mode, "inline-comment.list", map[string]any{"page": args[0], "page_id": pageID}, result, "Listed inline comments.")
		},
	}
	cmd.Flags().Int("limit", 25, "Maximum inline comments to fetch")
	cmd.Flags().String("body-format", "storage", "Body format to request")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newInlineCommentAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <page>",
		Short: "Create an inline comment from raw inline-comment properties",
		Args:  cobra.ExactArgs(1),
		RunE:  runInlineCommentAdd,
	}
	addInlineCommentMutationFlags(cmd)
	return cmd
}

func newInlineCommentResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <comment-id>",
		Short: "Resolve an inline comment",
		Args:  cobra.ExactArgs(1),
		RunE:  runInlineCommentResolve,
	}
	cmd.Flags().Bool("resolved", true, "Resolved state to apply")
	cmd.Flags().String("message", "", "Optional version message")
	cmd.Flags().String("body", "", "Optional updated comment body")
	cmd.Flags().String("file", "", "Optional file for updated comment body")
	cmd.Flags().String("format", "storage", "Comment body format: storage or markdown")
	cmd.Flags().Bool("dry-run", false, "Preview the resolve without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func newInlineCommentDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <comment-id>",
		Short: "Delete an inline comment permanently",
		Args:  cobra.ExactArgs(1),
		RunE:  runInlineCommentDelete,
	}
	cmd.Flags().Bool("dry-run", false, "Preview the delete without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func addInlineCommentMutationFlags(cmd *cobra.Command) {
	cmd.Flags().String("body", "", "Comment body content")
	cmd.Flags().String("file", "", "Read comment body from a file")
	cmd.Flags().String("format", "storage", "Comment body format: storage or markdown")
	cmd.Flags().String("properties-json", "", "Raw inlineCommentProperties JSON object")
	cmd.Flags().String("properties-file", "", "Read inlineCommentProperties JSON from a file")
	cmd.Flags().Bool("dry-run", false, "Preview the create without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
}

func runInlineCommentAdd(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	cfgData := loadProjectConfigData()
	pageID, err := ResolvePageID(client, args[0], defaultPageID(cfgData))
	if err != nil {
		return err
	}
	body, properties, dryRun, idemKey, err := inlineCommentBodyAndProperties(cmd)
	if err != nil {
		return err
	}
	target := map[string]any{"page": args[0], "page_id": pageID}
	payload := map[string]any{
		"pageId": pageID,
		"body": map[string]any{
			"representation": "storage",
			"value":          body,
		},
		"inlineCommentProperties": properties,
	}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "inline-comment.add", target, map[string]any{"dry_run": true, "payload": payload}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "inline-comment.add", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	result, err := client.CreateInlineComment(payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.inline-comment.add %s", pageID))
	}
	return printInlineCommentResult(mode, "inline-comment.add", target, result, "Added inline comment.")
}

func runInlineCommentResolve(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	commentID := args[0]
	comment, err := client.GetInlineComment(commentID)
	if err != nil {
		return err
	}
	resolved, _ := cmd.Flags().GetBool("resolved")
	message, _ := cmd.Flags().GetString("message")
	body, _ := cmd.Flags().GetString("body")
	filePath, _ := cmd.Flags().GetString("file")
	format, _ := cmd.Flags().GetString("format")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	if filePath != "" {
		content, err := readTextFile(filePath)
		if err != nil {
			return err
		}
		body = content
	}
	if strings.TrimSpace(body) != "" {
		body, err = convertStorageBody(body, format)
		if err != nil {
			return err
		}
	} else {
		body = getNestedString(comment, "body", "storage", "value")
	}
	version := intFromAny(getNested(comment, "version", "number"), 0) + 1
	target := map[string]any{"comment_id": commentID}
	payload := map[string]any{
		"version": map[string]any{
			"number": version,
		},
		"resolved": resolved,
		"body": map[string]any{
			"representation": "storage",
			"value":          body,
		},
	}
	if strings.TrimSpace(message) != "" {
		payload["version"].(map[string]any)["message"] = message
	}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "inline-comment.resolve", target, map[string]any{"dry_run": true, "payload": payload}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "inline-comment.resolve", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	result, err := client.UpdateInlineComment(commentID, payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.inline-comment.resolve %s", commentID))
	}
	return printInlineCommentResult(mode, "inline-comment.resolve", target, result, "Updated inline comment.")
}

func runInlineCommentDelete(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	commentID := args[0]
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	target := map[string]any{"comment_id": commentID}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "inline-comment.delete", target, map[string]any{"dry_run": true, "deleted": false}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "inline-comment.delete", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	if err := client.DeleteInlineComment(commentID); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.inline-comment.delete %s", commentID))
	}
	return printInlineCommentResult(mode, "inline-comment.delete", target, map[string]any{"deleted": true}, "Deleted inline comment.")
}

func inlineCommentBodyAndProperties(cmd *cobra.Command) (string, map[string]any, bool, string, error) {
	body, _ := cmd.Flags().GetString("body")
	filePath, _ := cmd.Flags().GetString("file")
	format, _ := cmd.Flags().GetString("format")
	propertiesJSON, _ := cmd.Flags().GetString("properties-json")
	propertiesFile, _ := cmd.Flags().GetString("properties-file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	if body != "" && filePath != "" {
		return "", nil, false, "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --body or --file, not both.", ExitCode: 2}
	}
	if propertiesJSON != "" && propertiesFile != "" {
		return "", nil, false, "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --properties-json or --properties-file, not both.", ExitCode: 2}
	}
	if filePath != "" {
		content, err := readTextFile(filePath)
		if err != nil {
			return "", nil, false, "", err
		}
		body = content
	}
	body = strings.TrimSpace(body)
	if body == "" {
		return "", nil, false, "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Inline comment body cannot be empty.", ExitCode: 2}
	}
	converted, err := convertStorageBody(body, format)
	if err != nil {
		return "", nil, false, "", err
	}
	properties, err := decodeInlineCommentProperties(propertiesJSON, propertiesFile)
	if err != nil {
		return "", nil, false, "", err
	}
	return converted, properties, dryRun, idemKey, nil
}

func decodeInlineCommentProperties(raw, filePath string) (map[string]any, error) {
	if filePath != "" {
		content, err := readTextFile(filePath)
		if err != nil {
			return nil, err
		}
		raw = content
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Inline comment properties JSON is required.", ExitCode: 2}
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: fmt.Sprintf("Invalid inline comment properties JSON: %v", err), ExitCode: 1}
	}
	return result, nil
}

func printInlineCommentResult(mode, command string, target map[string]any, result map[string]any, summary string) error {
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", command, target, result, nil, nil, "", "", "", nil))
	}
	fmt.Println(summary)
	return nil
}
