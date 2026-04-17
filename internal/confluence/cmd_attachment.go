package confluence

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
		Use:   "attachment <page>",
		Short: "List, upload, or download Confluence attachments",
		Args:  cobra.ExactArgs(1),
		RunE:  runAttachment,
	}
	cmd.Flags().StringArray("upload", nil, "File to upload (repeatable)")
	cmd.Flags().String("download", "", "Attachment ID to download")
	cmd.Flags().String("output", "", "Output path for --download")
	cmd.Flags().Bool("all", false, "Fetch all attachments")
	cmd.Flags().Int("limit", 20, "Maximum attachments to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cmd.Flags().Bool("dry-run", false, "Preview uploads without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
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
	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		return err
	}

	uploads, _ := cmd.Flags().GetStringArray("upload")
	downloadID, _ := cmd.Flags().GetString("download")
	outputPath, _ := cmd.Flags().GetString("output")
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if len(uploads) > 0 && downloadID != "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --upload or --download, not both.", ExitCode: 2}
	}

	if len(uploads) > 0 {
		return runAttachmentUpload(client, pageArg, pageID, uploads, dryRun, idemKey, mode)
	}
	if downloadID != "" {
		return runAttachmentDownload(client, pageArg, pageID, downloadID, outputPath, mode)
	}
	return runAttachmentList(client, pageArg, pageID, all, limit, start, pageSize, mode)
}

func runAttachmentList(client *Client, pageArg, pageID string, all bool, limit, start, pageSize int, mode string) error {
	items := make([]map[string]any, 0)
	total := 0
	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.ListAttachments(pageID, pageSize, offset)
			if err != nil {
				return err
			}
			pageItems := extractResults(data)
			total = intFromAny(data["size"], total)
			items = append(items, pageItems...)
			offset += len(pageItems)
			if len(pageItems) == 0 {
				break
			}
		}
	} else {
		data, err := client.ListAttachments(pageID, limit, start)
		if err != nil {
			return err
		}
		items = extractResults(data)
		total = intFromAny(data["size"], len(items))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "attachment",
			map[string]any{"page": pageArg, "page_id": pageID},
			map[string]any{"attachments": items, "summary": map[string]any{"count": len(items), "total": total}},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Found %d attachment(s) on page %s.\n", len(items), pageID)
		return nil
	}
	if len(items) == 0 {
		fmt.Println("No attachments found.")
		return nil
	}

	fmt.Printf("Attachments on %s:\n\n", pageID)
	for _, item := range items {
		download := getNestedString(item, "_links", "download")
		fmt.Printf("  %-12v %-32v %v\n", item["id"], item["title"], download)
	}
	return nil
}

func runAttachmentUpload(client *Client, pageArg, pageID string, uploads []string, dryRun bool, idemKey, mode string) error {
	target := map[string]any{"page": pageArg, "page_id": pageID}
	result := map[string]any{"files": uploads}

	if dryRun {
		result["dry_run"] = true
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment", target, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would upload %d attachment(s) to page %s.\n", len(uploads), pageID)
			return nil
		}
		fmt.Printf("Would upload %d attachment(s) to page %s.\n", len(uploads), pageID)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate upload for %s.\n", pageID)
		return nil
	}

	items := make([]map[string]any, 0, len(uploads))
	for _, filePath := range uploads {
		result, err := client.UploadAttachment(pageID, filePath)
		if err != nil {
			return err
		}
		items = append(items, extractResults(result)...)
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.attachment %s", pageID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment", target, map[string]any{"attachments": items, "summary": map[string]any{"uploaded": len(items)}}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Uploaded %d attachment(s) to page %s.\n", len(items), pageID)
		return nil
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, normalizeMaybeString(item["title"]))
	}
	fmt.Printf("Uploaded attachments to %s: %s\n", pageID, strings.Join(names, ", "))
	return nil
}

func runAttachmentDownload(client *Client, pageArg, pageID, downloadID, outputPath, mode string) error {
	data, err := client.ListAttachments(pageID, 200, 0)
	if err != nil {
		return err
	}
	var selected map[string]any
	for _, item := range extractResults(data) {
		if normalizeMaybeString(item["id"]) == downloadID {
			selected = item
			break
		}
	}
	if selected == nil {
		return &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Attachment %s was not found on page %s.", downloadID, pageID), ExitCode: 1}
	}

	if outputPath == "" {
		outputPath = filepath.Base(normalizeMaybeString(selected["title"]))
	}
	downloadURL := getNestedString(selected, "_links", "download")
	if downloadURL == "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Attachment %s does not expose a download URL.", downloadID), ExitCode: 1}
	}

	if err := client.DownloadAttachment(downloadURL, outputPath); err != nil {
		return err
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment", map[string]any{"page": pageArg, "page_id": pageID, "attachment_id": downloadID}, map[string]any{"saved_to": outputPath, "title": selected["title"]}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Downloaded attachment %s from page %s.\n", downloadID, pageID)
		return nil
	}
	fmt.Printf("Downloaded attachment %s to %s.\n", downloadID, outputPath)
	return nil
}
