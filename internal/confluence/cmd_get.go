package confluence

import (
	"fmt"
	"os"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewGetCmd creates the "get" subcommand.
func NewGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <page>",
		Short: "Download page content",
		Args:  cobra.ExactArgs(1),
		RunE:  runGet,
	}
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.Flags().String("format", "storage", "Output format: storage or markdown")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runGet(cmd *cobra.Command, args []string) error {
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
				false, "confluence", "get",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	page, err := client.GetPageByID(pageID, "title,body.storage")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "get",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error fetching page %s: %v\n", pageID, err)
		return err
	}

	content := getNestedString(page, "body", "storage", "value")
	format, _ := cmd.Flags().GetString("format")
	rendered, warnings, err := renderPageBody(content, format)
	if err != nil {
		return err
	}
	outputFile, _ := cmd.Flags().GetString("output")

	if outputFile != "" {
		if err := writeFile(outputFile, rendered); err != nil {
			return err
		}
		jsonWarnings := warningValues(warnings)
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "get",
				map[string]any{"page": pageArg, "page_id": pageID},
				map[string]any{"saved_to": outputFile, "format": format},
				jsonWarnings, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved page %s to %s.\n", pageID, outputFile)
			return nil
		}
		fmt.Printf("Saved %s format to: %s\n", format, outputFile)
		printWarnings(cmd, warnings)
		return nil
	}

	if mode == "json" {
		jsonWarnings := warningValues(warnings)
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "get",
			map[string]any{"page": pageArg, "page_id": pageID},
			map[string]any{"content": rendered, "format": format},
			jsonWarnings, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Fetched page %s as %s (content omitted in summary mode).\n", pageID, format)
		return nil
	}
	fmt.Println(rendered)
	printWarnings(cmd, warnings)
	return nil
}

func printWarnings(cmd *cobra.Command, warnings []string) {
	if len(warnings) == 0 {
		return
	}
	for _, warning := range warnings {
		fmt.Fprintf(cmd.ErrOrStderr(), "Warning: %s\n", warning)
	}
}

func warningValues(values []string) []any {
	if len(values) == 0 {
		return nil
	}
	out := make([]any, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}
