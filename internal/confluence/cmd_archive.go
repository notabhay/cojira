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

// NewArchiveCmd creates the "archive" subcommand.
func NewArchiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive <page>",
		Short: "Move a page under an archive parent and apply a label",
		Args:  cobra.ExactArgs(1),
		RunE:  runArchive,
	}
	cmd.Flags().String("to-parent", "", "Destination archive parent page identifier")
	_ = cmd.MarkFlagRequired("to-parent")
	cmd.Flags().String("to-space", "", "Optional destination space key (best-effort)")
	cmd.Flags().String("label", "archived", "Label to apply (default: archived)")
	cmd.Flags().Bool("label-tree", false, "Apply label to all descendants (may be slow)")
	cmd.Flags().Bool("dry-run", false, "Preview actions without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runArchive(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	toParentArg, _ := cmd.Flags().GetString("to-parent")
	toSpace, _ := cmd.Flags().GetString("to-space")
	label, _ := cmd.Flags().GetString("label")
	label = strings.TrimSpace(label)
	if label == "" {
		label = "archived"
	}
	labelTree, _ := cmd.Flags().GetBool("label-tree")
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	reqID := output.RequestID()

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "archive",
				map[string]any{"page": pageArg, "to_parent": toParentArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	parentID, err := ResolvePageID(client, toParentArg, "")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "archive",
				map[string]any{"page": pageArg, "to_parent": toParentArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	target := map[string]any{
		"page":      pageArg,
		"page_id":   pageID,
		"to_parent": toParentArg,
		"parent_id": parentID,
	}
	if toSpace != "" {
		target["to_space"] = toSpace
	}

	var descendants []string
	if labelTree {
		descendants, err = collectDescendantIDs(client, pageID)
		if err != nil {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
				return output.PrintJSON(output.BuildEnvelope(
					false, "confluence", "archive",
					target,
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Error: %v\n", err)
			return err
		}
	}

	if dryRun {
		labelIDs := []string{pageID}
		labelIDs = append(labelIDs, descendants...)

		receipt := output.Receipt{
			OK:     true,
			DryRun: true,
			Message: fmt.Sprintf("Would archive page %s under %s (label=%s, label_count=%d)",
				pageID, parentID, label, len(labelIDs)),
		}

		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "archive",
				target,
				map[string]any{
					"dry_run":     true,
					"label":       label,
					"label_tree":  labelTree,
					"label_count": len(labelIDs),
					"receipt":     receipt.Format(),
					"request_id":  reqID,
					"idempotency": map[string]any{
						"key": output.IdempotencyKey("confluence.archive", pageID, parentID, label),
					},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would archive page %s under %s (label=%s).\n", pageID, parentID, label)
			return nil
		}
		quiet, _ := cmd.Flags().GetBool("quiet")
		if !quiet {
			fmt.Println(receipt.Format())
		}
		return nil
	}

	// Move page by updating ancestors.
	page, err := client.GetPageByID(pageID, "version,space,body.storage")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.MoveFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "archive", target,
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error moving page %s: %v\n", pageID, err)
		return err
	}

	title, _ := page["title"].(string)
	body := getNestedString(page, "body", "storage", "value")
	currentVersion := int(getNestedFloat(page, "version", "number"))

	payload := map[string]any{
		"id":    pageID,
		"type":  "page",
		"title": title,
		"version": map[string]any{
			"number": currentVersion + 1,
		},
		"ancestors": []map[string]any{{"id": parentID}},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          body,
				"representation": "storage",
			},
		},
	}
	if toSpace != "" {
		payload["space"] = map[string]any{"key": toSpace}
	}

	_, err = client.UpdatePage(pageID, payload)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.MoveFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "archive", target,
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error moving page %s: %v\n", pageID, err)
		return err
	}

	// Apply label to root and optionally descendants.
	type labelFailure struct {
		pageID string
		err    string
	}
	var labelFailures []labelFailure
	toLabel := []string{pageID}
	if labelTree {
		toLabel = append(toLabel, descendants...)
	}

	for _, pid := range toLabel {
		if labelErr := client.SetPageLabel(pid, label); labelErr != nil {
			labelFailures = append(labelFailures, labelFailure{pageID: pid, err: labelErr.Error()})
		}
	}

	ok := len(labelFailures) == 0
	labeled := len(toLabel) - len(labelFailures)
	receipt := output.Receipt{
		OK: ok,
		Message: fmt.Sprintf("Archived page %s under %s (label=%s, labeled=%d/%d)",
			pageID, parentID, label, labeled, len(toLabel)),
	}

	if mode == "json" {
		var failureObjs []any
		var warningObjs []any
		for _, f := range labelFailures {
			obj, _ := output.ErrorObj(cerrors.LabelFailed, fmt.Sprintf("%s: %s", f.pageID, f.err), "", "", nil)
			warningObjs = append(warningObjs, obj)
			failureObjs = append(failureObjs, map[string]any{"page_id": f.pageID, "error": f.err})
		}
		return output.PrintJSON(output.BuildEnvelope(
			ok, "confluence", "archive", target,
			map[string]any{
				"moved":          true,
				"label":          label,
				"label_tree":     labelTree,
				"labeled":        labeled,
				"label_failures": failureObjs,
				"receipt":        receipt.Format(),
				"request_id":     reqID,
				"idempotency": map[string]any{
					"key": output.IdempotencyKey("confluence.archive", pageID, parentID, label),
				},
			},
			warningObjs, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Archived page %s under %s (label=%s).\n", pageID, parentID, label)
		if !ok {
			return &cerrors.CojiraError{Code: cerrors.LabelFailed, Message: "Some labels failed", ExitCode: 1}
		}
		return nil
	}

	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(receipt.Format())
	}
	for _, f := range labelFailures {
		fmt.Fprintf(os.Stderr, "Failed to label %s: %s\n", f.pageID, f.err)
	}
	if !ok {
		return &cerrors.CojiraError{Code: cerrors.LabelFailed, Message: "Some labels failed", ExitCode: 1}
	}
	return nil
}
