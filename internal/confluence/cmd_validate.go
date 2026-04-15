package confluence

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates the "validate" subcommand.
func NewValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Basic sanity check for Confluence storage-format XHTML",
		Args:  cobra.ExactArgs(1),
		RunE:  runValidate,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runValidate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	filePath := args[0]

	content, err := readTextFile(filePath)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FileNotFound, fmt.Sprintf("File not found: %s", filePath), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "validate",
				map[string]any{"file": filePath},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: File not found: %s\n", filePath)
		return err
	}

	if strings.TrimSpace(content) == "" {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.EmptyContent, "Refusing to validate empty content.", "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "validate",
				map[string]any{"file": filePath},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintln(os.Stderr, "Error: Refusing to validate empty content.")
		return &cerrors.CojiraError{Code: cerrors.EmptyContent, Message: "Refusing to validate empty content.", ExitCode: 1}
	}

	var warnings []any
	if !strings.Contains(content, "<") || !strings.Contains(content, ">") {
		warnings = append(warnings, "Content does not look like XHTML; ensure Confluence storage format.")
	}
	if err := validateStorageXHTML(content); err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.InvalidJSON, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "validate",
				map[string]any{"file": filePath},
				nil, warnings, []any{errObj}, "", "", "", nil,
			))
		}
		return &cerrors.CojiraError{Code: cerrors.InvalidJSON, Message: err.Error(), ExitCode: 1}
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "validate",
			map[string]any{"file": filePath},
			map[string]any{"valid": true, "bytes": len(content)},
			warnings, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Sanity check passed for Confluence content (%d bytes).\n", len(content))
		return nil
	}

	fmt.Println("Sanity check passed for Confluence content.")
	fmt.Printf("Bytes: %d\n", len(content))
	if len(warnings) > 0 {
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "Warning: %v\n", w)
		}
	}
	return nil
}

func validateStorageXHTML(content string) error {
	wrapped := `<root xmlns:ac="http://atlassian.com/content" xmlns:ri="http://atlassian.com/resource/identifier" xmlns:atlassian="http://atlassian.com/content">` + content + `</root>`
	decoder := xml.NewDecoder(strings.NewReader(wrapped))
	decoder.Entity = map[string]string{
		"nbsp":   "\u00a0",
		"ndash":  "\u2013",
		"mdash":  "\u2014",
		"hellip": "\u2026",
		"copy":   "\u00a9",
		"reg":    "\u00ae",
		"trade":  "\u2122",
	}
	for {
		if _, err := decoder.Token(); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("storage XHTML is not well-formed: %w", err)
		}
	}
}
