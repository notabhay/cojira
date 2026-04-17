package jira

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDiffCmd creates the "diff" subcommand.
func NewDiffCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "diff <issue> [other-issue]",
		Short: "Show a changelog-based diff for a Jira issue or compare two issues",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runDiff,
	}
	cmd.Flags().String("history-id", "", "Single changelog history ID to diff")
	cmd.Flags().String("from-history", "", "Start changelog history ID")
	cmd.Flags().String("to-history", "", "End changelog history ID (defaults to the newest available change)")
	cmd.Flags().String("field", "", "Optional field filter (case-insensitive)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runDiff(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	historyID, _ := cmd.Flags().GetString("history-id")
	fromHistory, _ := cmd.Flags().GetString("from-history")
	toHistory, _ := cmd.Flags().GetString("to-history")
	fieldFilter, _ := cmd.Flags().GetString("field")
	fieldFilter = strings.TrimSpace(strings.ToLower(fieldFilter))

	if len(args) == 2 && historyID == "" && fromHistory == "" && toHistory == "" {
		return runIssueCompare(cmd, client, args[0], args[1], fieldFilter, mode)
	}

	if historyID != "" && fromHistory != "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --history-id or --from-history, not both.", ExitCode: 2}
	}
	if historyID == "" && fromHistory == "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Either --history-id or --from-history is required.", ExitCode: 2}
	}

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

	entries := summarizeHistories(rawHistories, fieldFilter)
	chronological := chronologicalHistoryEntries(entries)
	selected, selectedFrom, selectedTo, err := selectHistoryEntries(chronological, historyID, fromHistory, toHistory)
	if err != nil {
		return err
	}

	changes := aggregateHistoryChanges(selected)
	target := map[string]any{"issue": issueID}
	if selectedFrom != "" {
		target["from_history"] = selectedFrom
	}
	if selectedTo != "" && selectedTo != selectedFrom {
		target["to_history"] = selectedTo
	}
	if fieldFilter != "" {
		target["field"] = fieldFilter
	}

	result := map[string]any{
		"issue":        key,
		"summary":      summary,
		"from_history": selectedFrom,
		"to_history":   selectedTo,
		"changes":      changes,
		"summary_info": map[string]any{"count": len(changes)},
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "diff", target, result, nil, nil, "", "", "", nil))
	}

	if mode == "summary" {
		fmt.Printf("Compared %d field change(s) on %s.\n", len(changes), key)
		return nil
	}

	fmt.Printf("Diff for %s", key)
	if summary != "" {
		fmt.Printf(": %s", summary)
	}
	fmt.Println()
	if selectedFrom == selectedTo {
		fmt.Printf("History: %s\n", selectedFrom)
	} else {
		fmt.Printf("History range: %s -> %s\n", selectedFrom, selectedTo)
	}
	fmt.Println()

	if len(changes) == 0 {
		fmt.Println("No field changes found.")
		return nil
	}

	for _, change := range changes {
		fmt.Printf("%-16v %v -> %v", change["field"], change["from"], change["to"])
		if count := normalizeMaybeString(change["change_count"]); count != "" {
			fmt.Printf(" (%s change%s)", count, pluralSuffix(parseIntFallback(count), "", "s"))
		}
		fmt.Println()
	}
	return nil
}

func runIssueCompare(cmd *cobra.Command, client *Client, leftArg, rightArg, fieldFilter, mode string) error {
	leftID := ResolveIssueIdentifier(leftArg)
	rightID := ResolveIssueIdentifier(rightArg)
	fields := "summary,status,issuetype,assignee,reporter,priority,project,labels,components,fixVersions,versions,parent"
	leftIssue, err := client.GetIssue(leftID, fields, "")
	if err != nil {
		return err
	}
	rightIssue, err := client.GetIssue(rightID, fields, "")
	if err != nil {
		return err
	}
	recordSearchRecents(client, []map[string]any{leftIssue, rightIssue}, "diff")
	changes := compareIssueFields(leftIssue, rightIssue, fieldFilter)
	target := map[string]any{"left": leftID, "right": rightID}
	result := map[string]any{"left": summarizeIssueInfo(client, leftIssue), "right": summarizeIssueInfo(client, rightIssue), "changes": changes}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "diff", target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Compared %s and %s across %d field(s).\n", leftID, rightID, len(changes))
		return nil
	}
	fmt.Printf("Issue diff: %s vs %s\n\n", leftID, rightID)
	if len(changes) == 0 {
		fmt.Println("No differences found.")
		return nil
	}
	for _, change := range changes {
		fmt.Printf("%-14s %v -> %v\n", change["field"], change["left"], change["right"])
	}
	return nil
}

