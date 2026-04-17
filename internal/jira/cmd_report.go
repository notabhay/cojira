package jira

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

type reportSprintSummary struct {
	Sprint           map[string]any   `json:"sprint"`
	StoryPointsField map[string]any   `json:"story_points_field,omitempty"`
	IssueCount       int              `json:"issue_count"`
	DoneCount        int              `json:"done_count"`
	OpenCount        int              `json:"open_count"`
	DonePercent      float64          `json:"done_percent"`
	StoryPointsTotal float64          `json:"story_points_total,omitempty"`
	StoryPointsDone  float64          `json:"story_points_done,omitempty"`
	StoryPointsOpen  float64          `json:"story_points_open,omitempty"`
	Statuses         []map[string]any `json:"statuses"`
	Assignees        []map[string]any `json:"assignees"`
	Issues           []map[string]any `json:"issues,omitempty"`
}

type velocityEntry struct {
	Sprint           map[string]any `json:"sprint"`
	IssueCount       int            `json:"issue_count"`
	DoneCount        int            `json:"done_count"`
	StoryPointsTotal float64        `json:"story_points_total,omitempty"`
	StoryPointsDone  float64        `json:"story_points_done,omitempty"`
	DonePercent      float64        `json:"done_percent"`
}

type burndownPoint struct {
	Date      string  `json:"date"`
	Remaining float64 `json:"remaining"`
	Ideal     float64 `json:"ideal"`
	Done      float64 `json:"done"`
}

type cycleTimeEntry struct {
	Key     string  `json:"key"`
	Summary string  `json:"summary"`
	StartAt string  `json:"start_at,omitempty"`
	DoneAt  string  `json:"done_at,omitempty"`
	Hours   float64 `json:"hours"`
	Days    float64 `json:"days"`
}

type blockerAgingEntry struct {
	Key         string   `json:"key"`
	Summary     string   `json:"summary"`
	Status      string   `json:"status"`
	Blockers    []string `json:"blockers"`
	BlockedFrom string   `json:"blocked_from,omitempty"`
	AgeHours    float64  `json:"age_hours"`
	AgeDays     float64  `json:"age_days"`
}

type workloadEntry struct {
	Assignee         string  `json:"assignee"`
	IssueCount       int     `json:"issue_count"`
	DoneCount        int     `json:"done_count"`
	OpenCount        int     `json:"open_count"`
	StoryPointsTotal float64 `json:"story_points_total,omitempty"`
	StoryPointsDone  float64 `json:"story_points_done,omitempty"`
}

// NewReportCmd creates the "report" command group.
func NewReportCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "report",
		Short: "Board and sprint reporting helpers",
	}
	cmd.AddCommand(
		newReportSprintCmd(),
		newReportVelocityCmd(),
		newReportBurndownCmd(),
		newReportCycleTimeCmd(),
		newReportBlockerAgingCmd(),
		newReportWorkloadCmd(),
	)
	return cmd
}

func newReportSprintCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sprint <board>",
		Short: "Summarize a sprint on a board",
		Args:  cobra.ExactArgs(1),
		RunE:  runReportSprint,
	}
	addReportSelectionFlags(cmd)
	cmd.Flags().Bool("include-issues", false, "Include issue summaries in JSON output")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runReportSprint(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	boardID := resolveBoardIdentifier(args[0])
	storyField, err := detectStoryPointsField(client, cmd)
	if err != nil {
		return err
	}

	sprint, err := resolveReportSprint(client, boardID, cmd, true)
	if err != nil {
		return err
	}
	includeIssues, _ := cmd.Flags().GetBool("include-issues")
	extraJQL := normalizedReportJQL(cmd)
	issues, _, err := collectReportIssues(client, fmt.Sprintf("sprint = %s%s", normalizeMaybeString(sprint["id"]), appendExtraJQL(extraJQL)), storyField)
	if err != nil {
		return err
	}

	summary := summarizeSprintIssues(sprint, issues, storyField, includeIssues)
	target := map[string]any{"board": boardID, "sprint": normalizeMaybeString(sprint["id"])}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "report.sprint", target, summary, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		if summary.StoryPointsField != nil {
			fmt.Printf("Sprint %s: %d issue(s), %.0f%% done, %.1f / %.1f points done.\n", normalizeMaybeString(sprint["name"]), summary.IssueCount, summary.DonePercent, summary.StoryPointsDone, summary.StoryPointsTotal)
			return nil
		}
		fmt.Printf("Sprint %s: %d issue(s), %.0f%% done.\n", normalizeMaybeString(sprint["name"]), summary.IssueCount, summary.DonePercent)
		return nil
	}

	printSprintSummaryHuman(summary)
	return nil
}

func newReportVelocityCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "velocity <board>",
		Short: "Show recent sprint velocity for a board",
		Args:  cobra.ExactArgs(1),
		RunE:  runReportVelocity,
	}
	cmd.Flags().Int("count", 5, "Number of closed sprints to include")
	cmd.Flags().String("jql", "", "Extra JQL filter applied within each sprint")
	cmd.Flags().String("story-points-field", "", "Story points field ID or name (use auto, none, or customfield_12345)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runReportVelocity(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	boardID := resolveBoardIdentifier(args[0])
	count, _ := cmd.Flags().GetInt("count")
	if count <= 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--count must be greater than zero", ExitCode: 2}
	}
	storyField, err := detectStoryPointsField(client, cmd)
	if err != nil {
		return err
	}

	sprints, err := collectClosedSprints(client, boardID, count)
	if err != nil {
		return err
	}
	extraJQL := normalizedReportJQL(cmd)
	entries := make([]velocityEntry, 0, len(sprints))
	for _, sprint := range sprints {
		issues, _, err := collectReportIssues(client, fmt.Sprintf("sprint = %s%s", normalizeMaybeString(sprint["id"]), appendExtraJQL(extraJQL)), storyField)
		if err != nil {
			return err
		}
		summary := summarizeSprintIssues(sprint, issues, storyField, false)
		entries = append(entries, velocityEntry{
			Sprint:           sprint,
			IssueCount:       summary.IssueCount,
			DoneCount:        summary.DoneCount,
			StoryPointsTotal: summary.StoryPointsTotal,
			StoryPointsDone:  summary.StoryPointsDone,
			DonePercent:      summary.DonePercent,
		})
	}

	result := buildVelocityResult(boardID, storyField, entries)
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "report.velocity", map[string]any{"board": boardID}, result, nil, nil, "", "", "", nil))
	}
	if len(entries) == 0 {
		if mode == "summary" {
			fmt.Printf("No closed sprints found on board %s.\n", boardID)
			return nil
		}
		printVelocityHuman(boardID, storyField, entries)
		return nil
	}
	if mode == "summary" {
		summary := result["summary"].(map[string]any)
		if storyField != nil {
			fmt.Printf("Velocity for board %s across %d sprint(s): avg %.1f completed points.\n", boardID, len(entries), summary["avg_story_points_done"])
			return nil
		}
		fmt.Printf("Velocity for board %s across %d sprint(s): avg %.1f completed issue(s).\n", boardID, len(entries), summary["avg_done_issues"])
		return nil
	}

	printVelocityHuman(boardID, storyField, entries)
	return nil
}

func newReportBurndownCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "burndown <board>",
		Short: "Estimate burndown across the selected sprint",
		Args:  cobra.ExactArgs(1),
		RunE:  runReportBurndown,
	}
	addReportSelectionFlags(cmd)
	cmd.Flags().Bool("use-issue-count", false, "Use issue count instead of story points")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runReportBurndown(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	boardID := resolveBoardIdentifier(args[0])
	useIssueCount, _ := cmd.Flags().GetBool("use-issue-count")
	storyField, err := detectStoryPointsField(client, cmd)
	if err != nil {
		return err
	}
	if useIssueCount {
		storyField = nil
	}
	sprint, err := resolveReportSprint(client, boardID, cmd, true)
	if err != nil {
		return err
	}
	rawIssues, err := collectReportIssueSnapshots(client, sprintIssueJQL(sprint, normalizedReportJQL(cmd)), storyField, true)
	if err != nil {
		return err
	}
	points := buildBurndownPoints(sprint, rawIssues, storyField, useIssueCount)
	result := map[string]any{
		"sprint":             sprint,
		"story_points_field": storyField,
		"use_issue_count":    useIssueCount,
		"points":             points,
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "report.burndown", map[string]any{"board": boardID, "sprint": normalizeMaybeString(sprint["id"])}, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Burndown for sprint %s has %d point(s).\n", normalizeMaybeString(sprint["name"]), len(points))
		return nil
	}
	printBurndownHuman(sprint, storyField, useIssueCount, points)
	return nil
}

func newReportCycleTimeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "cycle-time <board>",
		Short: "Estimate cycle time for completed issues in the selected sprint",
		Args:  cobra.ExactArgs(1),
		RunE:  runReportCycleTime,
	}
	addReportSelectionFlags(cmd)
	cmd.Flags().Bool("include-issues", false, "Include issue-level entries in JSON output")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runReportCycleTime(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	boardID := resolveBoardIdentifier(args[0])
	sprint, err := resolveReportSprint(client, boardID, cmd, true)
	if err != nil {
		return err
	}
	rawIssues, err := collectReportIssueSnapshots(client, sprintIssueJQL(sprint, normalizedReportJQL(cmd)), nil, true)
	if err != nil {
		return err
	}
	entries, summary := buildCycleTimeReport(rawIssues)
	result := map[string]any{"sprint": sprint, "summary": summary}
	includeIssues, _ := cmd.Flags().GetBool("include-issues")
	if includeIssues {
		result["issues"] = entries
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "report.cycle-time", map[string]any{"board": boardID, "sprint": normalizeMaybeString(sprint["id"])}, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Cycle time for sprint %s averages %.1f days across %d completed issue(s).\n", normalizeMaybeString(sprint["name"]), summary["avg_days"], summary["completed_issues"])
		return nil
	}
	printCycleTimeHuman(sprint, summary, entries)
	return nil
}

func newReportBlockerAgingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blocker-aging <board>",
		Short: "List currently blocked issues in the selected sprint and their age",
		Args:  cobra.ExactArgs(1),
		RunE:  runReportBlockerAging,
	}
	addReportSelectionFlags(cmd)
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runReportBlockerAging(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	boardID := resolveBoardIdentifier(args[0])
	sprint, err := resolveReportSprint(client, boardID, cmd, true)
	if err != nil {
		return err
	}
	rawIssues, err := collectReportIssueSnapshots(client, sprintIssueJQL(sprint, normalizedReportJQL(cmd)), nil, true)
	if err != nil {
		return err
	}
	items := buildBlockerAgingReport(rawIssues)
	result := map[string]any{
		"sprint":  sprint,
		"summary": map[string]any{"blocked_issues": len(items)},
		"issues":  items,
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "report.blocker-aging", map[string]any{"board": boardID, "sprint": normalizeMaybeString(sprint["id"])}, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Sprint %s has %d blocked issue(s).\n", normalizeMaybeString(sprint["name"]), len(items))
		return nil
	}
	printBlockerAgingHuman(sprint, items)
	return nil
}

func newReportWorkloadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workload <board>",
		Short: "Summarize assignee workload in the selected sprint",
		Args:  cobra.ExactArgs(1),
		RunE:  runReportWorkload,
	}
	addReportSelectionFlags(cmd)
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runReportWorkload(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	boardID := resolveBoardIdentifier(args[0])
	storyField, err := detectStoryPointsField(client, cmd)
	if err != nil {
		return err
	}
	sprint, err := resolveReportSprint(client, boardID, cmd, true)
	if err != nil {
		return err
	}
	issues, _, err := collectReportIssues(client, sprintIssueJQL(sprint, normalizedReportJQL(cmd)), storyField)
	if err != nil {
		return err
	}
	items := buildWorkloadEntries(issues)
	result := map[string]any{"sprint": sprint, "story_points_field": storyField, "assignees": items}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "report.workload", map[string]any{"board": boardID, "sprint": normalizeMaybeString(sprint["id"])}, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Workload for sprint %s covers %d assignee bucket(s).\n", normalizeMaybeString(sprint["name"]), len(items))
		return nil
	}
	printWorkloadHuman(sprint, storyField, items)
	return nil
}

func addReportSelectionFlags(cmd *cobra.Command) {
	cmd.Flags().String("sprint", "", "Explicit sprint ID")
	cmd.Flags().String("state", "auto", "Sprint selector: auto, active, future, or closed")
	cmd.Flags().String("jql", "", "Extra JQL filter applied within the sprint")
	cmd.Flags().String("story-points-field", "", "Story points field ID or name (use auto, none, or customfield_12345)")
}

func normalizedReportJQL(cmd *cobra.Command) string {
	jql, _ := cmd.Flags().GetString("jql")
	return strings.TrimSpace(FixJQLShellEscapes(jql))
}

func appendExtraJQL(extra string) string {
	if strings.TrimSpace(extra) == "" {
		return ""
	}
	return fmt.Sprintf(" AND (%s)", strings.TrimSpace(extra))
}

func resolveReportSprint(client *Client, boardID string, cmd *cobra.Command, closedAllowed bool) (map[string]any, error) {
	explicit, _ := cmd.Flags().GetString("sprint")
	state, _ := cmd.Flags().GetString("state")
	state = strings.ToLower(strings.TrimSpace(state))
	if explicit != "" {
		return client.GetSprint(strings.TrimSpace(explicit))
	}
	sprints, err := collectBoardSprints(client, boardID)
	if err != nil {
		return nil, err
	}
	selected := selectSprintForReport(sprints, state, closedAllowed)
	if selected == nil {
		return nil, &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: "No matching sprint found for this board.", ExitCode: 1}
	}
	return selected, nil
}

func collectBoardSprints(client *Client, boardID string) ([]map[string]any, error) {
	offset := 0
	items := []map[string]any{}
	for {
		resp, err := client.ListBoardSprints(boardID, "", 50, offset)
		if err != nil {
			return nil, err
		}
		raw, _ := resp["values"].([]any)
		pageItems := coerceJSONArray(raw)
		items = append(items, pageItems...)
		offset += len(pageItems)
		total := intFromAny(resp["total"], len(items))
		if len(pageItems) == 0 || (total > 0 && offset >= total) {
			break
		}
	}
	return preferBoardOriginSprints(boardID, items), nil
}

func collectClosedSprints(client *Client, boardID string, count int) ([]map[string]any, error) {
	sprints, err := collectBoardSprints(client, boardID)
	if err != nil {
		return nil, err
	}
	closed := make([]map[string]any, 0, len(sprints))
	for _, sprint := range sprints {
		if strings.EqualFold(normalizeMaybeString(sprint["state"]), "closed") {
			closed = append(closed, sprint)
		}
	}
	closed = preferNonBacklogSprints(closed)
	sort.SliceStable(closed, func(i, j int) bool {
		return sprintSortKey(closed[i]) > sprintSortKey(closed[j])
	})
	if len(closed) > count {
		closed = closed[:count]
	}
	return closed, nil
}

func selectSprintForReport(sprints []map[string]any, state string, closedAllowed bool) map[string]any {
	byState := map[string][]map[string]any{
		"active": {},
		"future": {},
		"closed": {},
	}
	for _, sprint := range sprints {
		s := strings.ToLower(normalizeMaybeString(sprint["state"]))
		if _, ok := byState[s]; ok {
			byState[s] = append(byState[s], sprint)
		}
	}
	for _, key := range []string{"active", "future", "closed"} {
		if key == "closed" {
			byState[key] = preferNonBacklogSprints(byState[key])
		}
		sort.SliceStable(byState[key], func(i, j int) bool {
			return sprintSortKey(byState[key][i]) > sprintSortKey(byState[key][j])
		})
	}

	switch state {
	case "", "auto":
		if len(byState["active"]) > 0 {
			return byState["active"][0]
		}
		if len(byState["future"]) > 0 {
			return byState["future"][0]
		}
		if closedAllowed && len(byState["closed"]) > 0 {
			return byState["closed"][0]
		}
	case "active", "future":
		if len(byState[state]) > 0 {
			return byState[state][0]
		}
	case "closed":
		if len(byState["closed"]) > 0 {
			return byState["closed"][0]
		}
	}
	return nil
}

