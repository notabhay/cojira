package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

type boardColumn struct {
	Name     string           `json:"name"`
	Statuses map[string]bool  `json:"-"`
	Items    []map[string]any `json:"items"`
}

// NewBoardViewCmd creates the "board-view" subcommand.
func NewBoardViewCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board-view <board>",
		Short: "Render a Jira board as columns with issue cards",
		Args:  cobra.ExactArgs(1),
		RunE:  runBoardView,
	}
	cmd.Flags().String("jql", "", "Additional JQL filter")
	cmd.Flags().Int("limit", 50, "Max results per page (default: 50)")
	cmd.Flags().Int("start", 0, "Start offset (default: 0)")
	cmd.Flags().Bool("all", false, "Fetch all issues on the board (paginate)")
	cmd.Flags().Int("max-issues", 500, "Safety cap for --all (default: 500; set 0 for unlimited)")
	cmd.Flags().Int("limit-per-column", 8, "Maximum cards to render per column in human mode")
	cmd.Flags().String("format", "columns", "Human output format: columns or list")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runBoardView(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	boardID := resolveBoardIdentifier(args[0])
	jqlFlag, _ := cmd.Flags().GetString("jql")
	if jqlFlag != "" {
		jqlFlag = FixJQLShellEscapes(jqlFlag)
	}
	pageSize, _ := cmd.Flags().GetInt("limit")
	startAt, _ := cmd.Flags().GetInt("start")
	fetchAll, _ := cmd.Flags().GetBool("all")
	maxIssues, _ := cmd.Flags().GetInt("max-issues")
	limitPerColumn, _ := cmd.Flags().GetInt("limit-per-column")
	formatFlag, _ := cmd.Flags().GetString("format")

	config, err := client.GetBoardConfiguration(boardID)
	if err != nil {
		return err
	}

	issues, total, truncated, err := collectBoardIssues(client, boardID, jqlFlag, pageSize, startAt, fetchAll, maxIssues)
	if err != nil {
		return err
	}

	boardName := normalizeMaybeString(config["name"])
	if boardName == "" {
		boardName = boardID
	}

	columns := buildBoardColumns(config, issues)
	target := map[string]any{"board": boardID}
	if jqlFlag != "" {
		target["jql"] = jqlFlag
	}

	if mode == "json" {
		resultColumns := make([]map[string]any, 0, len(columns))
		for _, column := range columns {
			resultColumns = append(resultColumns, map[string]any{
				"name":  column.Name,
				"items": column.Items,
				"count": len(column.Items),
			})
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "board-view",
			target,
			map[string]any{
				"board": map[string]any{
					"id":   boardID,
					"name": boardName,
				},
				"columns": resultColumns,
				"summary": map[string]any{
					"columns":   len(columns),
					"issues":    len(issues),
					"total":     total,
					"truncated": truncated,
				},
			},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Board %s: %d columns, %d issue(s).\n", boardName, len(columns), len(issues))
		return nil
	}

	if formatFlag == "list" {
		printBoardViewList(boardName, boardID, columns, total, truncated, limitPerColumn)
		return nil
	}

	printBoardViewColumns(boardName, boardID, columns, total, truncated, limitPerColumn)
	return nil
}

func collectBoardIssues(client *Client, boardID, jql string, pageSize, startAt int, fetchAll bool, maxIssues int) ([]map[string]any, int, bool, error) {
	fields := "summary,status,assignee,priority,issuetype,labels"
	var issues []map[string]any
	var total int
	truncated := false
	curStart := startAt

	for {
		page, err := client.GetBoardIssues(boardID, jql, pageSize, curStart, fields)
		if err != nil {
			return nil, 0, false, err
		}
		rawIssues, _ := page["issues"].([]any)
		for _, raw := range rawIssues {
			if issue, ok := raw.(map[string]any); ok {
				issues = append(issues, summarizeBoardIssue(issue))
			}
		}
		if total == 0 {
			total = intFromAny(page["total"], len(rawIssues))
		}
		curStart += len(rawIssues)
		if !fetchAll {
			break
		}
		if maxIssues > 0 && len(issues) >= maxIssues {
			truncated = true
			if len(issues) > maxIssues {
				issues = issues[:maxIssues]
			}
			break
		}
		if curStart >= total || len(rawIssues) == 0 {
			break
		}
	}

	return issues, total, truncated, nil
}

func summarizeBoardIssue(issue map[string]any) map[string]any {
	fields, _ := issue["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	status, _ := fields["status"].(map[string]any)
	priority, _ := fields["priority"].(map[string]any)

	return map[string]any{
		"key":         issue["key"],
		"summary":     normalizeMaybeString(fields["summary"]),
		"status":      safeString(fields, "status", "name"),
		"status_id":   normalizeMaybeString(status["id"]),
		"assignee":    safeString(fields, "assignee", "displayName"),
		"priority":    safeString(fields, "priority", "name"),
		"priority_id": normalizeMaybeString(priority["id"]),
		"labels":      safeStringSlice(fields, "labels"),
	}
}

func buildBoardColumns(config map[string]any, issues []map[string]any) []boardColumn {
	columnConfig, _ := config["columnConfig"].(map[string]any)
	rawColumns, _ := columnConfig["columns"].([]any)

	columns := make([]boardColumn, 0, len(rawColumns)+1)
	statusToColumn := map[string]int{}

	for _, raw := range rawColumns {
		column, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		built := boardColumn{
			Name:     normalizeMaybeString(column["name"]),
			Statuses: map[string]bool{},
			Items:    []map[string]any{},
		}
		rawStatuses, _ := column["statuses"].([]any)
		for _, rawStatus := range rawStatuses {
			status, ok := rawStatus.(map[string]any)
			if !ok {
				continue
			}
			statusID := normalizeMaybeString(status["id"])
			if statusID == "" {
				continue
			}
			built.Statuses[statusID] = true
			statusToColumn[statusID] = len(columns)
		}
		columns = append(columns, built)
	}

	unmappedIndex := -1
	for _, issue := range issues {
		statusID := normalizeMaybeString(issue["status_id"])
		idx, ok := statusToColumn[statusID]
		if !ok {
			if unmappedIndex < 0 {
				columns = append(columns, boardColumn{Name: "Unmapped", Statuses: map[string]bool{}, Items: []map[string]any{}})
				unmappedIndex = len(columns) - 1
			}
			idx = unmappedIndex
		}
		columns[idx].Items = append(columns[idx].Items, issue)
	}

	return columns
}

func printBoardViewList(boardName, boardID string, columns []boardColumn, total int, truncated bool, limitPerColumn int) {
	fmt.Printf("Board %s (%s)\n\n", boardName, boardID)
	for _, column := range columns {
		fmt.Printf("%s (%d)\n", column.Name, len(column.Items))
		if len(column.Items) == 0 {
			fmt.Println("  (empty)")
			fmt.Println()
			continue
		}
		visible, overflow := limitIssues(column.Items, limitPerColumn)
		for _, item := range visible {
			fmt.Printf("  - %s %s", item["key"], item["summary"])
			if assignee := normalizeMaybeString(item["assignee"]); assignee != "" {
				fmt.Printf(" [%s]", assignee)
			}
			fmt.Println()
		}
		if overflow > 0 {
			fmt.Printf("  ... %d more\n", overflow)
		}
		fmt.Println()
	}
	if truncated {
		fmt.Printf("Showing %d of %d issue(s); stopped early.\n", countBoardIssues(columns), total)
	}
}

func printBoardViewColumns(boardName, boardID string, columns []boardColumn, total int, truncated bool, limitPerColumn int) {
	const colWidth = 30
	const gap = "  "

	fmt.Printf("Board %s (%s)\n", boardName, boardID)
	if truncated {
		fmt.Printf("Showing %d of %d issue(s); stopped early.\n", countBoardIssues(columns), total)
	}
	fmt.Println()

	headers := make([]string, 0, len(columns))
	separators := make([]string, 0, len(columns))
	rendered := make([][]string, 0, len(columns))
	maxRows := 0

	for _, column := range columns {
		headers = append(headers, padBoardCell(fmt.Sprintf("%s (%d)", column.Name, len(column.Items)), colWidth))
		separators = append(separators, strings.Repeat("-", colWidth))
		visible, overflow := limitIssues(column.Items, limitPerColumn)
		lines := make([]string, 0, len(visible)*3+2)
		if len(visible) == 0 {
			lines = append(lines, padBoardCell("(empty)", colWidth))
		}
		for _, item := range visible {
			lines = append(lines, wrapBoardCell(fmt.Sprintf("%s [%s]", item["key"], stringOr(item["priority"], "-")), colWidth)...)
			lines = append(lines, wrapBoardCell(normalizeMaybeString(item["summary"]), colWidth)...)
			assignee := normalizeMaybeString(item["assignee"])
			if assignee == "" {
				assignee = "Unassigned"
			}
			lines = append(lines, wrapBoardCell("-> "+assignee, colWidth)...)
			lines = append(lines, padBoardCell("", colWidth))
		}
		if overflow > 0 {
			lines = append(lines, padBoardCell(fmt.Sprintf("... %d more", overflow), colWidth))
		}
		if len(lines) > maxRows {
			maxRows = len(lines)
		}
		rendered = append(rendered, lines)
	}

	fmt.Println(strings.Join(headers, gap))
	fmt.Println(strings.Join(separators, gap))
	for row := 0; row < maxRows; row++ {
		cells := make([]string, 0, len(rendered))
		for _, lines := range rendered {
			if row < len(lines) {
				cells = append(cells, padBoardCell(lines[row], colWidth))
			} else {
				cells = append(cells, strings.Repeat(" ", colWidth))
			}
		}
		fmt.Println(strings.Join(cells, gap))
	}
}

func limitIssues(items []map[string]any, limit int) ([]map[string]any, int) {
	if limit <= 0 || len(items) <= limit {
		return items, 0
	}
	return items[:limit], len(items) - limit
}

func countBoardIssues(columns []boardColumn) int {
	total := 0
	for _, column := range columns {
		total += len(column.Items)
	}
	return total
}

func wrapBoardCell(text string, width int) []string {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		return []string{padBoardCell("", width)}
	}
	words := strings.Fields(trimmed)
	if len(words) == 0 {
		return []string{padBoardCell("", width)}
	}
	lines := []string{}
	current := ""
	for _, word := range words {
		candidate := word
		if current != "" {
			candidate = current + " " + word
		}
		if len(candidate) <= width {
			current = candidate
			continue
		}
		if current != "" {
			lines = append(lines, padBoardCell(current, width))
			current = word
			continue
		}
		lines = append(lines, padBoardCell(word[:minInt(len(word), width)], width))
		if len(word) > width {
			current = word[width:]
		}
	}
	if current != "" {
		for len(current) > width {
			lines = append(lines, padBoardCell(current[:width], width))
			current = current[width:]
		}
		lines = append(lines, padBoardCell(current, width))
	}
	return lines
}

func padBoardCell(text string, width int) string {
	trimmed := strings.TrimSpace(text)
	if len(trimmed) > width {
		if width <= 3 {
			return trimmed[:width]
		}
		return trimmed[:width-3] + "..."
	}
	if len(trimmed) < width {
		return trimmed + strings.Repeat(" ", width-len(trimmed))
	}
	return trimmed
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}
