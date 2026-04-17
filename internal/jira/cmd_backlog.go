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

// NewBacklogCmd creates the "backlog" command group.
func NewBacklogCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "backlog",
		Short: "Inspect and manage Jira backlog placement",
	}
	cmd.AddCommand(
		newBacklogListCmd(),
		newBacklogMoveToCmd(),
		newBacklogRemoveCmd(),
	)
	return cmd
}

func newBacklogListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <board>",
		Short: "List issues currently in a board backlog",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			boardID := resolveBoardIdentifier(args[0])
			jql, _ := cmd.Flags().GetString("jql")
			all, _ := cmd.Flags().GetBool("all")
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			fields, _ := cmd.Flags().GetString("fields")

			items := make([]map[string]any, 0)
			total := 0
			if all {
				if pageSize <= 0 {
					pageSize = 50
				}
				offset := start
				for {
					page, err := client.GetBacklogIssues(boardID, jql, pageSize, offset, fields, "")
					if err != nil {
						return err
					}
					raw, _ := page["issues"].([]any)
					pageItems := coerceJSONArray(raw)
					total = intFromAny(page["total"], total)
					items = append(items, pageItems...)
					offset += len(pageItems)
					if len(pageItems) == 0 || (total > 0 && offset >= total) {
						break
					}
				}
			} else {
				page, err := client.GetBacklogIssues(boardID, jql, limit, start, fields, "")
				if err != nil {
					return err
				}
				raw, _ := page["issues"].([]any)
				items = coerceJSONArray(raw)
				total = intFromAny(page["total"], len(items))
			}

			target := map[string]any{"board": boardID}
			if strings.TrimSpace(jql) != "" {
				target["jql"] = jql
			}

			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "backlog.list", target, map[string]any{
					"issues": items,
					"summary": map[string]any{
						"count": len(items),
						"total": total,
					},
				}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d backlog issue(s) on board %s.\n", len(items), boardID)
				return nil
			}
			if len(items) == 0 {
				fmt.Println("No backlog issues found.")
				return nil
			}
			fmt.Printf("Backlog issues on board %s:\n\n", boardID)
			printIssueSearchRows(items)
			return nil
		},
	}
	cmd.Flags().String("jql", "", "Optional extra JQL filter applied within the backlog")
	cmd.Flags().String("fields", "summary,status,assignee,priority", "Fields to request")
	cmd.Flags().Bool("all", false, "Fetch all backlog issues")
	cmd.Flags().Int("limit", 20, "Maximum issues to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newBacklogMoveToCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "move-to <board> <issue> [issue...]",
		Short: "Move issues from backlog onto a board",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			boardID := resolveBoardIdentifier(args[0])
			issues := resolveIssueArgs(args[1:])
			return runBacklogMutation(cmd, "backlog.move-to", map[string]any{"board": boardID, "issues": issues}, func(client *Client, rankFieldID int) error {
				before, after := backlogRankFlags(cmd)
				return client.MoveIssuesToBoard(boardID, issues, before, after, rankFieldID)
			})
		},
	}
	addBacklogMutationFlags(cmd, true)
	return cmd
}

func newBacklogRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <issue> [issue...]",
		Short: "Move issues back into backlog",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			issues := resolveIssueArgs(args)
			boardID, _ := cmd.Flags().GetString("board")
			if strings.TrimSpace(boardID) != "" {
				boardID = resolveBoardIdentifier(boardID)
			}
			target := map[string]any{"issues": issues}
			if boardID != "" {
				target["board"] = boardID
			}
			return runBacklogMutation(cmd, "backlog.remove", target, func(client *Client, rankFieldID int) error {
				before, after := backlogRankFlags(cmd)
				if boardID != "" {
					return client.MoveIssuesToBacklogForBoard(boardID, issues, before, after, rankFieldID)
				}
				return client.MoveIssuesToBacklog(issues)
			})
		},
	}
	addBacklogMutationFlags(cmd, false)
	cmd.Flags().String("board", "", "Optional board id for board-specific backlog placement and ranking")
	return cmd
}