func chronologicalHistoryEntries(entries []map[string]any) []map[string]any {
	if len(entries) <= 1 {
		return entries
	}
	first := parseHistoryTime(entries[0])
	last := parseHistoryTime(entries[len(entries)-1])
	if !first.IsZero() && !last.IsZero() && first.After(last) {
		reversed := make([]map[string]any, 0, len(entries))
		for i := len(entries) - 1; i >= 0; i-- {
			reversed = append(reversed, entries[i])
		}
		return reversed
	}
	return entries
}

func parseHistoryTime(entry map[string]any) time.Time {
	created := normalizeMaybeString(entry["created"])
	if created == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02T15:04:05.000-0700"} {
		if parsed, err := time.Parse(layout, created); err == nil {
			return parsed
		}
	}
	return time.Time{}
}

func selectHistoryEntries(entries []map[string]any, historyID, fromHistory, toHistory string) ([]map[string]any, string, string, error) {
	if len(entries) == 0 {
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "No changelog history is available for this issue.", ExitCode: 1}
	}

	if historyID != "" {
		for _, entry := range entries {
			if normalizeMaybeString(entry["id"]) == historyID {
				return []map[string]any{entry}, historyID, historyID, nil
			}
		}
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("History ID %s was not found.", historyID), ExitCode: 1}
	}

	fromIndex := -1
	toIndex := -1
	for idx, entry := range entries {
		entryID := normalizeMaybeString(entry["id"])
		if entryID == fromHistory {
			fromIndex = idx
		}
		if toHistory != "" && entryID == toHistory {
			toIndex = idx
		}
	}
	if fromIndex < 0 {
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("History ID %s was not found.", fromHistory), ExitCode: 1}
	}
	if toHistory == "" {
		toIndex = len(entries) - 1
		toHistory = normalizeMaybeString(entries[toIndex]["id"])
	}
	if toIndex < 0 {
		return nil, "", "", &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("History ID %s was not found.", toHistory), ExitCode: 1}
	}

	if fromIndex > toIndex {
		fromIndex, toIndex = toIndex, fromIndex
		fromHistory = normalizeMaybeString(entries[fromIndex]["id"])
		toHistory = normalizeMaybeString(entries[toIndex]["id"])
	}

	selected := append([]map[string]any(nil), entries[fromIndex:toIndex+1]...)
	return selected, fromHistory, toHistory, nil
}

func aggregateHistoryChanges(entries []map[string]any) []map[string]any {
	ordered := make([]map[string]any, 0)
	byField := map[string]map[string]any{}

	for _, entry := range entries {
		rawChanges, _ := entry["changes"].([]map[string]any)
		for _, change := range rawChanges {
			field := normalizeMaybeString(change["field"])
			if field == "" {
				continue
			}
			item, ok := byField[field]
			if !ok {
				item = map[string]any{
					"field":         field,
					"from":          change["from"],
					"to":            change["to"],
					"change_count":  1,
					"first_history": entry["id"],
					"last_history":  entry["id"],
				}
				byField[field] = item
				ordered = append(ordered, item)
				continue
			}
			item["to"] = change["to"]
			item["last_history"] = entry["id"]
			item["change_count"] = item["change_count"].(int) + 1
		}
	}

	return ordered
}

func parseIntFallback(value string) int {
	var parsed int
	_, _ = fmt.Sscanf(value, "%d", &parsed)
	if parsed <= 0 {
		return 0
	}
	return parsed
}

func compareIssueFields(leftIssue, rightIssue map[string]any, fieldFilter string) []map[string]any {
	left := summarizeIssueInfo(nil, leftIssue)
	right := summarizeIssueInfo(nil, rightIssue)
	fields := []string{"summary", "status", "type", "assignee", "reporter", "priority", "project", "labels", "components", "fixVersions", "versions"}
	changes := make([]map[string]any, 0, len(fields))
	for _, field := range fields {
		if fieldFilter != "" && !strings.Contains(strings.ToLower(field), fieldFilter) {
			continue
		}
		leftJSON, _ := jsonMarshalString(left[field])
		rightJSON, _ := jsonMarshalString(right[field])
		if leftJSON == rightJSON {
			continue
		}
		changes = append(changes, map[string]any{
			"field": field,
			"left":  left[field],
			"right": right[field],
		})
	}
	return changes
}

func jsonMarshalString(value any) (string, error) {
	body, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(body), nil
}
