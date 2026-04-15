package confluence

import (
	"os"

	"github.com/spf13/cobra"
)

// NewConfluenceCmd creates the "confluence" parent command with all subcommands.
func NewConfluenceCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "confluence",
		Short: "Confluence page management",
		Long: `Unified Confluence CLI for page management.

Page identifiers (flexible):
  - Page ID:      12345
  - URL:          https://confluence.../pages/12345/...
  - Tiny link:    APnAVAE
  - Space:Title:  SPACE:"My Page Title"

Environment variables:
  CONFLUENCE_API_TOKEN  - Personal Access Token (required)
  CONFLUENCE_BASE_URL   - Base URL (required)`,
	}

	// Persistent flag: --base-url (applies to all subcommands).
	defaultBaseURL := os.Getenv("CONFLUENCE_BASE_URL")
	cmd.PersistentFlags().String("base-url", defaultBaseURL, "Confluence base URL (overrides CONFLUENCE_BASE_URL)")

	// Register all subcommands.
	cmd.AddCommand(NewValidateCmd())
	cmd.AddCommand(NewInfoCmd())
	cmd.AddCommand(NewGetCmd())
	cmd.AddCommand(NewFindCmd())
	cmd.AddCommand(NewSpacesCmd())
	cmd.AddCommand(NewLabelsCmd())
	cmd.AddCommand(NewTreeCmd())
	cmd.AddCommand(NewRenameCmd())
	cmd.AddCommand(NewCreateCmd())
	cmd.AddCommand(NewUpdateCmd())
	cmd.AddCommand(NewMoveCmd())
	cmd.AddCommand(NewArchiveCmd())
	cmd.AddCommand(NewCopyTreeCmd())
	cmd.AddCommand(NewBatchCmd())

	return cmd
}