func sprintSortKey(sprint map[string]any) string {
	for _, key := range []string{"completeDate", "endDate", "startDate", "createdDate"} {
		if v := normalizeMaybeString(sprint[key]); v != "" {
			return v
		}
	}
	return normalizeMaybeString(sprint["id"])
}

func detectStoryPointsField(client *Client, cmd *cobra.Command) (map[string]any, error) {
	flag, _ := cmd.Flags().GetString("story-points-field")
	flag = strings.TrimSpace(flag)
	switch strings.ToLower(flag) {
	case "none":
		return nil, nil
	case "":
	case "auto":
		flag = ""
	default:
		if strings.HasPrefix(strings.ToLower(flag), "customfield_") {
			return map[string]any{"id": flag, "name": flag}, nil
		}
	}

	fields, err := client.ListFields()
	if err != nil {
		return nil, err
	}
	if flag != "" {
		for _, field := range fields {
			id := normalizeMaybeString(field["id"])
			name := normalizeMaybeString(field["name"])
			if strings.EqualFold(id, flag) || strings.EqualFold(name, flag) {
				return map[string]any{"id": id, "name": name}, nil
			}
		}
		return nil, &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Story points field %q was not found.", flag), ExitCode: 1}
	}

	candidates := []string{
		"Story Points",
		"Story point estimate",
		"Story Points Estimate",
	}
	for _, candidate := range candidates {
		for _, field := range fields {
			if !strings.EqualFold(normalizeMaybeString(field["name"]), candidate) {
				continue
			}
			return map[string]any{"id": normalizeMaybeString(field["id"]), "name": normalizeMaybeString(field["name"])}, nil
		}
	}
	return nil, nil
}

func collectReportIssues(client *Client, jql string, storyField map[string]any) ([]map[string]any, int, error) {
	fields := []string{"summary", "status", "assignee", "priority", "resolution"}
	if storyField != nil {
		fields = append(fields, normalizeMaybeString(storyField["id"]))
	}
	pageSize := 100
	start := 0
	var issues []map[string]any
	total := 0
	for {
		page, err := client.Search(jql, pageSize, start, strings.Join(fields, ","), "")
		if err != nil {
			return nil, 0, err
		}
		rawIssues, _ := page["issues"].([]any)
		for _, raw := range rawIssues {
			if issue, ok := raw.(map[string]any); ok {
				issues = append(issues, summarizeReportIssue(issue, storyField))
			}
		}
		if total == 0 {
			total = intFromAny(page["total"], len(rawIssues))
		}
		start += len(rawIssues)
		if len(rawIssues) == 0 || (total > 0 && start >= total) {
			break
		}
	}
	return issues, total, nil
}

func collectReportIssueSnapshots(client *Client, jql string, storyField map[string]any, withChangelog bool) ([]map[string]any, error) {
	fields := []string{"summary", "status", "assignee", "priority", "resolution", "resolutiondate", "created", "updated", "issuelinks"}
	if storyField != nil {
		fields = append(fields, normalizeMaybeString(storyField["id"]))
	}
	pageSize := 100
	start := 0
	expand := ""
	if withChangelog {
		expand = "changelog"
	}
	var issues []map[string]any
	for {
		page, err := client.Search(jql, pageSize, start, strings.Join(fields, ","), expand)
		if err != nil {
			return nil, err
		}
		rawIssues, _ := page["issues"].([]any)
		pageItems := coerceJSONArray(rawIssues)
		issues = append(issues, pageItems...)
		total := intFromAny(page["total"], len(pageItems))
		start += len(pageItems)
		if len(pageItems) == 0 || (total > 0 && start >= total) {
			break
		}
	}
	return issues, nil
}

func sprintIssueJQL(sprint map[string]any, extraJQL string) string {
	return fmt.Sprintf("sprint = %s%s", normalizeMaybeString(sprint["id"]), appendExtraJQL(extraJQL))
}

