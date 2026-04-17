package jira

import (
	"crypto/sha256"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/notabhay/cojira/internal/undo"
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
	cmd.Flags().Bool("stdin", false, "Read one attachment body from stdin")
	cmd.Flags().String("filename", "", "Attachment filename to use with --stdin")
	cmd.Flags().String("download", "", "Attachment ID to download")
	cmd.Flags().Bool("download-all", false, "Download all attachments")
	cmd.Flags().String("delete", "", "Attachment ID to delete")
	cmd.Flags().String("output", "", "Output path for --download")
	cmd.Flags().String("output-dir", "", "Directory for --download-all or download outputs")
	cmd.Flags().String("sync-dir", "", "Sync local files from a directory into Jira attachments")
	cmd.Flags().Bool("replace-existing", false, "When syncing, replace same-name attachments whose content differs")
	cmd.Flags().Bool("delete-missing", false, "When syncing, delete remote attachments missing from the local directory")
	cmd.Flags().Bool("dry-run", false, "Preview the upload without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm destructive attachment sync operations")
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
	useStdin, _ := cmd.Flags().GetBool("stdin")
	stdinFilename, _ := cmd.Flags().GetString("filename")
	downloadID, _ := cmd.Flags().GetString("download")
	downloadAll, _ := cmd.Flags().GetBool("download-all")
	deleteID, _ := cmd.Flags().GetString("delete")
	outputPath, _ := cmd.Flags().GetString("output")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	syncDir, _ := cmd.Flags().GetString("sync-dir")
	replaceExisting, _ := cmd.Flags().GetBool("replace-existing")
	deleteMissing, _ := cmd.Flags().GetBool("delete-missing")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	actions := 0
	if len(uploads) > 0 {
		actions++
	}
	if useStdin {
		actions++
	}
	if downloadID != "" {
		actions++
	}
	if downloadAll {
		actions++
	}
	if deleteID != "" {
		actions++
	}
	if strings.TrimSpace(syncDir) != "" {
		actions++
	}
	if actions > 1 {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Use only one attachment action at a time.",
			ExitCode: 2,
		}
	}

	if len(uploads) > 0 {
		return runAttachmentUpload(cmd, client, issueID, uploads, nil, "", dryRun, idemKey, mode)
	}
	if useStdin {
		if strings.TrimSpace(stdinFilename) == "" {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "--filename is required with --stdin.",
				ExitCode: 2,
			}
		}
		if dryRun {
			return runAttachmentUpload(cmd, client, issueID, nil, nil, stdinFilename, dryRun, idemKey, mode)
		}
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Failed to read attachment from stdin: %v", err), ExitCode: 1}
		}
		return runAttachmentUpload(cmd, client, issueID, nil, data, stdinFilename, dryRun, idemKey, mode)
	}
	if downloadID != "" {
		return runAttachmentDownload(cmd, client, issueID, downloadID, outputPath, outputDir, mode)
	}
	if downloadAll {
		return runAttachmentDownloadAll(cmd, client, issueID, outputDir, mode)
	}
	if deleteID != "" {
		return runAttachmentDelete(cmd, client, issueID, deleteID, dryRun, idemKey, mode)
	}
	if strings.TrimSpace(syncDir) != "" {
		return runAttachmentSync(cmd, client, issueID, syncDir, dryRun, replaceExisting, deleteMissing, yes, idemKey, mode)
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

func runAttachmentUpload(cmd *cobra.Command, client *Client, issueID string, uploads []string, stdinData []byte, stdinFilename string, dryRun bool, idemKey, mode string) error {
	target := map[string]any{"issue": issueID}
	result := map[string]any{}
	if len(uploads) > 0 {
		result["files"] = uploads
	}
	if stdinFilename != "" {
		result["stdin"] = true
		result["filename"] = stdinFilename
	}

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
	undoGroupID := undo.NewGroupID("jira.attachment.upload")
	if stdinFilename != "" {
		item, err := client.UploadAttachmentBytes(issueID, stdinFilename, stdinData)
		if err != nil {
			return err
		}
		items = append(items, item)
		recordUndoAction(undoGroupID, issueID, "jira.attachment.upload", "attachment.delete", map[string]any{
			"attachment_id": normalizeMaybeString(item["id"]),
			"filename":      normalizeMaybeString(item["filename"]),
		})
	} else {
		for _, filePath := range uploads {
			item, err := client.UploadAttachment(issueID, filePath)
			if err != nil {
				return err
			}
			items = append(items, item)
			recordUndoAction(undoGroupID, issueID, "jira.attachment.upload", "attachment.delete", map[string]any{
				"attachment_id": normalizeMaybeString(item["id"]),
				"filename":      normalizeMaybeString(item["filename"]),
			})
		}
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

func runAttachmentDownload(cmd *cobra.Command, client *Client, issueID, downloadID, outputPath, outputDir, mode string) error {
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

	if outputPath == "" && outputDir != "" {
		outputPath = filepath.Join(outputDir, filepath.Base(fmt.Sprintf("%v", selected["filename"])))
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

func runAttachmentDownloadAll(cmd *cobra.Command, client *Client, issueID, outputDir, mode string) error {
	attachments, err := client.ListAttachments(issueID)
	if err != nil {
		return err
	}
	if outputDir == "" {
		outputDir = "."
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	items := make([]map[string]any, 0, len(attachments))
	for _, attachment := range attachments {
		filename := normalizeMaybeString(attachment["filename"])
		contentURL := strings.TrimSpace(normalizeMaybeString(attachment["content"]))
		if filename == "" || contentURL == "" {
			continue
		}
		targetPath := filepath.Join(outputDir, filename)
		if err := client.DownloadAttachment(contentURL, targetPath); err != nil {
			return err
		}
		items = append(items, map[string]any{
			"id":       normalizeMaybeString(attachment["id"]),
			"filename": filename,
			"saved_to": targetPath,
		})
	}

	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "attachment",
			map[string]any{"issue": issueID, "download_all": true},
			map[string]any{"attachments": items, "summary": map[string]any{"downloaded": len(items), "output_dir": outputDir}},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Downloaded %d attachment(s) from %s.\n", len(items), issueID)
		return nil
	}
	fmt.Printf("Downloaded %d attachment(s) to %s.\n", len(items), outputDir)
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

type localAttachmentFile struct {
	Name string
	Path string
	Size int64
	Hash string
}

type remoteAttachmentFile struct {
	ID         string
	Name       string
	Size       int64
	ContentURL string
	Hash       string
}

func runAttachmentSync(cmd *cobra.Command, client *Client, issueID, syncDir string, dryRun, replaceExisting, deleteMissing, yes bool, idemKey, mode string) error {
	if deleteMissing && !dryRun && !yes {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Attachment sync with --delete-missing is destructive. Preview with --dry-run first, then rerun with --yes.",
			ExitCode: 2,
		}
	}
	if idemKey != "" && !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" || mode == "ndjson" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "attachment.sync", map[string]any{"issue": issueID, "dir": syncDir}, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate attachment sync for %s.\n", issueID)
		return nil
	}

	localFiles, err := collectLocalAttachmentFiles(syncDir)
	if err != nil {
		return err
	}
	attachments, err := client.ListAttachments(issueID)
	if err != nil {
		return err
	}
	remoteFiles := buildRemoteAttachmentFiles(attachments)

	toUpload := make([]localAttachmentFile, 0)
	toDelete := make([]remoteAttachmentFile, 0)
	conflicts := make([]map[string]any, 0)
	localByName := make(map[string]localAttachmentFile, len(localFiles))
	for _, item := range localFiles {
		localByName[item.Name] = item
	}

	for _, localFile := range localFiles {
		sameName := findRemoteAttachmentsByName(remoteFiles, localFile.Name)
		if len(sameName) == 0 {
			toUpload = append(toUpload, localFile)
			continue
		}
		exactMatch, err := findExactRemoteAttachmentMatch(client, sameName, localFile)
		if err != nil {
			return err
		}
		if exactMatch != nil {
			continue
		}
		if replaceExisting {
			toUpload = append(toUpload, localFile)
			toDelete = append(toDelete, sameName...)
			continue
		}
		conflicts = append(conflicts, map[string]any{
			"filename": localFile.Name,
			"reason":   "remote_attachment_with_same_name_has_different_content",
		})
	}

	if deleteMissing {
		for _, remoteFile := range remoteFiles {
			if _, ok := localByName[remoteFile.Name]; !ok {
				toDelete = append(toDelete, remoteFile)
			}
		}
	}
	toDelete = uniqueRemoteAttachments(toDelete)

	result := map[string]any{
		"dir": syncDir,
		"summary": map[string]any{
			"upload":   len(toUpload),
			"delete":   len(toDelete),
			"conflict": len(conflicts),
			"skipped":  len(localFiles) - len(toUpload) - len(conflicts),
		},
		"upload_files": namesOfLocalAttachments(toUpload),
		"delete_files": namesOfRemoteAttachments(toDelete),
	}
	if len(conflicts) > 0 {
		result["conflicts"] = conflicts
	}

	if dryRun {
		result["dry_run"] = true
		if mode == "json" || mode == "ndjson" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "attachment.sync", map[string]any{"issue": issueID, "dir": syncDir}, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would sync attachments for %s: %d upload, %d delete, %d conflict.\n", issueID, len(toUpload), len(toDelete), len(conflicts))
			return nil
		}
		fmt.Printf("Would sync attachments for %s: %d upload, %d delete, %d conflict.\n", issueID, len(toUpload), len(toDelete), len(conflicts))
		return nil
	}

	for _, remoteFile := range toDelete {
		if err := client.DeleteAttachment(remoteFile.ID); err != nil {
			return err
		}
	}
	uploaded := make([]map[string]any, 0, len(toUpload))
	for _, localFile := range toUpload {
		item, err := client.UploadAttachment(issueID, localFile.Path)
		if err != nil {
			return err
		}
		uploaded = append(uploaded, item)
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.attachment.sync %s", issueID))
	}
	result["uploaded"] = uploaded
	result["deleted"] = namesOfRemoteAttachments(toDelete)

	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "attachment.sync", map[string]any{"issue": issueID, "dir": syncDir}, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Synced attachments for %s: %d uploaded, %d deleted, %d conflict.\n", issueID, len(uploaded), len(toDelete), len(conflicts))
		return nil
	}
	fmt.Printf("Synced attachments for %s.\n", issueID)
	return nil
}

func collectLocalAttachmentFiles(root string) ([]localAttachmentFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &cerrors.CojiraError{Code: cerrors.FileNotFound, Message: fmt.Sprintf("Sync directory is not a directory: %s", root), ExitCode: 1}
	}
	files := make([]localAttachmentFile, 0)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		info, statErr := d.Info()
		if statErr != nil {
			return statErr
		}
		hash, hashErr := hashLocalFile(path)
		if hashErr != nil {
			return hashErr
		}
		files = append(files, localAttachmentFile{
			Name: filepath.Base(path),
			Path: path,
			Size: info.Size(),
			Hash: hash,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Name == files[j].Name {
			return files[i].Path < files[j].Path
		}
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func buildRemoteAttachmentFiles(attachments []map[string]any) []remoteAttachmentFile {
	files := make([]remoteAttachmentFile, 0, len(attachments))
	for _, item := range attachments {
		files = append(files, remoteAttachmentFile{
			ID:         normalizeMaybeString(item["id"]),
			Name:       normalizeMaybeString(item["filename"]),
			Size:       int64(intFromAny(item["size"], 0)),
			ContentURL: normalizeMaybeString(item["content"]),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].Name == files[j].Name {
			return files[i].ID < files[j].ID
		}
		return files[i].Name < files[j].Name
	})
	return files
}

func findRemoteAttachmentsByName(files []remoteAttachmentFile, name string) []remoteAttachmentFile {
	matches := make([]remoteAttachmentFile, 0)
	for _, item := range files {
		if item.Name == name {
			matches = append(matches, item)
		}
	}
	return matches
}

func findExactRemoteAttachmentMatch(client *Client, files []remoteAttachmentFile, localFile localAttachmentFile) (*remoteAttachmentFile, error) {
	for idx := range files {
		if files[idx].Size != localFile.Size {
			continue
		}
		hash, err := hashRemoteAttachment(client, &files[idx])
		if err != nil {
			return nil, err
		}
		if hash == localFile.Hash {
			return &files[idx], nil
		}
	}
	return nil, nil
}

func uniqueRemoteAttachments(files []remoteAttachmentFile) []remoteAttachmentFile {
	seen := map[string]bool{}
	out := make([]remoteAttachmentFile, 0, len(files))
	for _, item := range files {
		key := item.ID
		if key == "" {
			key = item.Name + ":" + item.ContentURL
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, item)
	}
	return out
}

func namesOfLocalAttachments(files []localAttachmentFile) []string {
	names := make([]string, 0, len(files))
	for _, item := range files {
		names = append(names, item.Name)
	}
	return names
}

func namesOfRemoteAttachments(files []remoteAttachmentFile) []string {
	names := make([]string, 0, len(files))
	for _, item := range files {
		names = append(names, item.Name)
	}
	return names
}

func hashLocalFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}

func hashRemoteAttachment(client *Client, remote *remoteAttachmentFile) (string, error) {
	if remote.Hash != "" {
		return remote.Hash, nil
	}
	data, err := client.DownloadAttachmentContent(remote.ContentURL)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	remote.Hash = fmt.Sprintf("%x", sum[:])
	return remote.Hash, nil
}
