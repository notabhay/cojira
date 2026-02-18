package confluence

import (
	"fmt"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewRenameCmd creates the "rename" subcommand.
func NewRenameCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rename <page> <title>",
		Short: "Rename a page",
		Args:  cobra.ExactArgs(2),
		RunE:  runRename,
	}
	cmd.Flags().Bool("plan", false, "Preview rename without applying")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runRename(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	newTitle := strings.TrimSpace(args[1])
	planMode, _ := cmd.Flags().GetBool("plan")

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "rename",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	if newTitle == "" {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.InvalidTitle, "Title cannot be empty.", "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "rename",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintln(os.Stderr, "Error: Title cannot be empty.")
		return &cerrors.CojiraError{Code: cerrors.InvalidTitle, Message: "Title cannot be empty.", ExitCode: 1}
	}

	if planMode {
		page, fetchErr := client.GetPageByID(pageID, "version")
		if fetchErr != nil {
			return fetchErr
		}
		oldTitle, _ := page["title"].(string)
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "rename",
				map[string]any{"page": pageArg, "page_id": pageID},
				map[string]any{
					"plan":       true,
					"id":         pageID,
					"title_from": oldTitle,
					"title_to":   newTitle,
					"idempotency": map[string]any{
						"key": output.IdempotencyKey("confluence.rename", pageID, newTitle),
					},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would rename page %s: %q -> %q.\n", pageID, oldTitle, newTitle)
			return nil
		}
		quiet, _ := cmd.Flags().GetBool("quiet")
		if !quiet {
			r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would rename page %s: %q -> %q", pageID, oldTitle, newTitle)}
			fmt.Println(r.Format())
		}
		return nil
	}

	page, err := client.GetPageByID(pageID, "version,body.storage")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.RenameFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "rename",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error renaming page %s: %v\n", pageID, err)
		return err
	}

	oldTitle, _ := page["title"].(string)
	body := getNestedString(page, "body", "storage", "value")
	oldVersion := int(getNestedFloat(page, "version", "number"))

	payload := map[string]any{
		"type":  "page",
		"title": newTitle,
		"version": map[string]any{
			"number": oldVersion + 1,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          body,
				"representation": "storage",
			},
		},
	}

	_, err = client.UpdatePage(pageID, payload)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.RenameFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "rename",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error renaming page %s: %v\n", pageID, err)
		return err
	}

	receipt := output.Receipt{
		OK:      true,
		Message: fmt.Sprintf("Renamed page %s: %q -> %q", pageID, oldTitle, newTitle),
		Changes: []output.Change{{Field: "title", OldValue: oldTitle, NewValue: newTitle}},
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "rename",
			map[string]any{"page": pageArg, "page_id": pageID},
			map[string]any{
				"id":           pageID,
				"title_from":   oldTitle,
				"title_to":     newTitle,
				"version_from": oldVersion,
				"version_to":   oldVersion + 1,
				"receipt":      receipt.Format(),
				"idempotency": map[string]any{
					"key": output.IdempotencyKey("confluence.rename", pageID, newTitle),
				},
			},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Renamed page %s: %q -> %q.\n", pageID, oldTitle, newTitle)
		return nil
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}
