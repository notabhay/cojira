package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewLinkCmd creates the "link" subcommand.
func NewLinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link <source> <target>",
		Short: "Create an issue link between two Jira issues",
		Args:  cobra.ExactArgs(2),
		RunE:  runLink,
	}
	cmd.Flags().String("type", "Relates", "Link type name (for example: Relates, Blocks, Duplicates)")
	cmd.Flags().String("comment", "", "Optional comment to include with the link")
	cmd.Flags().String("comment-file", "", "Read the optional comment from a file")
	cmd.Flags().Bool("dry-run", false, "Preview the link without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runLink(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	source := ResolveIssueIdentifier(args[0])
	targetIssue := ResolveIssueIdentifier(args[1])
	linkType, _ := cmd.Flags().GetString("type")
	commentText, _ := cmd.Flags().GetString("comment")
	commentFile, _ := cmd.Flags().GetString("comment-file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if commentText != "" && commentFile != "" {
		return fmt.Errorf("use either --comment or --comment-file, not both")
	}
	if commentFile != "" {
		content, err := readTextFile(commentFile)
		if err != nil {
			return err
		}
		commentText = strings.TrimSpace(content)
	}

	payload := map[string]any{
		"type":         map[string]any{"name": linkType},
		"outwardIssue": map[string]any{"key": source},
		"inwardIssue":  map[string]any{"key": targetIssue},
	}
	if commentText != "" {
		payload["comment"] = map[string]any{"body": commentText}
	}

	target := map[string]any{"source": source, "target": targetIssue}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "link",
				target,
				map[string]any{"dry_run": true, "payload": payload},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would link %s to %s using %q.\n", source, targetIssue, linkType)
			return nil
		}
		fmt.Printf("Would link %s to %s using %q.\n", source, targetIssue, linkType)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "link",
				target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Printf("Skipped duplicate link request for %s and %s.\n", source, targetIssue)
		return nil
	}

	if err := client.CreateIssueLink(payload); err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.link %s %s", source, targetIssue))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "link",
			target,
			map[string]any{"linked": true, "type": linkType},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Linked %s to %s using %q.\n", source, targetIssue, linkType)
		return nil
	}
	fmt.Printf("Linked %s to %s using %q.\n", source, targetIssue, linkType)
	return nil
}
