package jira

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewAssignCmd creates the "assign" subcommand.
func NewAssignCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "assign <issue> <user>",
		Short: "Assign an issue to a Jira user",
		Args:  cobra.ExactArgs(2),
		RunE:  runAssign,
	}
	cmd.Flags().Bool("dry-run", false, "Preview assignment without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runAssign(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	userRef := args[1]
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	user, err := resolveUserReference(client, userRef)
	if err != nil {
		return err
	}

	payload := jiraUserAssignmentPayload(user)
	display := formatUserDisplay(user)
	target := map[string]any{"issue": issueID, "user": userRef}
	result := map[string]any{
		"issue": issueID,
		"user":  user,
	}

	if dryRun {
		result["dry_run"] = true
		result["assignment"] = payload
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "assign", target, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would assign %s to %s.\n", issueID, display)
			return nil
		}
		fmt.Printf("Would assign %s to %s.\n", issueID, display)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "assign",
				target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Printf("Skipped duplicate assignment for %s.\n", issueID)
		return nil
	}

	if err := client.AssignIssue(issueID, payload); err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.assign %s", issueID))
	}

	if mode == "json" {
		result["updated"] = true
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "assign", target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Assigned %s to %s.\n", issueID, display)
		return nil
	}
	fmt.Printf("Assigned %s to %s.\n", issueID, display)
	return nil
}
