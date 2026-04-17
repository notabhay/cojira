package confluence

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
	cmd.Flags().Bool("stdin", false, "Read one attachment body from stdin")
	cmd.Flags().String("filename", "", "Attachment filename to use with --stdin")
	cmd.Flags().String("download", "", "Attachment ID to download")
	cmd.Flags().Bool("download-all", false, "Download all attachments")
	cmd.Flags().String("output", "", "Output path for --download")
	cmd.Flags().String("output-dir", "", "Directory for --download-all or download outputs")
	cmd.Flags().String("sync-dir", "", "Sync local directory contents into page attachments")
	cmd.Flags().Bool("delete-missing", false, "When syncing, delete remote attachments missing from the local directory")
	cmd.Flags().Bool("all", false, "Fetch all attachments")
	cmd.Flags().Int("limit", 20, "Maximum attachments to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cmd.Flags().Bool("dry-run", false, "Preview uploads without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm destructive attachment sync operations")
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
	useStdin, _ := cmd.Flags().GetBool("stdin")
	stdinFilename, _ := cmd.Flags().GetString("filename")
	downloadID, _ := cmd.Flags().GetString("download")
	downloadAll, _ := cmd.Flags().GetBool("download-all")
	outputPath, _ := cmd.Flags().GetString("output")
	outputDir, _ := cmd.Flags().GetString("output-dir")
	syncDir, _ := cmd.Flags().GetString("sync-dir")
	deleteMissing, _ := cmd.Flags().GetBool("delete-missing")
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")
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
	if strings.TrimSpace(syncDir) != "" {
		actions++
	}
	if actions > 1 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use only one attachment action at a time.", ExitCode: 2}
	}

	if len(uploads) > 0 {
		return runAttachmentUpload(client, pageArg, pageID, uploads, nil, "", dryRun, idemKey, mode)
	}
	if useStdin {
		if strings.TrimSpace(stdinFilename) == "" {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--filename is required with --stdin.", ExitCode: 2}
		}
		if dryRun {
			return runAttachmentUpload(client, pageArg, pageID, nil, nil, stdinFilename, dryRun, idemKey, mode)
		}
		data, err := io.ReadAll(cmd.InOrStdin())
		if err != nil {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Failed to read attachment from stdin: %v", err), ExitCode: 1}
		}
		return runAttachmentUpload(client, pageArg, pageID, nil, data, stdinFilename, dryRun, idemKey, mode)
	}
	if downloadID != "" {
		return runAttachmentDownload(client, pageArg, pageID, downloadID, outputPath, outputDir, mode)
	}
	if downloadAll {
		return runAttachmentDownloadAll(client, pageArg, pageID, outputDir, mode)
	}
	if strings.TrimSpace(syncDir) != "" {
		return runAttachmentSync(client, pageArg, pageID, syncDir, dryRun, deleteMissing, yes, idemKey, mode)
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

func runAttachmentUpload(client *Client, pageArg, pageID string, uploads []string, stdinData []byte, stdinFilename string, dryRun bool, idemKey, mode string) error {
	target := map[string]any{"page": pageArg, "page_id": pageID}
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
	if stdinFilename != "" {
		resultMap, err := client.UploadAttachmentBytes(pageID, stdinFilename, stdinData)
		if err != nil {
			return err
		}
		items = append(items, extractResults(resultMap)...)
	} else {
		for _, filePath := range uploads {
			resultMap, err := client.UploadAttachment(pageID, filePath)
			if err != nil {
				return err
			}
			items = append(items, extractResults(resultMap)...)
		}
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

func runAttachmentDownload(client *Client, pageArg, pageID, downloadID, outputPath, outputDir, mode string) error {
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

	if outputPath == "" && outputDir != "" {
		outputPath = filepath.Join(outputDir, filepath.Base(normalizeMaybeString(selected["title"])))
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

func runAttachmentDownloadAll(client *Client, pageArg, pageID, outputDir, mode string) error {
	data, err := client.ListAttachments(pageID, 200, 0)
	if err != nil {
		return err
	}
	items := extractResults(data)
	if outputDir == "" {
		outputDir = "."
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}
	downloads := make([]map[string]any, 0, len(items))
	for _, item := range items {
		name := normalizeMaybeString(item["title"])
		downloadURL := getNestedString(item, "_links", "download")
		if name == "" || downloadURL == "" {
			continue
		}
		targetPath := filepath.Join(outputDir, filepath.Base(name))
		if err := client.DownloadAttachment(downloadURL, targetPath); err != nil {
			return err
		}
		downloads = append(downloads, map[string]any{
			"id":       normalizeMaybeString(item["id"]),
			"title":    name,
			"saved_to": targetPath,
		})
	}
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment", map[string]any{"page": pageArg, "page_id": pageID, "download_all": true}, map[string]any{"attachments": downloads, "summary": map[string]any{"downloaded": len(downloads), "output_dir": outputDir}}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Downloaded %d attachment(s) from page %s.\n", len(downloads), pageID)
		return nil
	}
	fmt.Printf("Downloaded %d attachment(s) to %s.\n", len(downloads), outputDir)
	return nil
}

type localConfluenceAttachmentFile struct {
	Name string
	Path string
	Hash string
}

type remoteConfluenceAttachmentFile struct {
	ID          string
	Name        string
	DownloadURL string
	Hash        string
}

func runAttachmentSync(client *Client, pageArg, pageID, syncDir string, dryRun, deleteMissing, yes bool, idemKey, mode string) error {
	if deleteMissing && !dryRun && !yes {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Attachment sync with --delete-missing is destructive. Preview with --dry-run first, then rerun with --yes.", ExitCode: 2}
	}
	if idemKey != "" && !dryRun && idempotency.IsDuplicate(idemKey) {
		if mode == "json" || mode == "ndjson" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment.sync", map[string]any{"page": pageArg, "page_id": pageID, "dir": syncDir}, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate attachment sync for page %s.\n", pageID)
		return nil
	}

	localFiles, err := collectLocalConfluenceAttachments(syncDir)
	if err != nil {
		return err
	}
	data, err := client.ListAttachments(pageID, 200, 0)
	if err != nil {
		return err
	}
	remoteFiles := buildRemoteConfluenceAttachments(extractResults(data))

	localByName := map[string]localConfluenceAttachmentFile{}
	for _, item := range localFiles {
		localByName[item.Name] = item
	}

	toUpload := make([]localConfluenceAttachmentFile, 0)
	for _, localFile := range localFiles {
		remote := findRemoteConfluenceAttachment(remoteFiles, localFile.Name)
		if remote == nil {
			toUpload = append(toUpload, localFile)
			continue
		}
		hash, err := hashRemoteConfluenceAttachment(client, remote)
		if err != nil {
			return err
		}
		if hash != localFile.Hash {
			toUpload = append(toUpload, localFile)
		}
	}

	toDelete := make([]remoteConfluenceAttachmentFile, 0)
	if deleteMissing {
		for _, remote := range remoteFiles {
			if _, ok := localByName[remote.Name]; !ok {
				toDelete = append(toDelete, remote)
			}
		}
	}

	result := map[string]any{
		"dir":          syncDir,
		"upload_files": namesOfLocalConfluenceAttachments(toUpload),
		"delete_files": namesOfRemoteConfluenceAttachments(toDelete),
		"summary": map[string]any{
			"upload":  len(toUpload),
			"delete":  len(toDelete),
			"skipped": len(localFiles) - len(toUpload),
		},
	}
	if dryRun {
		result["dry_run"] = true
		if mode == "json" || mode == "ndjson" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment.sync", map[string]any{"page": pageArg, "page_id": pageID, "dir": syncDir}, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would sync attachments for page %s: %d upload, %d delete.\n", pageID, len(toUpload), len(toDelete))
			return nil
		}
		fmt.Printf("Would sync attachments for page %s: %d upload, %d delete.\n", pageID, len(toUpload), len(toDelete))
		return nil
	}

	uploaded := make([]map[string]any, 0)
	for _, localFile := range toUpload {
		resultMap, err := client.UploadAttachment(pageID, localFile.Path)
		if err != nil {
			return err
		}
		uploaded = append(uploaded, extractResults(resultMap)...)
	}
	for _, remote := range toDelete {
		if err := client.DeleteAttachment(remote.ID); err != nil {
			return err
		}
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.attachment.sync %s", pageID))
	}
	result["uploaded"] = uploaded
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "attachment.sync", map[string]any{"page": pageArg, "page_id": pageID, "dir": syncDir}, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Synced attachments for page %s: %d uploaded, %d deleted.\n", pageID, len(uploaded), len(toDelete))
		return nil
	}
	fmt.Printf("Synced attachments for page %s.\n", pageID)
	return nil
}

