package jira

import (
	"fmt"

	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewTransitionsCmd creates the "transitions" subcommand.
func NewTransitionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transitions <issue>",
		Short: "List available transitions",
		Long:  "List transitions or auto-pick a transition ID for a target status.",
		Args:  cobra.ExactArgs(1),
		RunE:  runTransitions,
	}
	cmd.Flags().String("to", "", "Target status name (case-insensitive)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runTransitions(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	toFlag, _ := cmd.Flags().GetString("to")

	data, err := client.ListTransitions(issueID)
	if err != nil {
		return err
	}

	transitions, _ := data["transitions"].([]any)
	if len(transitions) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "transitions",
				map[string]any{"issue": issueID},
				map[string]any{"transitions": []any{}},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("No transitions found for %s.\n", issueID)
			return nil
		}
		fmt.Println("No transitions found.")
		return nil
	}

	if toFlag != "" {
		matches := filterTransitionsByStatus(transitions, toFlag)

		if len(matches) == 0 {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.TransitionNotFound,
					fmt.Sprintf("No transitions to status %q found for %s.", toFlag, issueID),
					"", "", map[string]any{
						"action": "run", "command": fmt.Sprintf("cojira jira transitions %s --output-mode json", issueID),
						"requires_user": false,
					})
				return output.PrintJSON(output.BuildEnvelope(
					false, "jira", "transitions",
					map[string]any{"issue": issueID, "to": toFlag},
					map[string]any{"transitions": []any{}},
					nil, []any{errObj}, "", "", "", nil,
				))
			}
			if mode == "summary" {
				fmt.Printf("No transition to status %q found for %s.\n", toFlag, issueID)
				return &cerrors.CojiraError{Code: cerrors.TransitionNotFound, ExitCode: 1}
			}
			fmt.Fprintf(cmd.ErrOrStderr(), "No transitions to status '%s' found for %s\n", toFlag, issueID)
			return &cerrors.CojiraError{Code: cerrors.TransitionNotFound, ExitCode: 1}
		}

		if mode == "json" {
			var warnings []any
			if len(matches) > 1 {
				warnObj, _ := output.ErrorObj(cerrors.AmbiguousTransition,
					fmt.Sprintf("Multiple transitions found for status %q; using first match.", toFlag),
					"", "", map[string]any{
						"action": "run", "command": fmt.Sprintf("cojira jira transitions %s --output-mode json", issueID),
						"requires_user": false,
					})
				warnings = []any{warnObj}
			}
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "transitions",
				map[string]any{"issue": issueID, "to": toFlag},
				map[string]any{"transitions": matches},
				warnings, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			first := matches[0].(map[string]any)
			fmt.Printf("Transition ID for %s -> %s: %v\n", issueID, toFlag, first["id"])
			return nil
		}
		if len(matches) > 1 {
			fmt.Fprintf(cmd.ErrOrStderr(), "Multiple transitions found for status '%s', using the first match.\n", toFlag)
		}
		first := matches[0].(map[string]any)
		fmt.Println(first["id"])
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "transitions",
			map[string]any{"issue": issueID},
			data, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Found %d transition(s) for %s.\n", len(transitions), issueID)
		return nil
	}

	fmt.Printf("Available transitions for %s:\n", issueID)
	for _, t := range transitions {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		toStatus := safeString(tm, "to", "name")
		fmt.Printf("  %6v  %v -> %s\n", tm["id"], tm["name"], toStatus)
	}
	return nil
}
