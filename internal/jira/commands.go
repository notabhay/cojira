package jira

import (
	"os"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/spf13/cobra"
)

// NewJiraCmd creates the top-level "jira" command with all subcommands registered.
func NewJiraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jira",
		Short: "Jira CLI for creating, updating, and syncing issues",
		Long: "cojira jira: a general-purpose Jira CLI for creating, updating, and syncing issues.\n" +
			"Use --base-url or JIRA_BASE_URL to point at your Jira instance.",
	}

	// Persistent flags available to all subcommands.
	cmd.PersistentFlags().String("base-url", os.Getenv("JIRA_BASE_URL"), "Jira base URL (overrides JIRA_BASE_URL)")
	cmd.PersistentFlags().Bool("experimental", false, "Enable experimental commands (may use unsupported/internal Jira APIs)")
	cli.AddHTTPRetryFlags(cmd)

	// Register all subcommands.
	cmd.AddCommand(
		NewInfoCmd(),
		NewGetCmd(),
		NewRawCmd(),
		NewRawInternalCmd(),
		NewDeleteCmd(),
		NewUpdateCmd(),
		NewTransitionCmd(),
		NewTransitionsCmd(),
		NewSearchCmd(),
		NewBoardIssuesCmd(),
		NewCreateCmd(),
		NewCloneCmd(),
		NewFieldsCmd(),
		NewValidateCmd(),
		NewWhoamiCmd(),
		NewBatchCmd(),
		NewBulkUpdateCmd(),
		NewBulkTransitionCmd(),
		NewBulkUpdateSummariesCmd(),
		NewSyncCmd(),
		NewSyncFromDirCmd(),
		NewDevelopmentCmd(),
	)

	return cmd
}
