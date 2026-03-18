package confluence

import (
	"fmt"
	"os"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewViewCmd creates the "view" subcommand.
func NewViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "view <page>",
		Short: "Fetch rendered page content for reading",
		Args:  cobra.ExactArgs(1),
		RunE:  runView,
	}
	cmd.Flags().String("format", "html", "Output format: html, text, or markdown")
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runView(cmd *cobra.Command, args []string) error {
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
				false, "confluence", "view",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	page, err := client.GetPageByID(pageID, "body.view")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "view",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error fetching page %s: %v\n", pageID, err)
		return err
	}

	content := getNestedString(page, "body", "view", "value")
	format, _ := cmd.Flags().GetString("format")
	rendered, err := renderViewContent(content, format)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.Unsupported, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "view",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		return err
	}
	outputFile, _ := cmd.Flags().GetString("output")

	if outputFile != "" {
		if err := writeFile(outputFile, rendered); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "view",
				map[string]any{"page": pageArg, "page_id": pageID},
				map[string]any{"saved_to": outputFile, "representation": "view", "format": format},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved rendered page %s (%s) to %s.\n", pageID, format, outputFile)
			return nil
		}
		fmt.Printf("Saved rendered content to: %s\n", outputFile)
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "view",
			map[string]any{"page": pageArg, "page_id": pageID},
			map[string]any{"content": rendered, "representation": "view", "format": format},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Fetched rendered page %s in %s format.\n", pageID, format)
		return nil
	}
	fmt.Println(rendered)
	return nil
}
