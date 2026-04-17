package meta

import (
	"os"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/spf13/cobra"
)

// NewCompletionCmd returns the "completion" command for shell completions.
func NewCompletionCmd(root *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "completion <bash|zsh|fish|powershell>",
		Short: "Generate shell completion scripts",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			shell := strings.ToLower(strings.TrimSpace(args[0]))
			switch shell {
			case "bash":
				return root.GenBashCompletionV2(os.Stdout, true)
			case "zsh":
				return root.GenZshCompletion(os.Stdout)
			case "fish":
				return root.GenFishCompletion(os.Stdout, true)
			case "powershell":
				return root.GenPowerShellCompletionWithDesc(os.Stdout)
			default:
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Unsupported shell. Use bash, zsh, fish, or powershell.",
					ExitCode: 2,
				}
			}
		},
	}
	return cmd
}
