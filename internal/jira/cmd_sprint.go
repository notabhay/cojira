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

// NewSprintCmd creates the "sprint" command group.
func NewSprintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sprint",
		Short: "Manage Jira sprints",
	}
	cmd.AddCommand(
		newSprintListCmd(),
		newSprintGetCmd(),
		newSprintCreateCmd(),
		newSprintUpdateCmd(),
		newSprintStartCmd(),
		newSprintCompleteCmd(),
		newSprintDeleteCmd(),
		newSprintAddIssuesCmd(),
	)
	return cmd
}

func newSprintListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list <board>",
		Short: "List sprints on a board",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			boardID := resolveBoardIdentifier(args[0])
			state, _ := cmd.Flags().GetString("state")
			all, _ := cmd.Flags().GetBool("all")
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			pageSize, _ := cmd.Flags().GetInt("page-size")

			items := make([]map[string]any, 0)
			total := 0
			if all {
				if pageSize <= 0 {
					pageSize = 50
				}
				offset := start
				for {
					data, err := client.ListBoardSprints(boardID, state, pageSize, offset)
					if err != nil {
						return err
					}
					raw, _ := data["values"].([]any)
					pageItems := coerceJSONArray(raw)
					total = intFromAny(data["total"], total)
					items = append(items, pageItems...)
					offset += len(pageItems)
					if len(pageItems) == 0 || (total > 0 && offset >= total) {
						break
					}
				}
			} else {
				data, err := client.ListBoardSprints(boardID, state, limit, start)
				if err != nil {
					return err
				}
				raw, _ := data["values"].([]any)
				items = coerceJSONArray(raw)
				total = intFromAny(data["total"], len(items))
			}

			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.list", map[string]any{"board": boardID}, map[string]any{"sprints": items, "summary": map[string]any{"count": len(items), "total": total}}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d sprint(s) on board %s.\n", len(items), boardID)
				return nil
			}
			if len(items) == 0 {
				fmt.Println("No sprints found.")
				return nil
			}
			fmt.Printf("Sprints on board %s:\n\n", boardID)
			for _, sprint := range items {
				fmt.Printf("  %-10v %-10v %v\n", sprint["id"], sprint["state"], sprint["name"])
			}
			return nil
		},
	}
	cmd.Flags().String("state", "", "Optional sprint state filter")
	cmd.Flags().Bool("all", false, "Fetch all sprints")
	cmd.Flags().Int("limit", 20, "Maximum sprints to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newSprintGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <sprint>",
		Short: "Fetch sprint metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			sprintID := strings.TrimSpace(args[0])
			sprint, err := client.GetSprint(sprintID)
			if err != nil {
				return err
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.get", map[string]any{"sprint": sprintID}, sprint, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Sprint %s: %v (%v)\n", sprintID, sprint["name"], sprint["state"])
				return nil
			}
			fmt.Printf("Sprint %s\n", sprintID)
			for _, key := range []string{"name", "state", "goal", "startDate", "endDate", "completeDate", "originBoardId"} {
				if v := normalizeMaybeString(sprint[key]); v != "" {
					fmt.Printf("  %-12s %s\n", key+":", v)
				}
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newSprintCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <board> <name>",
		Short: "Create a sprint on a board",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			boardID := resolveBoardIdentifier(args[0])
			payload := sprintPayloadFromFlags(cmd)
			payload["name"] = args[1]
			payload["originBoardId"] = boardID
			return runSprintMutation(cmd, "sprint.create", map[string]any{"board": boardID}, payload, func(client *Client) (map[string]any, error) {
				return client.CreateSprint(payload)
			})
		},
	}
	addSprintMutationFlags(cmd)
	return cmd
}

func newSprintUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <sprint>",
		Short: "Update sprint metadata",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sprintID := strings.TrimSpace(args[0])
			payload := sprintPayloadFromFlags(cmd)
			if len(payload) == 0 {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide at least one sprint field to update.", ExitCode: 2}
			}
			return runSprintMutation(cmd, "sprint.update", map[string]any{"sprint": sprintID}, payload, func(client *Client) (map[string]any, error) {
				return client.UpdateSprint(sprintID, payload)
			})
		},
	}
	addSprintMutationFlags(cmd)
	return cmd
}

func newSprintStartCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "start <sprint>",
		Short: "Start a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sprintID := strings.TrimSpace(args[0])
			payload := sprintPayloadFromFlags(cmd)
			payload["state"] = "active"
			return runSprintMutation(cmd, "sprint.start", map[string]any{"sprint": sprintID}, payload, func(client *Client) (map[string]any, error) {
				return client.UpdateSprint(sprintID, payload)
			})
		},
	}
	addSprintMutationFlags(cmd)
	return cmd
}

func newSprintCompleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "complete <sprint>",
		Short: "Complete a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sprintID := strings.TrimSpace(args[0])
			payload := sprintPayloadFromFlags(cmd)
			payload["state"] = "closed"
			return runSprintMutation(cmd, "sprint.complete", map[string]any{"sprint": sprintID}, payload, func(client *Client) (map[string]any, error) {
				return client.UpdateSprint(sprintID, payload)
			})
		},
	}
	addSprintMutationFlags(cmd)
	return cmd
}

func newSprintDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <sprint>",
		Short: "Delete a sprint",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			sprintID := strings.TrimSpace(args[0])
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			yes, _ := cmd.Flags().GetBool("yes")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			target := map[string]any{"sprint": sprintID}
			if dryRun {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.delete", target, map[string]any{"dry_run": true}, nil, nil, "", "", "", nil))
				}
				if mode == "summary" {
					fmt.Printf("Would delete sprint %s.\n", sprintID)
					return nil
				}
				fmt.Printf("Would delete sprint %s.\n", sprintID)
				return nil
			}
			if !yes {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Sprint deletion is destructive. Preview with --dry-run first, then rerun with --yes.", ExitCode: 2}
			}
			if idemKey != "" && idempotency.IsDuplicate(idemKey) {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.delete", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
				}
				fmt.Printf("Skipped duplicate sprint delete for %s.\n", sprintID)
				return nil
			}
			if err := client.DeleteSprint(sprintID); err != nil {
				return err
			}
			if idemKey != "" {
				_ = idempotency.Record(idemKey, fmt.Sprintf("jira.sprint.delete %s", sprintID))
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.delete", target, map[string]any{"deleted": true}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Deleted sprint %s.\n", sprintID)
				return nil
			}
			fmt.Printf("Deleted sprint %s.\n", sprintID)
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "Preview deletion without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm destructive deletion")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func newSprintAddIssuesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "add-issues <sprint> <issue> [issue...]",
		Short: "Assign issues to a sprint",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			cli.ApplyPlanFlag(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			sprintID := strings.TrimSpace(args[0])
			issues := make([]string, 0, len(args)-1)
			for _, issue := range args[1:] {
				issues = append(issues, ResolveIssueIdentifier(issue))
			}
			dryRun, _ := cmd.Flags().GetBool("dry-run")
			idemKey, _ := cmd.Flags().GetString("idempotency-key")
			target := map[string]any{"sprint": sprintID}
			if dryRun {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.add-issues", target, map[string]any{"dry_run": true, "issues": issues}, nil, nil, "", "", "", nil))
				}
				if mode == "summary" {
					fmt.Printf("Would add %d issue(s) to sprint %s.\n", len(issues), sprintID)
					return nil
				}
				fmt.Printf("Would add %d issue(s) to sprint %s.\n", len(issues), sprintID)
				return nil
			}
			if idemKey != "" && idempotency.IsDuplicate(idemKey) {
				if mode == "json" {
					return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.add-issues", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
				}
				fmt.Printf("Skipped duplicate sprint add-issues for %s.\n", sprintID)
				return nil
			}
			if err := client.AddIssuesToSprint(sprintID, issues); err != nil {
				return err
			}
			if idemKey != "" {
				_ = idempotency.Record(idemKey, fmt.Sprintf("jira.sprint.add-issues %s", sprintID))
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "sprint.add-issues", target, map[string]any{"updated": true, "issues": issues}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Added %d issue(s) to sprint %s.\n", len(issues), sprintID)
				return nil
			}
			fmt.Printf("Added %d issue(s) to sprint %s.\n", len(issues), sprintID)
			return nil
		},
	}
	cmd.Flags().Bool("dry-run", false, "Preview add-issues without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func addSprintMutationFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Sprint name")
	cmd.Flags().String("goal", "", "Sprint goal")
	cmd.Flags().String("start-date", "", "Sprint start date/time")
	cmd.Flags().String("end-date", "", "Sprint end date/time")
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
}

func sprintPayloadFromFlags(cmd *cobra.Command) map[string]any {
	payload := map[string]any{}
	name, _ := cmd.Flags().GetString("name")
	goal, _ := cmd.Flags().GetString("goal")
	startDate, _ := cmd.Flags().GetString("start-date")
	endDate, _ := cmd.Flags().GetString("end-date")
	if strings.TrimSpace(name) != "" {
		payload["name"] = name
	}
	if strings.TrimSpace(goal) != "" {
		payload["goal"] = goal
	}
	if strings.TrimSpace(startDate) != "" {
		payload["startDate"] = startDate
	}
	if strings.TrimSpace(endDate) != "" {
		payload["endDate"] = endDate
	}
	return payload
}

func runSprintMutation(cmd *cobra.Command, command string, target map[string]any, payload map[string]any, apply func(client *Client) (map[string]any, error)) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", command, target, map[string]any{"dry_run": true, "payload": payload}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would apply %s.\n", command)
			return nil
		}
		fmt.Printf("Would apply %s.\n", command)
		return nil
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", command, target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate %s.\n", command)
		return nil
	}
	result, err := apply(client)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, command)
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", command, target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Applied %s.\n", command)
		return nil
	}
	fmt.Printf("Applied %s.\n", command)
	return nil
}
