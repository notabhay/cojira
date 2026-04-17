package confluence

import (
	"fmt"
	"os"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDiffCmd creates the "diff" subcommand.
func NewDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <page>",
		Short: "Show a diff between two Confluence page versions",
		Args:  cobra.ExactArgs(1),
		RunE:  runDiff,
	}
	cmd.Flags().Int("from-version", 0, "Historical version number to compare from")
	cmd.Flags().Int("to-version", 0, "Optional version number to compare to (defaults to current)")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runDiff(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	fromVersion, _ := cmd.Flags().GetInt("from-version")
	toVersion, _ := cmd.Flags().GetInt("to-version")

	if fromVersion <= 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--from-version is required and must be > 0.", ExitCode: 2}
	}

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(false, "confluence", "diff", map[string]any{"page": pageArg}, nil, nil, []any{errObj}, "", "", "", nil))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	fromPage, err := client.GetPageVersion(pageID, fromVersion, "body.storage,version")
	if err != nil {
		return err
	}

	var toPage map[string]any
	if toVersion > 0 {
		toPage, err = client.GetPageVersion(pageID, toVersion, "body.storage,version")
	} else {
		toPage, err = client.GetPageByID(pageID, "body.storage,version")
	}
	if err != nil {
		return err
	}

	fromBody := getNestedString(fromPage, "body", "storage", "value")
	toBody := getNestedString(toPage, "body", "storage", "value")
	fromTitle, _ := fromPage["title"].(string)
	toTitle, _ := toPage["title"].(string)
	diffText, additions, deletions := computeUnifiedDiff(fromBody, toBody, pageID)
	titleChanged := fromTitle != toTitle
	changed := diffText != "" || titleChanged
	toVersionResolved := int(getNestedFloat(toPage, "version", "number"))

	result := map[string]any{
		"id":            pageID,
		"from_version":  fromVersion,
		"to_version":    toVersionResolved,
		"from_title":    fromTitle,
		"to_title":      toTitle,
		"changed":       changed,
		"title_changed": titleChanged,
		"diff":          diffText,
		"summary": map[string]any{
			"additions":     additions,
			"deletions":     deletions,
			"title_changed": titleChanged,
		},
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "diff",
			map[string]any{"page": pageArg, "page_id": pageID},
			result, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		status := "changes detected"
		if !changed {
			status = "no changes"
		}
		fmt.Printf("Compared page %s v%d -> v%d (%s).\n", pageID, fromVersion, toVersionResolved, status)
		return nil
	}

	fmt.Printf("Page %s diff: v%d -> v%d\n", pageID, fromVersion, toVersionResolved)
	if fromTitle != toTitle {
		fmt.Printf("Title: %q -> %q\n", fromTitle, toTitle)
	}
	if !changed {
		fmt.Println("No content changes.")
		return nil
	}
	fmt.Print(diffText)
	fmt.Printf("\n%d addition(s), %d deletion(s)\n", additions, deletions)
	return nil
}
