package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewWatchersCmd creates the "watchers" subcommand.
func NewWatchersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "watchers <issue>",
		Short: "List, add, or remove Jira watchers",
		Args:  cobra.ExactArgs(1),
		RunE:  runWatchers,
	}
	cmd.Flags().String("add", "", "User to add as a watcher")
	cmd.Flags().String("remove", "", "User to remove as a watcher")
	cmd.Flags().Bool("dry-run", false, "Preview watcher changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runWatchers(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	addRef, _ := cmd.Flags().GetString("add")
	removeRef, _ := cmd.Flags().GetString("remove")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if addRef != "" && removeRef != "" {
		return fmt.Errorf("use either --add or --remove, not both")
	}

	if addRef == "" && removeRef == "" {
		data, err := client.GetWatchers(issueID)
		if err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "watchers",
				map[string]any{"issue": issueID},
				data, nil, nil, "", "", "", nil,
			))
		}
		count := intFromAny(data["watchCount"], 0)
		if mode == "summary" {
			fmt.Printf("Found %d watcher(s) on %s.\n", count, issueID)
			return nil
		}
		fmt.Printf("Watchers on %s (%d):\n\n", issueID, count)
		watchers := coerceJSONArray(getMapArray(data, "watchers"))
		if len(watchers) == 0 {
			fmt.Println("No watchers found.")
			return nil
		}
		rows := make([][]string, 0, len(watchers))
		for _, watcher := range watchers {
			rows = append(rows, []string{
				output.Truncate(formatUserDisplay(watcher), 32),
				output.Truncate(normalizeMaybeString(watcher["emailAddress"]), 28),
				output.Truncate(stringOr(watcher["accountId"], normalizeMaybeString(watcher["name"])), 28),
			})
		}
		fmt.Println(output.TableString([]string{"DISPLAY", "EMAIL", "ACCOUNT"}, rows))
		return nil
	}

	action := "add"
	userRef := addRef
	if removeRef != "" {
		action = "remove"
		userRef = removeRef
	}

	user, err := resolveUserReference(client, userRef)
	if err != nil {
		return err
	}
	watcherValue, removeKey := watcherReferenceForAPI(user)
	target := map[string]any{"issue": issueID, "user": userRef}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "watchers",
				target,
				map[string]any{"dry_run": true, "action": action, "watcher": user},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would %s watcher %s on %s.\n", action, formatUserDisplay(user), issueID)
			return nil
		}
		fmt.Printf("Would %s watcher %s on %s.\n", action, formatUserDisplay(user), issueID)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "watchers",
				target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Printf("Skipped duplicate watcher %s for %s.\n", action, issueID)
		return nil
	}

	switch action {
	case "add":
		err = client.AddWatcher(issueID, watcherValue)
	case "remove":
		err = client.RemoveWatcher(issueID, removeKey, watcherValue)
	}
	if err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.watchers %s %s", action, issueID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "watchers",
			target,
			map[string]any{"updated": true, "action": action, "watcher": user},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("%sed watcher %s on %s.\n", capitalize(action), formatUserDisplay(user), issueID)
		return nil
	}
	fmt.Printf("%sed watcher %s on %s.\n", capitalize(action), formatUserDisplay(user), issueID)
	return nil
}

func watcherReferenceForAPI(user map[string]any) (value string, removeKey string) {
	for _, key := range []string{"name", "accountId", "key"} {
		if v := normalizeMaybeString(user[key]); v != "" {
			switch key {
			case "accountId":
				return v, "accountId"
			default:
				return v, "username"
			}
		}
	}
	if v := normalizeMaybeString(user["emailAddress"]); v != "" {
		return v, "username"
	}
	return "", "username"
}

func normalizeMaybeString(v any) string {
	text := fmt.Sprintf("%v", v)
	if text == "" || text == "<nil>" {
		return ""
	}
	return text
}

func getMapArray(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	arr, _ := m[key].([]any)
	return arr
}

func capitalize(s string) string {
	if s == "" {
		return s
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
