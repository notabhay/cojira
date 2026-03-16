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
	outputFile, _ := cmd.Flags().GetString("output")

	if outputFile != "" {
		if err := writeFile(outputFile, content); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "view",
				map[string]any{"page": pageArg, "page_id": pageID},
				map[string]any{"saved_to": outputFile, "representation": "view"},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved rendered page %s to %s.\n", pageID, outputFile)
			return nil
		}
		fmt.Printf("Saved rendered content to: %s\n", outputFile)
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "view",
			map[string]any{"page": pageArg, "page_id": pageID},
			map[string]any{"content": content, "representation": "view"},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Fetched rendered page %s (content omitted in summary mode).\n", pageID)
		return nil
	}
	fmt.Println(content)
	return nil
}