func collectLocalConfluenceAttachments(root string) ([]localConfluenceAttachmentFile, error) {
	info, err := os.Stat(root)
	if err != nil {
		return nil, err
	}
	if !info.IsDir() {
		return nil, &cerrors.CojiraError{Code: cerrors.FileNotFound, Message: fmt.Sprintf("Sync directory is not a directory: %s", root), ExitCode: 1}
	}
	files := make([]localConfluenceAttachmentFile, 0)
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		hash, hashErr := hashLocalConfluenceAttachment(path)
		if hashErr != nil {
			return hashErr
		}
		files = append(files, localConfluenceAttachmentFile{
			Name: filepath.Base(path),
			Path: path,
			Hash: hash,
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files, nil
}

func buildRemoteConfluenceAttachments(items []map[string]any) []remoteConfluenceAttachmentFile {
	files := make([]remoteConfluenceAttachmentFile, 0, len(items))
	for _, item := range items {
		files = append(files, remoteConfluenceAttachmentFile{
			ID:          normalizeMaybeString(item["id"]),
			Name:        normalizeMaybeString(item["title"]),
			DownloadURL: getNestedString(item, "_links", "download"),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name < files[j].Name
	})
	return files
}

func findRemoteConfluenceAttachment(items []remoteConfluenceAttachmentFile, name string) *remoteConfluenceAttachmentFile {
	for idx := range items {
		if items[idx].Name == name {
			return &items[idx]
		}
	}
	return nil
}

func hashLocalConfluenceAttachment(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return fmt.Sprintf("%x", sum[:]), nil
}

func hashRemoteConfluenceAttachment(client *Client, item *remoteConfluenceAttachmentFile) (string, error) {
	if item.Hash != "" {
		return item.Hash, nil
	}
	data, err := client.DownloadAttachmentContent(item.DownloadURL)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	item.Hash = fmt.Sprintf("%x", sum[:])
	return item.Hash, nil
}

func namesOfLocalConfluenceAttachments(items []localConfluenceAttachmentFile) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}

func namesOfRemoteConfluenceAttachments(items []remoteConfluenceAttachmentFile) []string {
	names := make([]string, 0, len(items))
	for _, item := range items {
		names = append(names, item.Name)
	}
	return names
}
