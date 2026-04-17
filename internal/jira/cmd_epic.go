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

// NewEpicCmd creates the "epic" command group.
func NewEpicCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "epic",
		Short: "Inspect and manage Jira epic relationships",
	}
	cmd.AddCommand(
		newEpicListCmd(),
		newEpicChildrenCmd(),
		newEpicAddCmd(),
		newEpicRemoveCmd(),
	)
	return cmd
}

func newEpicListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List visible epic issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			project, _ := cmd.Flags().GetString("project")
			extraJQL, _ := cmd.Flags().GetString("jql")
			all, _ := cmd.Flags().GetBool("all")
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			pageSize, _ := cmd.Flags().GetInt("page-size")
			fields, _ := cmd.Flags().GetString("fields")

			jqlParts := []string{"issuetype = Epic"}
			if strings.TrimSpace(project) != "" {
				jqlParts = append(jqlParts, fmt.Sprintf("project = %s", project))
			}
			if strings.TrimSpace(extraJQL) != "" {
				jqlParts = append(jqlParts, fmt.Sprintf("(%s)", extraJQL))
			}
			jql := strings.Join(jqlParts, " AND ")

			items, total, err := searchAllIssues(client, jql, limit, start, pageSize, fields, "", all)
			if err != nil {
				return err
			}

			target := map[string]any{"jql": jql}
			if strings.TrimSpace(project) != "" {
				target["project"] = project
			}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "epic.list", target, map[string]any{
					"epics": items,
					"summary": map[string]any{
						"count": len(items),
						"total": total,
					},
				}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d epic(s).\n", len(items))
				return nil
			}
			if len(items) == 0 {
				fmt.Println("No epics found.")
				return nil
			}
			fmt.Println("Epics:")
			fmt.Println()
			printIssueSearchRows(items)
			return nil
		},
	}
	cmd.Flags().String("project", "", "Optional project key filter")
	cmd.Flags().String("jql", "", "Optional extra JQL filter")
	cmd.Flags().String("fields", "summary,status,assignee,priority", "Fields to request")
	cmd.Flags().Bool("all", false, "Fetch all epics")
	cmd.Flags().Int("limit", 20, "Maximum epics to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newEpicChildrenCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "children <epic>",
		Short: "List issues assigned to an epic",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			epicID := ResolveIssueIdentifier(args[0])
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
					page, err := client.GetEpicIssues(epicID, jql, pageSize, offset, fields, "")
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
				page, err := client.GetEpicIssues(epicID, jql, limit, start, fields, "")
				if err != nil {
					return err
				}
				raw, _ := page["issues"].([]any)
				items = coerceJSONArray(raw)
				total = intFromAny(page["total"], len(items))
			}

			target := map[string]any{"epic": epicID}
			if strings.TrimSpace(jql) != "" {
				target["jql"] = jql
			}

			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "epic.children", target, map[string]any{
					"issues": items,
					"summary": map[string]any{
						"count": len(items),
						"total": total,
					},
				}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d child issue(s) in epic %s.\n", len(items), epicID)
				return nil
			}
			if len(items) == 0 {
				fmt.Println("No epic child issues found.")
				return nil
			}
			fmt.Printf("Epic children for %s:\n\n", epicID)
			printIssueSearchRows(items)
			return nil
		},
	}
	cmd.Flags().String("jql", "", "Optional extra JQL filter")
	cmd.Flags().String("fields", "summary,status,assignee,priority", "Fields to request")
	cmd.Flags().Bool("all", false, "Fetch all child issues")
	cmd.Flags().Int("limit", 20, "Maximum issues to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newEpicAddCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add <epic> <issue> [issue...]",
		Short: "Assign one or more issues to an epic",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			epicID := ResolveIssueIdentifier(args[0])
			issues := resolveIssueArgs(args[1:])
			return runEpicMutation(cmd, "epic.add", map[string]any{"epic": epicID, "issues": issues}, func(client *Client) error {
				return client.MoveIssuesToEpic(epicID, issues)
			})
		},
	}
	addEpicMutationFlags(cmd)
	return cmd
}

func newEpicRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <issue> [issue...]",
		Short: "Remove one or more issues from their current epic",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			issues := resolveIssueArgs(args)
			return runEpicMutation(cmd, "epic.remove", map[string]any{"issues": issues}, func(client *Client) error {
				return client.RemoveIssuesFromEpic(issues)
			})
		},
	}
	addEpicMutationFlags(cmd)
	return cmd
}

func addEpicMutationFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("dry-run", false, "Preview epic changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
}

func runEpicMutation(cmd *cobra.Command, command string, target map[string]any, apply func(client *Client) error) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	issues := coerceIssueList(target["issues"])

	if len(issues) == 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide at least one issue.", ExitCode: 2}
	}

	result := map[string]any{"issues": issues}
	if epicID, ok := target["epic"].(string); ok && epicID != "" {
		result["epic"] = epicID
	}

	if dryRun {
		result["dry_run"] = true
		return printEpicMutationResult(mode, command, target, result)
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return printEpicMutationResult(mode, command, target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"})
	}
	if err := apply(client); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.%s %s", command, strings.Join(issues, ",")))
	}
	result["updated"] = true
	return printEpicMutationResult(mode, command, target, result)
}

func printEpicMutationResult(mode, command string, target, result map[string]any) error {
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", command, target, result, nil, nil, "", "", "", nil))
	}
	issues := coerceIssueList(target["issues"])
	if mode == "summary" {
		if result["dry_run"] == true {
			fmt.Printf("Would update epic assignment for %d issue(s).\n", len(issues))
			return nil
		}
		if result["skipped"] == true {
			fmt.Println("Skipped duplicate epic request.")
			return nil
		}
		fmt.Printf("Updated epic assignment for %d issue(s).\n", len(issues))
		return nil
	}
	if result["dry_run"] == true {
		fmt.Printf("Would update epic assignment for: %s\n", strings.Join(issues, ", "))
		return nil
	}
	if result["skipped"] == true {
		fmt.Println("Skipped duplicate epic request.")
		return nil
	}
	fmt.Printf("Updated epic assignment for: %s\n", strings.Join(issues, ", "))
	return nil
}
