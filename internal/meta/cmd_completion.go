package meta

import (
	"os"
	"path/filepath"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// NewCompletionCmd returns the "completion" command for shell completions.
func NewCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion",
		Short: "Generate shell completion scripts or man pages",
	}
	cmd.AddCommand(
		&cobra.Command{
			Use:   "bash",
			Short: "Generate Bash completion",
			RunE: func(cmd *cobra.Command, args []string) error {
				return root.GenBashCompletionV2(os.Stdout, true)
			},
		},
		&cobra.Command{
			Use:   "zsh",
			Short: "Generate Zsh completion",
			RunE: func(cmd *cobra.Command, args []string) error {
				return root.GenZshCompletion(os.Stdout)
			},
		},
		&cobra.Command{
			Use:   "fish",
			Short: "Generate Fish completion",
			RunE: func(cmd *cobra.Command, args []string) error {
				return root.GenFishCompletion(os.Stdout, true)
			},
		},
		&cobra.Command{
			Use:   "powershell",
			Short: "Generate PowerShell completion",
			RunE: func(cmd *cobra.Command, args []string) error {
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			},
		},
		newCompletionManCmd(root),
	)
	return cmd
}

func newCompletionManCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "man",
		Short: "Generate man pages for cojira commands",
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, _ := cmd.Flags().GetString("dir")
			if strings.TrimSpace(dir) == "" {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "--dir is required for man page generation.",
					ExitCode: 2,
				}
			}
			if err := os.MkdirAll(filepath.Clean(dir), 0o755); err != nil {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Could not create the man page output directory.",
					Hint:     err.Error(),
					ExitCode: 1,
				}
			}
			header := &doc.GenManHeader{
				Title:   "COJIRA",
				Section: "1",
			}
			return doc.GenManTree(root, header, dir)
		},
	}
	cmd.Flags().String("dir", "", "Output directory for generated man pages")
	return cmd
}
