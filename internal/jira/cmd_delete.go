package jira

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the "delete" subcommand.
func NewDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <issue>",
		Short: "Delete a Jira issue",
		Args:  cobra.ExactArgs(1),
		RunE:  runDelete,
	}
	cmd.Flags().Bool("delete-subtasks", false, "Also delete subtasks")
	cmd.Flags().Bool("dry-run", false, "Preview deletion without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm destructive deletion")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
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
	deleteSubtasks, _ := cmd.Flags().GetBool("delete-subtasks")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	target := map[string]any{"issue": issueID}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "delete", target, map[string]any{"dry_run": true, "delete_subtasks": deleteSubtasks}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would delete %s.\n", issueID)
			return nil
		}
		fmt.Printf("Would delete %s.\n", issueID)
		return nil
	}

	if !yes {
		return &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Deletion is destructive. Preview with --dry-run first, then rerun with --yes to confirm.",
			ExitCode: 2,
		}
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "delete", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate delete for %s.\n", issueID)
		return nil
	}

	if err := client.DeleteIssue(issueID, deleteSubtasks); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.delete %s", issueID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "delete", target, map[string]any{"deleted": true}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Deleted %s.\n", issueID)
		return nil
	}
	fmt.Printf("Deleted %s.\n", issueID)
	return nil
}
