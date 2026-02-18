package board

import (
	"github.com/notabhay/cojira/internal/jira"
	"github.com/spf13/cobra"
)

// RegisterBoardCommands adds the board-related experimental subcommands
// (board-swimlanes, board-detail-view) to the given jira parent command.
// clientFn is called lazily to construct the Jira client from flags on
// the parent command.
func RegisterBoardCommands(jiraCmd *cobra.Command, clientFn func(cmd *cobra.Command) (*jira.Client, error)) {
	jiraCmd.AddCommand(NewBoardSwimlanesCmd(clientFn))
	jiraCmd.AddCommand(NewBoardDetailViewCmd(clientFn))
}
