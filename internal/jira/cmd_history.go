package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewHistoryCmd creates the "history" subcommand.
func NewHistoryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "history <issue>",
		Short: "Show Jira issue changelog history",
		Args:  cobra.ExactArgs(1),
		RunE:  runHistory,
	}
	cmd.Flags().Int("limit", 20, "Maximum history entries to return")
	cmd.Flags().String("field", "", "Optional field filter (case-insensitive)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runHistory(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	limit, _ := cmd.Flags().GetInt("limit")
	fieldFilter, _ := cmd.Flags().GetString("field")
	fieldFilter = strings.TrimSpace(strings.ToLower(fieldFilter))

	issue, err := client.GetIssue(issueID, "summary,status", "changelog")
	if err != nil {
		return err
	}

	key := normalizeMaybeString(issue["key"])
	if key == "" {
		key = issueID
	}
	fields, _ := issue["fields"].(map[string]any)
	summary := normalizeMaybeString(fields["summary"])
	changelog, _ := issue["changelog"].(map[string]any)
	rawHistories, _ := changelog["histories"].([]any)

	items := summarizeHistories(rawHistories, fieldFilter)
	if limit > 0 && len(items) > limit {
		items = items[:limit]
	}

	target := map[string]any{"issue": issueID}
	if fieldFilter != "" {
		target["field"] = fieldFilter
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "history",
			target,
			map[string]any{
				"issue":   key,
				"summary": summary,
				"history": items,
				"summary_info": map[string]any{
					"count": len(items),
				},
			},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Found %d history entr%s on %s.\n", len(items), pluralSuffix(len(items), "y", "ies"), key)
		return nil
	}

	if len(items) == 0 {
		fmt.Println("No history entries found.")
		return nil
	}

	fmt.Printf("History for %s", key)
	if summary != "" {
		fmt.Printf(": %s", summary)
	}
	fmt.Println()
	fmt.Println()
	rows := make([][]string, 0, len(items))
	for _, entry := range items {
		changes, _ := entry["changes"].([]map[string]any)
		rows = append(rows, []string{
			normalizeMaybeString(entry["id"]),
			formatHumanTimestamp(normalizeMaybeString(entry["created"])),
			output.Truncate(normalizeMaybeString(entry["author"]), 24),
			output.Truncate(summarizeHistoryChangesHuman(changes), 96),
		})
	}
	fmt.Println(output.TableString([]string{"ID", "WHEN", "AUTHOR", "CHANGES"}, rows))
	return nil
}

func pluralSuffix(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}
	return plural
}

func summarizeHistories(rawHistories []any, fieldFilter string) []map[string]any {
	items := make([]map[string]any, 0, len(rawHistories))
	for _, raw := range rawHistories {
		history, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		rawChanges, _ := history["items"].([]any)
		changes := make([]map[string]any, 0, len(rawChanges))
		for _, rawChange := range rawChanges {
			change, ok := rawChange.(map[string]any)
			if !ok {
				continue
			}
			field := normalizeMaybeString(change["field"])
			if fieldFilter != "" && strings.ToLower(field) != fieldFilter {
				continue
			}
			changes = append(changes, map[string]any{
				"field": field,
				"from":  stringOr(normalizeMaybeString(change["fromString"]), normalizeMaybeString(change["from"])),
				"to":    stringOr(normalizeMaybeString(change["toString"]), normalizeMaybeString(change["to"])),
			})
		}
		if len(changes) == 0 {
			continue
		}
		author, _ := history["author"].(map[string]any)
		items = append(items, map[string]any{
			"id":      history["id"],
			"created": history["created"],
			"author":  formatUserDisplay(author),
			"changes": changes,
		})
	}
	return items
}

func summarizeHistoryChangesHuman(changes []map[string]any) string {
	parts := make([]string, 0, len(changes))
	for _, change := range changes {
		parts = append(parts, fmt.Sprintf(
			"%s: %s -> %s",
			normalizeMaybeString(change["field"]),
			compactWhitespace(normalizeMaybeString(change["from"])),
			compactWhitespace(normalizeMaybeString(change["to"])),
		))
	}
	return strings.Join(parts, "; ")
}