func addBacklogMutationFlags(cmd *cobra.Command, includeRanking bool) {
	if includeRanking {
		cmd.Flags().String("before", "", "Rank these issues before another issue on the target board")
		cmd.Flags().String("after", "", "Rank these issues after another issue on the target board")
		cmd.Flags().String("rank-field", "", "Optional Rank custom field id (for example: customfield_12345)")
	} else {
		cmd.Flags().String("before", "", "Rank these issues before another issue when used with --board")
		cmd.Flags().String("after", "", "Rank these issues after another issue when used with --board")
		cmd.Flags().String("rank-field", "", "Optional Rank custom field id (for example: customfield_12345)")
	}
	cmd.Flags().Bool("dry-run", false, "Preview backlog changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
}

func runBacklogMutation(cmd *cobra.Command, command string, target map[string]any, apply func(client *Client, rankFieldID int) error) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	boardID, _ := target["board"].(string)
	before, after := backlogRankFlags(cmd)
	if boardID == "" && (before != "" || after != "") {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Ranking backlog placement requires --board.", ExitCode: 2}
	}
	if before != "" && after != "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use only one of --before or --after.", ExitCode: 2}
	}

	issues := coerceIssueList(target["issues"])
	rankFieldFlag, _ := cmd.Flags().GetString("rank-field")
	rankFieldID, err := resolveRankCustomFieldID(client, firstIssue(issues), rankFieldFlag)
	if err != nil {
		return err
	}

	result := map[string]any{"issues": issues}
	if before != "" {
		result["rank_before_issue"] = before
	}
	if after != "" {
		result["rank_after_issue"] = after
	}
	if rankFieldID > 0 {
		result["rank_custom_field_id"] = rankFieldID
	}

	if dryRun {
		result["dry_run"] = true
		return printBacklogMutationResult(mode, command, target, result)
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return printBacklogMutationResult(mode, command, target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"})
	}
	if err := apply(client, rankFieldID); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.%s %s", command, strings.Join(issues, ",")))
	}
	result["updated"] = true
	return printBacklogMutationResult(mode, command, target, result)
}

func backlogRankFlags(cmd *cobra.Command) (string, string) {
	before, _ := cmd.Flags().GetString("before")
	after, _ := cmd.Flags().GetString("after")
	before = strings.TrimSpace(before)
	after = strings.TrimSpace(after)
	if before != "" {
		before = ResolveIssueIdentifier(before)
	}
	if after != "" {
		after = ResolveIssueIdentifier(after)
	}
	return before, after
}

func printBacklogMutationResult(mode, command string, target, result map[string]any) error {
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", command, target, result, nil, nil, "", "", "", nil))
	}
	issues := coerceIssueList(target["issues"])
	if mode == "summary" {
		if result["dry_run"] == true {
			fmt.Printf("Would update backlog placement for %d issue(s).\n", len(issues))
			return nil
		}
		if result["skipped"] == true {
			fmt.Println("Skipped duplicate backlog request.")
			return nil
		}
		fmt.Printf("Updated backlog placement for %d issue(s).\n", len(issues))
		return nil
	}
	if result["dry_run"] == true {
		fmt.Printf("Would update backlog placement for: %s\n", strings.Join(issues, ", "))
		return nil
	}
	if result["skipped"] == true {
		fmt.Println("Skipped duplicate backlog request.")
		return nil
	}
	fmt.Printf("Updated backlog placement for: %s\n", strings.Join(issues, ", "))
	return nil
}

func resolveIssueArgs(args []string) []string {
	issues := make([]string, 0, len(args))
	for _, arg := range args {
		issues = append(issues, ResolveIssueIdentifier(arg))
	}
	return issues
}

func firstIssue(issues []string) string {
	if len(issues) == 0 {
		return ""
	}
	return issues[0]
}
