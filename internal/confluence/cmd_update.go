package confluence

import (
	"fmt"
	"os"
	"strings"

	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/idempotency"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewUpdateCmd creates the "update" subcommand.
func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <page> <file>",
		Short: "Update page from file (storage format XHTML)",
		Args:  cobra.ExactArgs(2),
		RunE:  runUpdate,
	}
	cmd.Flags().String("title", "", "New title (optional)")
	cmd.Flags().Bool("minor", false, "Mark as minor edit")
	cmd.Flags().Bool("diff", false, "Show a unified diff and exit without updating")
	cmd.Flags().Bool("preview", false, "Alias for --diff")
	cmd.Flags().Bool("plan", false, "Alias for --diff")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	filePath := args[1]
	titleFlag, _ := cmd.Flags().GetString("title")
	minorEdit, _ := cmd.Flags().GetBool("minor")
	diffMode, _ := cmd.Flags().GetBool("diff")
	previewMode, _ := cmd.Flags().GetBool("preview")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	wantsDiff := diffMode || previewMode

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "update",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	content, err := readTextFile(filePath)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FileNotFound, fmt.Sprintf("File not found: %s", filePath), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "update",
				map[string]any{"page": pageArg, "page_id": pageID, "file": filePath},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: File not found: %s\n", filePath)
		return err
	}

	if strings.TrimSpace(content) == "" {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.EmptyContent, "Refusing to update with empty content.", "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "update",
				map[string]any{"page": pageArg, "page_id": pageID, "file": filePath},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintln(os.Stderr, "Error: Refusing to update with empty content.")
		return &cerrors.CojiraError{Code: cerrors.EmptyContent, Message: "Refusing to update with empty content.", ExitCode: 1}
	}

	// Fetch current page.
	expandParts := "version"
	if wantsDiff {
		expandParts = "version,body.storage"
	}
	page, err := client.GetPageByID(pageID, expandParts)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.UpdateFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "update",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error updating page %s: %v\n", pageID, err)
		return err
	}

	title := titleFlag
	if title == "" {
		title, _ = page["title"].(string)
	}
	oldVersion := int(getNestedFloat(page, "version", "number"))

	if wantsDiff {
		current := getNestedString(page, "body", "storage", "value")
		diffText, additions, deletions := computeUnifiedDiff(current, content, pageID)
		changed := diffText != ""

		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "update",
				map[string]any{"page": pageArg, "page_id": pageID},
				map[string]any{
					"diff":    diffText,
					"changed": changed,
					"summary": map[string]any{
						"additions": additions,
						"deletions": deletions,
					},
					"idempotency": map[string]any{
						"key": output.IdempotencyKey("confluence.update", pageID, title, content),
					},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			status := "changes detected"
			if !changed {
				status = "no changes"
			}
			fmt.Printf("Previewed update for page %s (%s).\n", pageID, status)
			return nil
		}
		if changed {
			fmt.Printf("--- Changes for page %s ---\n", pageID)
			fmt.Print(diffText)
			fmt.Printf("\n%d addition(s), %d deletion(s)\n", additions, deletions)
		} else {
			fmt.Println("No changes.")
		}
		return nil
	}

	// Idempotency check.
	if idemKey != "" {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "confluence", "update",
					map[string]any{"page": pageArg, "page_id": pageID},
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Skipped (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	// Perform update.
	payload := map[string]any{
		"type":  "page",
		"title": title,
		"version": map[string]any{
			"number":    oldVersion + 1,
			"minorEdit": minorEdit,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          content,
				"representation": "storage",
			},
		},
	}

	_, err = client.UpdatePage(pageID, payload)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.UpdateFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "update",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error updating page %s: %v\n", pageID, err)
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.update %s", pageID))
	}

	newVersion := oldVersion + 1
	receipt := output.Receipt{
		OK:      true,
		Message: fmt.Sprintf("Updated page %s (%s) v%d -> v%d", pageID, title, oldVersion, newVersion),
		Changes: []output.Change{{Field: "version", OldValue: fmt.Sprintf("%d", oldVersion), NewValue: fmt.Sprintf("%d", newVersion)}},
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "update",
			map[string]any{"page": pageArg, "page_id": pageID},
			map[string]any{
				"id":           pageID,
				"title":        title,
				"version_from": oldVersion,
				"version_to":   newVersion,
				"minor":        minorEdit,
				"receipt":      receipt.Format(),
				"idempotency": map[string]any{
					"key": output.IdempotencyKey("confluence.update", pageID, title, content),
				},
			},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Updated page %s (%s) v%d -> v%d.\n", pageID, title, oldVersion, newVersion)
		return nil
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}

// computeUnifiedDiff computes a simple line-based diff between two strings.
// Returns the diff text, addition count, and deletion count.
func computeUnifiedDiff(oldContent, newContent, label string) (string, int, int) {
	oldLines := strings.Split(oldContent, "\n")
	newLines := strings.Split(newContent, "\n")

	if strings.Join(oldLines, "\n") == strings.Join(newLines, "\n") {
		return "", 0, 0
	}

	var diffLines []string
	diffLines = append(diffLines, fmt.Sprintf("--- %s.current", label))
	diffLines = append(diffLines, fmt.Sprintf("+++ %s.new", label))

	additions := 0
	deletions := 0

	i, j := 0, 0
	for i < len(oldLines) || j < len(newLines) {
		if i < len(oldLines) && j < len(newLines) && oldLines[i] == newLines[j] {
			diffLines = append(diffLines, " "+oldLines[i])
			i++
			j++
		} else if i < len(oldLines) {
			diffLines = append(diffLines, "-"+oldLines[i])
			deletions++
			i++
		} else if j < len(newLines) {
			diffLines = append(diffLines, "+"+newLines[j])
			additions++
			j++
		}
	}

	return strings.Join(diffLines, "\n"), additions, deletions
}
