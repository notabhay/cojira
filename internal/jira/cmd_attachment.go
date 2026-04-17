package jira

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewAttachmentCmd creates the "attachment" subcommand.
func NewAttachmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "attachment <issue>",
		Short: "List, upload, download, or delete Jira attachments",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttachment,
	}
	cmd.Flags().StringArray("upload", nil, "File to upload (repeatable)")
	cmd.Flags().String("download", "", "Attachment ID to download")
	cmd.Flags().String("delete", "", "Attachment ID to delete")
	cmd.Flags().String("output", "", "Output path for --download")
	cmd.Flags().Bool("dry-run", false, "Preview the upload without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runAttachment(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	uploads, _ := cmd.Flags().GetStringArray("upload")
	downloadID, _ := cmd.Flags().GetString("download")
	deleteID, _ := cmd.Flags().GetString("delete")
	outputPath, _ := cmd.Flags().GetString("output")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	actions := 0
	if len(uploads) > 0 {
		actions++
	}
	if downloadID != "" {
		actions++
	}
	if deleteID != "" {
		actions++
	}
	if actions > 1 {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Use only one of --upload, --download, or --delete.",
			ExitCode: 2,
		}
	}

	if len(uploads) > 0 {
		return runAttachmentUpload(cmd, client, issueID, uploads, dryRun, idemKey, mode)
	}
	if downloadID != "" {
		return runAttachmentDownload(cmd, client, issueID, downloadID, outputPath, mode)
	}
	if deleteID != "" {
		return runAttachmentDelete(cmd, client, issueID, deleteID, dryRun, idemKey, mode)
	}
	return runAttachmentList(cmd, client, issueID, mode)
}

func runAttachmentList(cmd *cobra.Command, client *Client, issueID, mode string) error {
	attachments, err := client.ListAttachments(issueID)
	if err != nil {
		return err
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "attachment",
			map[string]any{"issue": issueID},
			map[string]any{"attachments": attachments, "summary": map[string]any{"count": len(attachments)}},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Found %d attachment(s) on %s.\n", len(attachments), issueID)
		return nil
	}
	if len(attachments) == 0 {
		fmt.Println("No attachments found.")
		return nil
	}

	fmt.Printf("Attachments for %s:\n\n", issueID)
	rows := make([][]string, 0, len(attachments))
	for _, attachment := range attachments {
		author, _ := attachment["author"].(map[string]any)
		rows = append(rows, []string{
			normalizeMaybeString(attachment["id"]),
			output.Truncate(normalizeMaybeString(attachment["filename"]), 40),
			output.Truncate(formatUserDisplay(author), 24),
			formatHumanBytes(attachment["size"]),
			formatHumanTimestamp(normalizeMaybeString(attachment["created"])),
		})
	}
	fmt.Println(output.TableString([]string{"ID", "FILE", "AUTHOR", "SIZE", "CREATED"}, rows))
	return nil
}

func runAttachmentUpload(cmd *cobra.Command, client *Client, issueID string, uploads []string, dryRun bool, idemKey, mode string) error {
	target := map[string]any{"issue": issueID}
	result := map[string]any{"files": uploads}

	if dryRun {
		result["dry_run"] = true
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "attachment", target, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would upload %d attachment(s) to %s.\n", len(uploads), issueID)
			return nil
		}
		fmt.Printf("Would upload %d attachment(s) to %s.\n", len(uploads), issueID)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "attachment",
				target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Printf("Skipped duplicate attachment upload for %s.\n", issueID)
		return nil
	}

	items := make([]map[string]any, 0, len(uploads))
	for _, filePath := range uploads {
		item, err := client.UploadAttachment(issueID, filePath)
		if err != nil {
			return err
		}
		items = append(items, item)
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.attachment %s", issueID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "attachment",
			target,
			map[string]any{"attachments": items, "summary": map[string]any{"uploaded": len(items)}},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Uploaded %d attachment(s) to %s.\n", len(items), issueID)
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, fmt.Sprintf("%v", item["filename"]))
	}
	fmt.Printf("Uploaded attachments to %s: %s\n", issueID, strings.Join(names, ", "))
	return nil
}

func runAttachmentDownload(cmd *cobra.Command, client *Client, issueID, downloadID, outputPath, mode string) error {
	attachments, err := client.ListAttachments(issueID)
	if err != nil {
		return err
	}

	var selected map[string]any
	for _, attachment := range attachments {
		if fmt.Sprintf("%v", attachment["id"]) == downloadID {
			selected = attachment
			break
		}
	}
	if selected == nil {
		return &cerrors.CojiraError{
			Code:     cerrors.IdentUnresolved,
			Message:  fmt.Sprintf("Attachment %s was not found on %s.", downloadID, issueID),
			ExitCode: 1,
		}
	}

	if outputPath == "" {
		outputPath = filepath.Base(fmt.Sprintf("%v", selected["filename"]))
	}

	contentURL := strings.TrimSpace(fmt.Sprintf("%v", selected["content"]))
	if contentURL == "" || contentURL == "<nil>" {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Attachment %s does not expose a downloadable content URL.", downloadID),
			ExitCode: 1,
		}
	}

	if err := client.DownloadAttachment(contentURL, outputPath); err != nil {
		return err
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "attachment",
			map[string]any{"issue": issueID, "attachment_id": downloadID},
			map[string]any{"saved_to": outputPath, "filename": selected["filename"]},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Downloaded attachment %s from %s.\n", downloadID, issueID)
		return nil
	}
	fmt.Printf("Downloaded attachment %s to %s.\n", downloadID, outputPath)
	return nil
}

func runAttachmentDelete(cmd *cobra.Command, client *Client, issueID, deleteID string, dryRun bool, idemKey, mode string) error {
	attachments, err := client.ListAttachments(issueID)
	if err != nil {
		return err
	}

	var selected map[string]any
	for _, attachment := range attachments {
		if fmt.Sprintf("%v", attachment["id"]) == deleteID {
			selected = attachment
			break
		}
	}
	if selected == nil {
		return &cerrors.CojiraError{
			Code:     cerrors.IdentUnresolved,
			Message:  fmt.Sprintf("Attachment %s was not found on %s.", deleteID, issueID),
			ExitCode: 1,
		}
	}

	target := map[string]any{"issue": issueID, "attachment_id": deleteID}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "attachment", target, map[string]any{"dry_run": true, "deleted": false, "filename": selected["filename"]}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would delete attachment %s from %s.\n", deleteID, issueID)
			return nil
		}
		fmt.Printf("Would delete attachment %s from %s.\n", deleteID, issueID)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "attachment", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate attachment delete for %s.\n", issueID)
		return nil
	}

	if err := client.DeleteAttachment(deleteID); err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.attachment delete %s %s", issueID, deleteID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "attachment", target, map[string]any{"deleted": true, "filename": selected["filename"]}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Deleted attachment %s from %s.\n", deleteID, issueID)
		return nil
	}
	fmt.Printf("Deleted attachment %s from %s.\n", deleteID, issueID)
	return nil
}
