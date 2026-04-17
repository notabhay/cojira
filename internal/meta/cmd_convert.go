package meta

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/markdownconv"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewConvertCmd returns the standalone markup conversion utility.
func NewConvertCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "convert [text]",
		Short:         "Convert between supported markup formats",
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.MaximumNArgs(1),
		RunE:          runConvert,
	}
	cmd.Flags().String("from", markdownconv.FormatMarkdown, "Source format (currently only markdown)")
	cmd.Flags().String("to", markdownconv.FormatConfluenceStorage, "Target format: confluence-storage, jira-wiki, or jira-adf")
	cmd.Flags().StringP("file", "f", "", "Read input from a file")
	cmd.Flags().StringP("output", "o", "", "Write converted output to a file")
	cli.AddOutputFlags(cmd, false)
	return cmd
}

func runConvert(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	from, _ := cmd.Flags().GetString("from")
	to, _ := cmd.Flags().GetString("to")
	filePath, _ := cmd.Flags().GetString("file")
	outputPath, _ := cmd.Flags().GetString("output")

	if strings.ToLower(strings.TrimSpace(from)) != markdownconv.FormatMarkdown {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported source format %q. Only markdown is supported right now.", from),
			ExitCode: 2,
		}
	}

	if filePath != "" && len(args) > 0 {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Use either an inline argument or --file, not both.",
			ExitCode: 2,
		}
	}

	input, err := readConvertInput(filePath, args)
	if err != nil {
		return err
	}

	converted, err := markdownconv.Convert(input, to)
	if err != nil {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  err.Error(),
			ExitCode: 2,
		}
	}

	if outputPath != "" {
		if err := os.WriteFile(outputPath, []byte(converted), 0o644); err != nil {
			return err
		}
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "cojira", "convert",
			map[string]any{"from": from, "to": to, "file": filePath, "output": outputPath},
			map[string]any{"content": converted, "output_path": outputPath},
			nil, nil, "", "", "", nil,
		))
	}

	if outputPath != "" {
		if mode == "summary" {
			fmt.Printf("Converted %s to %s and wrote %s.\n", from, to, outputPath)
			return nil
		}
		fmt.Printf("Wrote converted %s output to %s.\n", to, outputPath)
		return nil
	}

	fmt.Print(converted)
	if !strings.HasSuffix(converted, "\n") {
		fmt.Println()
	}
	return nil
}

func readConvertInput(filePath string, args []string) (string, error) {
	if filePath != "" {
		data, err := os.ReadFile(filePath)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	if len(args) > 0 {
		return args[0], nil
	}
	if !output.IsTTY(int(os.Stdin.Fd())) {
		data, err := io.ReadAll(os.Stdin)
		if err != nil {
			return "", err
		}
		return string(data), nil
	}
	return "", &cerrors.CojiraError{
		Code:     cerrors.OpFailed,
		Message:  "Provide input text, pass --file, or pipe markdown on stdin.",
		ExitCode: 2,
	}
}