func summarizeReportIssue(issue map[string]any, storyField map[string]any) map[string]any {
	fields, _ := issue["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}
	status, _ := fields["status"].(map[string]any)
	summary := map[string]any{
		"key":             issue["key"],
		"summary":         normalizeMaybeString(fields["summary"]),
		"status":          safeString(fields, "status", "name"),
		"status_category": safeString(status, "statusCategory", "name"),
		"assignee":        safeString(fields, "assignee", "displayName"),
		"priority":        safeString(fields, "priority", "name"),
		"done":            isDoneStatus(fields),
	}
	if storyField != nil {
		fieldID := normalizeMaybeString(storyField["id"])
		if points, ok := numberFromAny(fields[fieldID]); ok {
			summary["story_points"] = points
		}
	}
	return summary
}

func buildBurndownPoints(sprint map[string]any, issues []map[string]any, storyField map[string]any, useIssueCount bool) []burndownPoint {
	start, end := sprintBounds(sprint)
	if start.IsZero() {
		start = time.Now().UTC().Truncate(24 * time.Hour)
	}
	if end.IsZero() || end.Before(start) {
		end = start
	}
	totalUnits := 0.0
	doneTimes := make([]time.Time, 0, len(issues))
	units := make([]float64, 0, len(issues))
	for _, issue := range issues {
		unit := 1.0
		if !useIssueCount && storyField != nil {
			fields, _ := issue["fields"].(map[string]any)
			if points, ok := numberFromAny(fields[normalizeMaybeString(storyField["id"])]); ok && points > 0 {
				unit = points
			}
		}
		totalUnits += unit
		units = append(units, unit)
		doneTimes = append(doneTimes, issueDoneTime(issue))
	}
	series := make([]burndownPoint, 0)
	dayCount := int(end.Sub(start).Hours()/24) + 1
	if dayCount < 1 {
		dayCount = 1
	}
	for idx := 0; idx < dayCount; idx++ {
		day := start.Add(time.Duration(idx) * 24 * time.Hour)
		cutoff := day.Add(24*time.Hour - time.Nanosecond)
		remaining := 0.0
		done := 0.0
		for i, doneAt := range doneTimes {
			if doneAt.IsZero() || doneAt.After(cutoff) {
				remaining += units[i]
				continue
			}
			done += units[i]
		}
		ideal := totalUnits
		if dayCount > 1 {
			ideal = totalUnits - (totalUnits * float64(idx) / float64(dayCount-1))
		}
		if ideal < 0 {
			ideal = 0
		}
		series = append(series, burndownPoint{
			Date:      day.Format("2006-01-02"),
			Remaining: remaining,
			Ideal:     ideal,
			Done:      done,
		})
	}
	return series
}

func buildCycleTimeReport(issues []map[string]any) ([]cycleTimeEntry, map[string]any) {
	entries := make([]cycleTimeEntry, 0)
	totalHours := 0.0
	for _, issue := range issues {
		start := issueStartTime(issue)
		done := issueDoneTime(issue)
		if start.IsZero() || done.IsZero() || done.Before(start) {
			continue
		}
		hours := done.Sub(start).Hours()
		fields, _ := issue["fields"].(map[string]any)
		entries = append(entries, cycleTimeEntry{
			Key:     normalizeMaybeString(issue["key"]),
			Summary: normalizeMaybeString(fields["summary"]),
			StartAt: start.Format(time.RFC3339),
			DoneAt:  done.Format(time.RFC3339),
			Hours:   hours,
			Days:    hours / 24,
		})
		totalHours += hours
	}
	sort.SliceStable(entries, func(i, j int) bool { return entries[i].Hours > entries[j].Hours })
	summary := map[string]any{
		"completed_issues": len(entries),
		"avg_hours":        averageFloat(totalHours, len(entries)),
		"avg_days":         averageFloat(totalHours/24, len(entries)),
	}
	return entries, summary
}

func buildBlockerAgingReport(issues []map[string]any) []blockerAgingEntry {
	now := time.Now().UTC()
	items := make([]blockerAgingEntry, 0)
	for _, issue := range issues {
		blockers := currentBlockers(issue)
		if len(blockers) == 0 && !isBlockedStatus(issue) {
			continue
		}
		blockedFrom := issueBlockedSince(issue)
		if blockedFrom.IsZero() {
			blockedFrom = issueCreatedTime(issue)
		}
		fields, _ := issue["fields"].(map[string]any)
		ageHours := 0.0
		if !blockedFrom.IsZero() {
			ageHours = now.Sub(blockedFrom).Hours()
		}
		items = append(items, blockerAgingEntry{
			Key:         normalizeMaybeString(issue["key"]),
			Summary:     normalizeMaybeString(fields["summary"]),
			Status:      safeString(fields, "status", "name"),
			Blockers:    blockers,
			BlockedFrom: blockedFrom.Format(time.RFC3339),
			AgeHours:    ageHours,
			AgeDays:     ageHours / 24,
		})
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].AgeHours > items[j].AgeHours })
	return items
}

func buildWorkloadEntries(issues []map[string]any) []workloadEntry {
	type bucket struct {
		workloadEntry
	}
	byAssignee := map[string]*bucket{}
	for _, issue := range issues {
		assignee := normalizeMaybeString(issue["assignee"])
		if assignee == "" {
			assignee = "Unassigned"
		}
		entry := byAssignee[assignee]
		if entry == nil {
			entry = &bucket{workloadEntry: workloadEntry{Assignee: assignee}}
			byAssignee[assignee] = entry
		}
		entry.IssueCount++
		if done, _ := issue["done"].(bool); done {
			entry.DoneCount++
		} else {
			entry.OpenCount++
		}
		if points, ok := numberFromAny(issue["story_points"]); ok {
			entry.StoryPointsTotal += points
			if done, _ := issue["done"].(bool); done {
				entry.StoryPointsDone += points
			}
		}
	}
	items := make([]workloadEntry, 0, len(byAssignee))
	for _, item := range byAssignee {
		items = append(items, item.workloadEntry)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].IssueCount == items[j].IssueCount {
			return items[i].Assignee < items[j].Assignee
		}
		return items[i].IssueCount > items[j].IssueCount
	})
	return items
}

