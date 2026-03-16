package jira

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the "delete" subcommand.
func NewDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <issue>",
		Short: "Delete an issue",
		Args:  cobra.ExactArgs(1),
		RunE:  runDelete,
	}
	cmd.Flags().Bool("dry-run", false, "Preview deletion without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runDelete(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	dryRun, _ := cmd.Flags().GetBool("dry-run")

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "delete",
				map[string]any{"issue": issueID},
				map[string]any{"dry_run": true},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would delete %s.\n", issueID)
			return nil
		}
		receipt := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would delete %s", issueID)}
		fmt.Println(receipt.Format())
		return nil
	}

	if err := client.DeleteIssue(issueID); err != nil {
		return err
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "delete",
			map[string]any{"issue": issueID},
			map[string]any{"deleted": true},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Deleted %s.\n", issueID)
		return nil
	}
	receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Deleted %s", issueID)}
	fmt.Println(receipt.Format())
	return nil
}
