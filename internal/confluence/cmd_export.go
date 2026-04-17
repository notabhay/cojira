package confluence

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewExportCmd creates the "export" subcommand.
func NewExportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "export <page>",
		Short: "Export page content as storage XHTML, markdown, pdf, or word",
		Args:  cobra.ExactArgs(1),
		RunE:  runExport,
	}
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.Flags().String("format", "markdown", "Export format: storage, markdown, pdf, or word")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runExport(cmd *cobra.Command, args []string) error {
	format, _ := cmd.Flags().GetString("format")
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "storage", "markdown", "md", "raw", "xhtml", "storage-xhtml":
		return runGet(cmd, args)
	case "pdf", "word", "doc", "docx":
	default:
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Unsupported export format %q.", format), ExitCode: 2}
	}

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
		return err
	}
	data, suggestedName, err := client.DownloadPageExport(pageID, format)
	if err != nil {
		return err
	}
	outputFile, _ := cmd.Flags().GetString("output")
	if strings.TrimSpace(outputFile) == "" {
		if suggestedName == "" {
			ext := strings.ToLower(strings.TrimSpace(format))
			if ext == "word" {
				ext = "doc"
			}
			suggestedName = fmt.Sprintf("%s.%s", pageID, ext)
		}
		outputFile = filepath.Clean(suggestedName)
	}
	if err := os.WriteFile(outputFile, data, 0o644); err != nil {
		return err
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "export", map[string]any{"page": pageArg, "page_id": pageID}, map[string]any{"saved_to": outputFile, "format": format}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Saved %s export for page %s to %s.\n", format, pageID, outputFile)
		return nil
	}
	fmt.Printf("Saved %s export to: %s\n", format, outputFile)
	return nil
}