func summarizeSprintIssues(sprint map[string]any, issues []map[string]any, storyField map[string]any, includeIssues bool) reportSprintSummary {
	statusCounts := map[string]int{}
	assigneeCounts := map[string]int{}
	doneCount := 0
	pointsTotal := 0.0
	pointsDone := 0.0
	for _, issue := range issues {
		statusCounts[normalizeMaybeString(issue["status"])]++
		assignee := normalizeMaybeString(issue["assignee"])
		if assignee == "" {
			assignee = "Unassigned"
		}
		assigneeCounts[assignee]++
		if done, _ := issue["done"].(bool); done {
			doneCount++
		}
		if points, ok := numberFromAny(issue["story_points"]); ok {
			pointsTotal += points
			if done, _ := issue["done"].(bool); done {
				pointsDone += points
			}
		}
	}

	summary := reportSprintSummary{
		Sprint:           sprint,
		IssueCount:       len(issues),
		DoneCount:        doneCount,
		OpenCount:        len(issues) - doneCount,
		DonePercent:      percentage(doneCount, len(issues)),
		Statuses:         sortCountMap(statusCounts),
		Assignees:        sortCountMap(assigneeCounts),
		StoryPointsTotal: pointsTotal,
		StoryPointsDone:  pointsDone,
		StoryPointsOpen:  pointsTotal - pointsDone,
	}
	if storyField != nil {
		summary.StoryPointsField = storyField
	}
	if includeIssues {
		summary.Issues = issues
	}
	return summary
}

func buildVelocityResult(boardID string, storyField map[string]any, entries []velocityEntry) map[string]any {
	totalDone := 0
	totalPointsDone := 0.0
	for _, entry := range entries {
		totalDone += entry.DoneCount
		totalPointsDone += entry.StoryPointsDone
	}
	summary := map[string]any{
		"board":           boardID,
		"sprint_count":    len(entries),
		"avg_done_issues": averageInt(totalDone, len(entries)),
	}
	if storyField != nil {
		summary["story_points_field"] = storyField
		summary["avg_story_points_done"] = averageFloat(totalPointsDone, len(entries))
	}
	return map[string]any{
		"entries": entries,
		"summary": summary,
	}
}

func printSprintSummaryHuman(summary reportSprintSummary) {
	name := normalizeMaybeString(summary.Sprint["name"])
	if name == "" {
		name = normalizeMaybeString(summary.Sprint["id"])
	}
	fmt.Printf("Sprint %s\n", name)
	fmt.Printf("State: %s\n", normalizeMaybeString(summary.Sprint["state"]))
	if goal := normalizeMaybeString(summary.Sprint["goal"]); goal != "" {
		fmt.Printf("Goal: %s\n", goal)
	}
	fmt.Printf("Issues: %d total, %d done, %d open (%.0f%% done)\n", summary.IssueCount, summary.DoneCount, summary.OpenCount, summary.DonePercent)
	if summary.StoryPointsField != nil {
		fmt.Printf("Story points (%s): %.1f total, %.1f done, %.1f open\n", normalizeMaybeString(summary.StoryPointsField["name"]), summary.StoryPointsTotal, summary.StoryPointsDone, summary.StoryPointsOpen)
	}
	if len(summary.Statuses) > 0 {
		fmt.Println("\nStatuses:")
		rows := make([][]string, 0, len(summary.Statuses))
		for _, item := range summary.Statuses {
			rows = append(rows, []string{output.StatusBadge(normalizeMaybeString(item["name"])), fmt.Sprintf("%v", item["count"])})
		}
		fmt.Println(output.TableString([]string{"STATUS", "COUNT"}, rows))
	}
	if len(summary.Assignees) > 0 {
		fmt.Println("\nAssignees:")
		rows := make([][]string, 0, len(summary.Assignees))
		for _, item := range summary.Assignees {
			rows = append(rows, []string{normalizeMaybeString(item["name"]), fmt.Sprintf("%v", item["count"])})
		}
		fmt.Println(output.TableString([]string{"ASSIGNEE", "COUNT"}, rows))
	}
}

func printVelocityHuman(boardID string, storyField map[string]any, entries []velocityEntry) {
	fmt.Printf("Velocity report for board %s\n\n", boardID)
	if len(entries) == 0 {
		fmt.Println("No closed sprints found.")
		return
	}
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		name := normalizeMaybeString(entry.Sprint["name"])
		if storyField != nil {
			rows = append(rows, []string{
				output.Truncate(name, 28),
				fmt.Sprintf("%d/%d", entry.DoneCount, entry.IssueCount),
				fmt.Sprintf("%.1f/%.1f", entry.StoryPointsDone, entry.StoryPointsTotal),
			})
			continue
		}
		rows = append(rows, []string{
			output.Truncate(name, 28),
			fmt.Sprintf("%d/%d", entry.DoneCount, entry.IssueCount),
		})
	}
	if storyField != nil {
		fmt.Println(output.TableString([]string{"SPRINT", "DONE", "POINTS"}, rows))
		return
	}
	fmt.Println(output.TableString([]string{"SPRINT", "DONE"}, rows))
}

