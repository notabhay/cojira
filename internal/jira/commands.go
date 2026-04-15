package jira

import (
	"github.com/notabhay/cojira/internal/cli"
	"github.com/spf13/cobra"
)

// NewJiraCmd creates the top-level "jira" command with all subcommands registered.
func NewJiraCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jira",
		Short: "Jira CLI for creating, updating, and syncing issues",
		Long: "cojira jira: a general-purpose Jira CLI for creating, updating, and syncing issues.\n" +
			"Use --base-url or JIRA_BASE_URL unless the command includes an issue URL that can be inferred.",
	}

	// Persistent flags available to all subcommands.
	cmd.PersistentFlags().Bool("experimental", false, "Enable experimental commands (may use unsupported/internal Jira APIs)")
	cli.AddHTTPRetryFlags(cmd)

	// Register all subcommands.
	cmd.AddCommand(
		NewInfoCmd(),
		NewGetCmd(),
		NewProjectsCmd(),
		NewUsersCmd(),
		NewAssignCmd(),
		NewCommentCmd(),
		NewAttachmentCmd(),
		NewLinkCmd(),
		NewUpdateCmd(),
		NewTransitionCmd(),
		NewTransitionsCmd(),
		NewSearchCmd(),
		NewBoardIssuesCmd(),
		NewCreateCmd(),
		NewFieldsCmd(),
		NewValidateCmd(),
		NewWhoamiCmd(),
		NewBatchCmd(),
		NewBulkUpdateCmd(),
		NewBulkTransitionCmd(),
		NewBulkUpdateSummariesCmd(),
		NewSyncCmd(),
		NewSyncFromDirCmd(),
	)

	return cmd
}
