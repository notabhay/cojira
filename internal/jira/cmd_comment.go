package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCommentCmd creates the "comment" subcommand.
func NewCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment <issue>",
		Short: "List or add Jira comments",
		Args:  cobra.ExactArgs(1),
		RunE:  runComment,
	}
	cmd.Flags().String("add", "", "Comment body to add")
	cmd.Flags().String("file", "", "Read comment body from a file")
	cmd.Flags().Bool("dry-run", false, "Preview the comment add without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("all", false, "Fetch all comments")
	cmd.Flags().Int("limit", 20, "Maximum comments to fetch")
	cmd.Flags().Int("start", 0, "Start offset for comment listing")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runComment(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	addBody, _ := cmd.Flags().GetString("add")
	bodyFile, _ := cmd.Flags().GetString("file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if addBody != "" || bodyFile != "" {
		if addBody != "" && bodyFile != "" {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "Use either --add or --file, not both.",
				ExitCode: 2,
			}
		}
		if bodyFile != "" {
			content, err := readTextFile(bodyFile)
			if err != nil {
				return err
			}
			addBody = content
		}
		bodyText := strings.TrimSpace(addBody)
		if bodyText == "" {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "Comment body cannot be empty.",
				ExitCode: 2,
			}
		}
		return runAddComment(cmd, client, issueID, bodyText, dryRun, idemKey, mode)
	}

	return runListComments(cmd, client, issueID, all, limit, start, pageSize, mode)
}

func runAddComment(cmd *cobra.Command, client *Client, issueID, bodyText string, dryRun bool, idemKey, mode string) error {
	target := map[string]any{"issue": issueID}
	result := map[string]any{"issue": issueID, "body": bodyText}

	if dryRun {
		result["dry_run"] = true
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "comment", target, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would add a comment to %s.\n", issueID)
			return nil
		}
		fmt.Printf("Would add a comment to %s.\n", issueID)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "comment",
				target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Printf("Skipped duplicate comment add for %s.\n", issueID)
		return nil
	}

	comment, err := client.AddComment(issueID, bodyText)
	if err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.comment %s", issueID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "comment",
			target,
			map[string]any{"comment": comment},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Added a comment to %s.\n", issueID)
		return nil
	}
	fmt.Printf("Added comment %v to %s.\n", comment["id"], issueID)
	return nil
}

func runListComments(cmd *cobra.Command, client *Client, issueID string, all bool, limit, start, pageSize int, mode string) error {
	target := map[string]any{"issue": issueID}
	items := make([]map[string]any, 0)
	total := 0

	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.ListComments(issueID, pageSize, offset)
			if err != nil {
				return err
			}
			raw, _ := data["comments"].([]any)
			pageItems := coerceJSONArray(raw)
			total = intFromAny(data["total"], total)
			items = append(items, pageItems...)
			offset += len(pageItems)
			if len(pageItems) == 0 || (total > 0 && offset >= total) {
				break
			}
		}
	} else {
		data, err := client.ListComments(issueID, limit, start)
		if err != nil {
			return err
		}
		if raw, ok := data["comments"].([]any); ok {
			items = coerceJSONArray(raw)
		}
		total = intFromAny(data["total"], len(items))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "comment",
			target,
			map[string]any{
				"comments": items,
				"summary":  map[string]any{"count": len(items), "total": total},
			},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		if all {
			fmt.Printf("Found %d comment(s) on %s.\n", len(items), issueID)
		} else {
			fmt.Printf("Fetched %d of %d comment(s) on %s.\n", len(items), total, issueID)
		}
		return nil
	}

	if len(items) == 0 {
		fmt.Println("No comments found.")
		return nil
	}

	fmt.Printf("Comments for %s:\n\n", issueID)
	for _, comment := range items {
		author, _ := comment["author"].(map[string]any)
		authorName := strings.TrimSpace(fmt.Sprintf("%v", author["displayName"]))
		if authorName == "" {
			authorName = strings.TrimSpace(fmt.Sprintf("%v", author["name"]))
		}
		body := strings.TrimSpace(fmt.Sprintf("%v", comment["body"]))
		fmt.Printf("[%v] %s | %v\n%s\n\n", comment["id"], authorName, comment["created"], body)
	}
	return nil
}