func printBurndownHuman(sprint map[string]any, storyField map[string]any, useIssueCount bool, points []burndownPoint) {
	fmt.Printf("Burndown for %s\n\n", normalizeMaybeString(sprint["name"]))
	unit := "points"
	if useIssueCount || storyField == nil {
		unit = "issues"
	}
	rows := make([][]string, 0, len(points))
	for _, point := range points {
		rows = append(rows, []string{
			point.Date,
			fmt.Sprintf("%.1f", point.Remaining),
			fmt.Sprintf("%.1f", point.Ideal),
			fmt.Sprintf("%.1f", point.Done),
		})
	}
	fmt.Println(output.TableString([]string{"DATE", "REMAINING", "IDEAL", "DONE"}, rows))
	fmt.Printf("\nUnits: %s\n", unit)
}

func printCycleTimeHuman(sprint map[string]any, summary map[string]any, entries []cycleTimeEntry) {
	fmt.Printf("Cycle time for %s\n", normalizeMaybeString(sprint["name"]))
	fmt.Printf("Completed issues: %v | Avg days: %.1f\n\n", summary["completed_issues"], summary["avg_days"])
	rows := make([][]string, 0, len(entries))
	for _, entry := range entries {
		rows = append(rows, []string{
			entry.Key,
			fmt.Sprintf("%.1f", entry.Days),
			output.Truncate(entry.Summary, 64),
		})
	}
	fmt.Println(output.TableString([]string{"KEY", "DAYS", "SUMMARY"}, rows))
}

func printBlockerAgingHuman(sprint map[string]any, items []blockerAgingEntry) {
	fmt.Printf("Blocker aging for %s\n\n", normalizeMaybeString(sprint["name"]))
	if len(items) == 0 {
		fmt.Println("No blocked issues.")
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			item.Key,
			fmt.Sprintf("%.1f", item.AgeDays),
			output.StatusBadge(item.Status),
			output.Truncate(strings.Join(item.Blockers, ", "), 32),
			output.Truncate(item.Summary, 48),
		})
	}
	fmt.Println(output.TableString([]string{"KEY", "AGE DAYS", "STATUS", "BLOCKERS", "SUMMARY"}, rows))
}

func printWorkloadHuman(sprint map[string]any, storyField map[string]any, items []workloadEntry) {
	fmt.Printf("Workload for %s\n\n", normalizeMaybeString(sprint["name"]))
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		if storyField != nil {
			rows = append(rows, []string{
				item.Assignee,
				fmt.Sprintf("%d", item.IssueCount),
				fmt.Sprintf("%d", item.DoneCount),
				fmt.Sprintf("%d", item.OpenCount),
				fmt.Sprintf("%.1f/%.1f", item.StoryPointsDone, item.StoryPointsTotal),
			})
			continue
		}
		rows = append(rows, []string{
			item.Assignee,
			fmt.Sprintf("%d", item.IssueCount),
			fmt.Sprintf("%d", item.DoneCount),
			fmt.Sprintf("%d", item.OpenCount),
		})
	}
	if storyField != nil {
		fmt.Println(output.TableString([]string{"ASSIGNEE", "ISSUES", "DONE", "OPEN", "POINTS"}, rows))
		return
	}
	fmt.Println(output.TableString([]string{"ASSIGNEE", "ISSUES", "DONE", "OPEN"}, rows))
}

func sortCountMap(items map[string]int) []map[string]any {
	result := make([]map[string]any, 0, len(items))
	for name, count := range items {
		result = append(result, map[string]any{"name": name, "count": count})
	}
	sort.SliceStable(result, func(i, j int) bool {
		if intFromAny(result[i]["count"], 0) == intFromAny(result[j]["count"], 0) {
			return normalizeMaybeString(result[i]["name"]) < normalizeMaybeString(result[j]["name"])
		}
		return intFromAny(result[i]["count"], 0) > intFromAny(result[j]["count"], 0)
	})
	return result
}

func sprintBounds(sprint map[string]any) (time.Time, time.Time) {
	start := parseJiraTime(normalizeMaybeString(sprint["startDate"]))
	end := parseJiraTime(normalizeMaybeString(sprint["completeDate"]))
	if end.IsZero() {
		end = parseJiraTime(normalizeMaybeString(sprint["endDate"]))
	}
	if end.IsZero() && strings.EqualFold(normalizeMaybeString(sprint["state"]), "active") {
		end = time.Now().UTC()
	}
	return start, end
}

func issueCreatedTime(issue map[string]any) time.Time {
	fields, _ := issue["fields"].(map[string]any)
	return parseJiraTime(normalizeMaybeString(fields["created"]))
}

func issueUpdatedTime(issue map[string]any) time.Time {
	fields, _ := issue["fields"].(map[string]any)
	return parseJiraTime(normalizeMaybeString(fields["updated"]))
}

func issueDoneTime(issue map[string]any) time.Time {
	changelog, _ := issue["changelog"].(map[string]any)
	rawHistories, _ := changelog["histories"].([]any)
	for _, entry := range chronologicalHistoryEntries(summarizeHistories(rawHistories, "status")) {
		changes, _ := entry["changes"].([]map[string]any)
		for _, change := range changes {
			if isDoneLikeStatus(normalizeMaybeString(change["to"])) {
				return parseJiraTime(normalizeMaybeString(entry["created"]))
			}
		}
	}
	fields, _ := issue["fields"].(map[string]any)
	if isDoneStatus(fields) {
		if resolved := parseJiraTime(normalizeMaybeString(fields["resolutiondate"])); !resolved.IsZero() {
			return resolved
		}
		return issueUpdatedTime(issue)
	}
	return time.Time{}
}

