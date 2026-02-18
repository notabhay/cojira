package jira

import (
	"fmt"
	"strings"

	"github.com/cojira/cojira/internal/cli"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/idempotency"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewTransitionCmd creates the "transition" subcommand.
func NewTransitionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "transition <issue> [transition-id]",
		Short: "Transition issue",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runTransition,
	}
	cmd.Flags().String("to", "", "Target status name (case-insensitive)")
	cmd.Flags().String("payload", "", "JSON file with extra fields/update payload")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview transition without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runTransition(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	var transitionArg string
	if len(args) > 1 {
		transitionArg = args[1]
	}
	toFlag, _ := cmd.Flags().GetString("to")
	payloadFile, _ := cmd.Flags().GetString("payload")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")

	if transitionArg != "" && toFlag != "" {
		msg := "Use either a transition ID or --to, not both."
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, msg, "", "", nil)
			ec := 2
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "transition",
				map[string]any{"issue": issueID},
				nil, nil, []any{errObj}, "", "", "", &ec,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", msg)
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: msg, ExitCode: 2}
	}
	if transitionArg == "" && toFlag == "" {
		msg := "Missing transition. Provide a transition ID or --to \"Status\"."
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, msg, "", "", nil)
			ec := 2
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "transition",
				map[string]any{"issue": issueID},
				nil, nil, []any{errObj}, "", "", "", &ec,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", msg)
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: msg, ExitCode: 2}
	}

	issue, err := client.GetIssue(issueID, "status", "")
	if err != nil {
		return err
	}
	fd, _ := issue["fields"].(map[string]any)
	fromStatus := safeString(fd, "status", "name")

	var transitionID string
	var toStatus string
	var warnings []any

	if toFlag != "" {
		// Check if already in target status.
		if fromStatus != "" && strings.EqualFold(strings.TrimSpace(fromStatus), strings.TrimSpace(toFlag)) {
			receipt := output.Receipt{OK: true, DryRun: dryRun, Message: fmt.Sprintf("No-op for %s: already in status %s", issueID, fromStatus)}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "jira", "transition",
					map[string]any{"issue": issueID, "to": toFlag},
					map[string]any{
						"dry_run":       dryRun,
						"changed":       false,
						"from_status":   fromStatus,
						"to_status":     fromStatus,
						"transition_id": nil,
						"receipt":       receipt.Format(),
					},
					nil, nil, "", "", "", nil,
				))
			}
			if mode == "summary" {
				fmt.Printf("No-op: %s already in status %s.\n", issueID, fromStatus)
				return nil
			}
			if !quiet {
				fmt.Println(receipt.Format())
			}
			return nil
		}

		data, err := client.ListTransitions(issueID)
		if err != nil {
			return err
		}
		transitions, _ := data["transitions"].([]any)
		matches := filterTransitionsByStatus(transitions, toFlag)

		if len(matches) == 0 {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.TransitionNotFound,
					fmt.Sprintf("No transition to status %q found for %s.", toFlag, issueID),
					"", "", map[string]any{
						"action": "run", "command": fmt.Sprintf("cojira jira transitions %s --output-mode json", issueID),
						"requires_user": false,
					})
				return output.PrintJSON(output.BuildEnvelope(
					false, "jira", "transition",
					map[string]any{"issue": issueID, "to": toFlag},
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: No transitions to status %q found for %s\n", toFlag, issueID)
			return &cerrors.CojiraError{Code: cerrors.TransitionNotFound, ExitCode: 1}
		}

		first := matches[0].(map[string]any)
		transitionID = fmt.Sprintf("%v", first["id"])
		toStatus = safeString(first, "to", "name")

		if len(matches) > 1 {
			warnMsg := fmt.Sprintf("Multiple transitions match status '%s'; using first: %s", toFlag, transitionID)
			warnObj, _ := output.ErrorObj(cerrors.AmbiguousTransition, warnMsg, "", "", map[string]any{
				"action": "run", "command": fmt.Sprintf("cojira jira transitions %s --output-mode json", issueID),
				"requires_user": false,
			})
			warnings = []any{warnObj}
			if !quiet && mode != "json" {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Warning: Multiple transitions found for status %q, using the first match.\n", toFlag)
			}
		}
	} else {
		transitionID = transitionArg
	}

	payload := map[string]any{"transition": map[string]any{"id": transitionID}}
	if payloadFile != "" {
		extra, err := readJSONFile(payloadFile)
		if err != nil {
			return err
		}
		for k, v := range extra {
			payload[k] = v
		}
		payload["transition"] = map[string]any{"id": transitionID}
	}

	if dryRun {
		toDisplay := toStatus
		if toDisplay == "" {
			toDisplay = toFlag
		}
		if toDisplay == "" {
			toDisplay = "?"
		}
		receipt := output.Receipt{
			OK: true, DryRun: true,
			Message: fmt.Sprintf("Would transition %s: %s -> %s (transition %s)", issueID, fromStatus, toDisplay, transitionID),
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "transition",
				map[string]any{"issue": issueID},
				map[string]any{
					"dry_run":       true,
					"from_status":   fromStatus,
					"to_status":     stringOr(toStatus, toFlag),
					"transition_id": transitionID,
					"receipt":       receipt.Format(),
					"idempotency":   map[string]any{"key": output.IdempotencyKey("jira.transition", issueID, transitionID, toFlag)},
				},
				warnings, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would transition %s: %s -> %s.\n", issueID, fromStatus, toDisplay)
			return nil
		}
		if !quiet {
			fmt.Println(receipt.Format())
		}
		return nil
	}

	if idemKey != "" {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "jira", "transition",
					map[string]any{"issue": issueID},
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	if err := client.TransitionIssue(issueID, payload, !noNotify); err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.transition %s", issueID))
	}

	issue2, err := client.GetIssue(issueID, "status", "")
	if err != nil {
		return err
	}
	fd2, _ := issue2["fields"].(map[string]any)
	newStatus := safeString(fd2, "status", "name")

	receipt := output.Receipt{
		OK:      true,
		Message: fmt.Sprintf("Transitioned %s: %s -> %s (transition %s)", issueID, fromStatus, newStatus, transitionID),
		Changes: []output.Change{{Field: "status", OldValue: fromStatus, NewValue: newStatus}},
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "transition",
			map[string]any{"issue": issueID},
			map[string]any{
				"from_status":   fromStatus,
				"to_status":     newStatus,
				"transition_id": transitionID,
				"receipt":       receipt.Format(),
				"idempotency":   map[string]any{"key": output.IdempotencyKey("jira.transition", issueID, transitionID, newStatus)},
			},
			warnings, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Transitioned %s: %s -> %s.\n", issueID, fromStatus, newStatus)
		return nil
	}
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}

// filterTransitionsByStatus filters transitions by target status name (case-insensitive).
func filterTransitionsByStatus(transitions []any, targetStatus string) []any {
	target := strings.TrimSpace(strings.ToLower(targetStatus))
	var matches []any
	for _, t := range transitions {
		tm, ok := t.(map[string]any)
		if !ok {
			continue
		}
		toName := safeString(tm, "to", "name")
		if strings.ToLower(toName) == target {
			matches = append(matches, t)
		}
	}
	return matches
}
