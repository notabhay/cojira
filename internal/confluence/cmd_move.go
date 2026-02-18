package confluence

import (
	"fmt"
	"os"

	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewMoveCmd creates the "move" subcommand.
func NewMoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move <page> <parent>",
		Short: "Move page to new parent",
		Args:  cobra.ExactArgs(2),
		RunE:  runMove,
	}
	cmd.Flags().Bool("plan", false, "Preview move without applying")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runMove(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	parentArg := args[1]
	planMode, _ := cmd.Flags().GetBool("plan")

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "move",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error resolving page: %v\n", err)
		return err
	}

	// Resolve parent (0 or "root" means root).
	var parentID string
	if parentArg == "0" || parentArg == "root" {
		parentID = ""
	} else {
		parentID, err = ResolvePageID(client, parentArg, "")
		if err != nil {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
				return output.PrintJSON(output.BuildEnvelope(
					false, "confluence", "move",
					map[string]any{"page": pageArg, "parent": parentArg, "page_id": pageID},
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Error resolving parent: %v\n", err)
			return err
		}
	}

	parentDesc := fmt.Sprintf("under %s", parentID)
	if parentID == "" {
		parentDesc = "to root"
	}

	if planMode {
		page, fetchErr := client.GetPageByID(pageID, "version")
		if fetchErr != nil {
			return fetchErr
		}
		title, _ := page["title"].(string)

		var parentIDVal any
		if parentID != "" {
			parentIDVal = parentID
		}

		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "move",
				map[string]any{"page": pageArg, "page_id": pageID, "parent": parentArg, "parent_id": parentIDVal},
				map[string]any{
					"plan":      true,
					"id":        pageID,
					"title":     title,
					"parent_id": parentIDVal,
					"idempotency": map[string]any{
						"key": output.IdempotencyKey("confluence.move", pageID, parentID),
					},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would move page %s (%s) %s.\n", pageID, title, parentDesc)
			return nil
		}
		quiet, _ := cmd.Flags().GetBool("quiet")
		if !quiet {
			r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would move page %s (%s) %s", pageID, title, parentDesc)}
			fmt.Println(r.Format())
		}
		return nil
	}

	// Perform the move via the client's MovePage helper.
	page, err := client.GetPageByID(pageID, "version,body.storage")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.MoveFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "move",
				map[string]any{"page": pageArg, "page_id": pageID, "parent": parentArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error moving page %s: %v\n", pageID, err)
		return err
	}

	title, _ := page["title"].(string)
	body := getNestedString(page, "body", "storage", "value")
	currentVersion := int(getNestedFloat(page, "version", "number"))

	ancestors := []map[string]any{}
	if parentID != "" {
		ancestors = []map[string]any{{"id": parentID}}
	}
	payload := map[string]any{
		"id":    pageID,
		"type":  "page",
		"title": title,
		"version": map[string]any{
			"number": currentVersion + 1,
		},
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
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.MoveFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "move",
				map[string]any{"page": pageArg, "page_id": pageID, "parent": parentArg, "parent_id": parentID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error moving page %s: %v\n", pageID, err)
		return err
	}

	versionFrom := currentVersion
	versionTo := currentVersion + 1

	var parentIDVal any
	if parentID != "" {
		parentIDVal = parentID
	}

	receipt := output.Receipt{
		OK:      true,
		Message: fmt.Sprintf("Moved page %s (%s) %s", pageID, title, parentDesc),
		Changes: []output.Change{{Field: "parent", OldValue: "", NewValue: parentID}},
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "move",
			map[string]any{"page": pageArg, "page_id": pageID, "parent": parentArg, "parent_id": parentIDVal},
			map[string]any{
				"id":           pageID,
				"title":        title,
				"parent_id":    parentIDVal,
				"version_from": versionFrom,
				"version_to":   versionTo,
				"receipt":      receipt.Format(),
				"idempotency": map[string]any{
					"key": output.IdempotencyKey("confluence.move", pageID, parentID),
				},
			},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Moved page %s (%s) %s.\n", pageID, title, parentDesc)
		return nil
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}