func issueStartTime(issue map[string]any) time.Time {
	changelog, _ := issue["changelog"].(map[string]any)
	rawHistories, _ := changelog["histories"].([]any)
	for _, entry := range chronologicalHistoryEntries(summarizeHistories(rawHistories, "status")) {
		changes, _ := entry["changes"].([]map[string]any)
		for _, change := range changes {
			if isStartedLikeStatus(normalizeMaybeString(change["to"])) {
				return parseJiraTime(normalizeMaybeString(entry["created"]))
			}
		}
	}
	return issueCreatedTime(issue)
}

func issueBlockedSince(issue map[string]any) time.Time {
	changelog, _ := issue["changelog"].(map[string]any)
	rawHistories, _ := changelog["histories"].([]any)
	for _, entry := range chronologicalHistoryEntries(summarizeHistories(rawHistories, "status")) {
		changes, _ := entry["changes"].([]map[string]any)
		for _, change := range changes {
			if isBlockedLikeStatus(normalizeMaybeString(change["to"])) {
				return parseJiraTime(normalizeMaybeString(entry["created"]))
			}
		}
	}
	if updated := issueUpdatedTime(issue); !updated.IsZero() {
		return updated
	}
	return issueCreatedTime(issue)
}

func parseJiraTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	for _, layout := range []string{
		time.RFC3339,
		"2006-01-02T15:04:05.000-0700",
		"2006-01-02T15:04:05.999-0700",
		"2006-01-02",
	} {
		if parsed, err := time.Parse(layout, raw); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func isDoneLikeStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(status, "done") || strings.Contains(status, "closed") || strings.Contains(status, "resolved") || strings.Contains(status, "complete")
}

func isStartedLikeStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(status, "progress") || strings.Contains(status, "review") || strings.Contains(status, "develop") || strings.Contains(status, "doing")
}

func isBlockedLikeStatus(status string) bool {
	status = strings.ToLower(strings.TrimSpace(status))
	return strings.Contains(status, "blocked") || strings.Contains(status, "imped") || strings.Contains(status, "waiting")
}

func currentBlockers(issue map[string]any) []string {
	fields, _ := issue["fields"].(map[string]any)
	rawLinks, _ := fields["issuelinks"].([]any)
	blockers := make([]string, 0)
	for _, raw := range rawLinks {
		link, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		linkType, _ := link["type"].(map[string]any)
		inward := strings.ToLower(normalizeMaybeString(linkType["inward"]))
		outward := strings.ToLower(normalizeMaybeString(linkType["outward"]))
		if issueMap, ok := link["inwardIssue"].(map[string]any); ok && (strings.Contains(inward, "blocked by") || strings.Contains(inward, "depends on")) {
			if key := normalizeMaybeString(issueMap["key"]); key != "" {
				blockers = append(blockers, key)
			}
			continue
		}
		if issueMap, ok := link["outwardIssue"].(map[string]any); ok && (strings.Contains(outward, "blocked by") || strings.Contains(outward, "depends on")) {
			if key := normalizeMaybeString(issueMap["key"]); key != "" {
				blockers = append(blockers, key)
			}
		}
	}
	sort.Strings(blockers)
	return blockers
}

func isBlockedStatus(issue map[string]any) bool {
	fields, _ := issue["fields"].(map[string]any)
	return isBlockedLikeStatus(safeString(fields, "status", "name"))
}

func numberFromAny(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case json.Number:
		f, err := n.Float64()
		return f, err == nil
	default:
		return 0, false
	}
}

func isDoneStatus(fields map[string]any) bool {
	if strings.EqualFold(safeString(fields, "status", "statusCategory", "key"), "done") {
		return true
	}
	category := strings.ToLower(safeString(fields, "status", "statusCategory", "name"))
	return strings.Contains(category, "done")
}

func percentage(done, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(done) * 100 / float64(total)
}

func averageFloat(total float64, count int) float64 {
	if count == 0 {
		return 0
	}
	return total / float64(count)
}

func averageInt(total, count int) float64 {
	if count == 0 {
		return 0
	}
	return float64(total) / float64(count)
}

func preferNonBacklogSprints(sprints []map[string]any) []map[string]any {
	filtered := make([]map[string]any, 0, len(sprints))
	for _, sprint := range sprints {
		if !isBacklogSprint(sprint) {
			filtered = append(filtered, sprint)
		}
	}
	if len(filtered) == 0 {
		return sprints
	}
	return filtered
}

func isBacklogSprint(sprint map[string]any) bool {
	name := strings.ToLower(normalizeMaybeString(sprint["name"]))
	return strings.Contains(name, "backlog")
}

func preferBoardOriginSprints(boardID string, sprints []map[string]any) []map[string]any {
	filtered := make([]map[string]any, 0, len(sprints))
	for _, sprint := range sprints {
		origin := normalizeMaybeString(sprint["originBoardId"])
		if origin == "" || origin == boardID {
			filtered = append(filtered, sprint)
		}
	}
	if len(filtered) == 0 {
		return sprints
	}
	return filtered
}
