package confluence

import (
	"fmt"
	"os"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewHistoryCmd creates the "history" subcommand.
func NewHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history <page>",
		Short: "Show Confluence page history metadata",
		Args:  cobra.ExactArgs(1),
		RunE:  runHistory,
	}
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runHistory(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "history",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	page, err := client.GetPageByID(pageID, "version")
	if err != nil {
		return err
	}
	history, err := client.GetPageHistory(pageID)
	if err != nil {
		return err
	}

	result := map[string]any{
		"id":             pageID,
		"title":          page["title"],
		"currentVersion": int(getNestedFloat(page, "version", "number")),
		"history":        history,
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "history",
			map[string]any{"page": pageArg, "page_id": pageID},
			result, nil, nil, "", "", "", nil,
		))
	}

	lastUpdatedBy := getNestedString(history, "lastUpdated", "by", "displayName")
	lastUpdatedWhen := getNestedString(history, "lastUpdated", "when")
	createdBy := getNestedString(history, "createdBy", "displayName")
	createdDate := getNestedString(history, "createdDate")
	prevVersion := getNestedFloat(history, "previousVersion", "number")
	nextVersion := getNestedFloat(history, "nextVersion", "number")

	if mode == "summary" {
		fmt.Printf("Page %s is at version %d.\n", pageID, result["currentVersion"])
		return nil
	}

	fmt.Printf("Page:            %v (%s)\n", result["title"], pageID)
	fmt.Printf("Current version: %d\n", result["currentVersion"])
	if createdBy != "" || createdDate != "" {
		fmt.Printf("Created:         %s | %s\n", createdBy, createdDate)
	}
	if lastUpdatedBy != "" || lastUpdatedWhen != "" {
		fmt.Printf("Last updated:    %s | %s\n", lastUpdatedBy, lastUpdatedWhen)
	}
	if prevVersion > 0 {
		fmt.Printf("Previous:        %d\n", int(prevVersion))
	}
	if nextVersion > 0 {
		fmt.Printf("Next:            %d\n", int(nextVersion))
	}
	return nil
}
