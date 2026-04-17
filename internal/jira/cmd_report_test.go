package jira

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterDashboards(t *testing.T) {
	items := []map[string]any{
		{"id": "1", "name": "Platform", "owner": map[string]any{"displayName": "Abhay"}, "isFavourite": true},
		{"id": "2", "name": "Marketing", "owner": map[string]any{"displayName": "Jane"}},
	}

	filtered := filterDashboards(items, "plat", "abh", true)
	require.Len(t, filtered, 1)
	assert.Equal(t, "1", filtered[0]["id"])
}

func TestSelectSprintForReportPrefersActiveThenFuture(t *testing.T) {
	sprints := []map[string]any{
		{"id": "3", "state": "closed", "completeDate": "2026-04-10T00:00:00Z"},
		{"id": "2", "state": "future", "startDate": "2026-04-20T00:00:00Z"},
		{"id": "1", "state": "active", "startDate": "2026-04-12T00:00:00Z"},
	}

	selected := selectSprintForReport(sprints, "auto", true)
	require.NotNil(t, selected)
	assert.Equal(t, "1", selected["id"])
}

func TestSelectSprintForReportFallsBackToClosed(t *testing.T) {
	sprints := []map[string]any{
		{"id": "3", "state": "closed", "name": "Backlog Completed", "completeDate": "2026-04-30T00:00:00Z"},
		{"id": "2", "state": "closed", "completeDate": "2026-04-20T00:00:00Z"},
	}

	selected := selectSprintForReport(sprints, "auto", true)
	require.NotNil(t, selected)
	assert.Equal(t, "2", selected["id"])
}

func TestPreferNonBacklogSprintsFallsBackWhenNeeded(t *testing.T) {
	sprints := []map[string]any{
		{"id": "1", "name": "Backlog Completed"},
	}

	filtered := preferNonBacklogSprints(sprints)
	require.Len(t, filtered, 1)
	assert.Equal(t, "1", filtered[0]["id"])
}

func TestPreferBoardOriginSprints(t *testing.T) {
	sprints := []map[string]any{
		{"id": "1", "originBoardId": 77},
		{"id": "2", "originBoardId": 99},
		{"id": "3"},
	}

	filtered := preferBoardOriginSprints("77", sprints)
	require.Len(t, filtered, 2)
	assert.Equal(t, "1", filtered[0]["id"])
	assert.Equal(t, "3", filtered[1]["id"])
}

func TestSummarizeSprintIssuesAggregatesCountsAndPoints(t *testing.T) {
	sprint := map[string]any{"id": "12", "name": "Sprint 12"}
	issues := []map[string]any{
		{"status": "Done", "assignee": "Abhay", "done": true, "story_points": 5.0},
		{"status": "In Progress", "assignee": "Abhay", "done": false, "story_points": 3.0},
		{"status": "To Do", "assignee": "", "done": false},
	}

	summary := summarizeSprintIssues(sprint, issues, map[string]any{"id": "customfield_1", "name": "Story Points"}, false)
	assert.Equal(t, 3, summary.IssueCount)
	assert.Equal(t, 1, summary.DoneCount)
	assert.InDelta(t, 33.33, summary.DonePercent, 0.1)
	assert.InDelta(t, 8.0, summary.StoryPointsTotal, 0.01)
	assert.InDelta(t, 5.0, summary.StoryPointsDone, 0.01)
	require.Len(t, summary.Assignees, 2)
	assert.Equal(t, "Abhay", summary.Assignees[0]["name"])
}

func TestNumberFromAnySupportsJSONNumber(t *testing.T) {
	n := json.Number("13.5")
	value, ok := numberFromAny(n)
	require.True(t, ok)
	assert.InDelta(t, 13.5, value, 0.001)
}

func TestBuildBurndownPointsUsesDoneTransitions(t *testing.T) {
	sprint := map[string]any{
		"id":        "12",
		"name":      "Sprint 12",
		"startDate": "2026-04-01T00:00:00.000+0000",
		"endDate":   "2026-04-03T00:00:00.000+0000",
	}
	issues := []map[string]any{
		reportIssueWithHistory("PROJ-1", "2026-04-01T00:00:00.000+0000", 5.0, []map[string]any{
			statusHistory("1", "2026-04-02T10:00:00.000+0000", "In Progress", "Done"),
		}),
		reportIssueWithHistory("PROJ-2", "2026-04-01T00:00:00.000+0000", 3.0, nil),
	}

	points := buildBurndownPoints(sprint, issues, map[string]any{"id": "customfield_1", "name": "Story Points"}, false)
	require.Len(t, points, 3)
	assert.InDelta(t, 8.0, points[0].Remaining, 0.01)
	assert.InDelta(t, 3.0, points[1].Remaining, 0.01)
	assert.InDelta(t, 3.0, points[2].Remaining, 0.01)
}

func TestBuildCycleTimeReportUsesStartAndDoneTransitions(t *testing.T) {
	issues := []map[string]any{
		reportIssueWithHistory("PROJ-1", "2026-04-01T00:00:00.000+0000", 0, []map[string]any{
			statusHistory("1", "2026-04-02T12:00:00.000+0000", "To Do", "In Progress"),
			statusHistory("2", "2026-04-04T12:00:00.000+0000", "In Progress", "Done"),
		}),
	}

	entries, summary := buildCycleTimeReport(issues)
	require.Len(t, entries, 1)
	assert.InDelta(t, 48.0, entries[0].Hours, 0.1)
	assert.Equal(t, 1, summary["completed_issues"])
	assert.InDelta(t, 2.0, summary["avg_days"], 0.1)
}

func TestBuildBlockerAgingReportDetectsCurrentBlockers(t *testing.T) {
	now := time.Now().UTC()
	issue := reportIssueWithHistory("PROJ-1", now.Add(-72*time.Hour).Format("2006-01-02T15:04:05.000-0700"), 0, []map[string]any{
		statusHistory("1", now.Add(-48*time.Hour).Format("2006-01-02T15:04:05.000-0700"), "In Progress", "Blocked"),
	})
	fields := issue["fields"].(map[string]any)
	fields["issuelinks"] = []any{
		map[string]any{
			"type":        map[string]any{"inward": "is blocked by"},
			"inwardIssue": map[string]any{"key": "PROJ-9"},
		},
	}

	items := buildBlockerAgingReport([]map[string]any{issue})
	require.Len(t, items, 1)
	assert.Equal(t, "PROJ-9", items[0].Blockers[0])
	assert.Greater(t, items[0].AgeHours, 0.0)
}

func TestBuildWorkloadEntriesAggregatesAssignees(t *testing.T) {
	issues := []map[string]any{
		{"assignee": "Abhay", "done": true, "story_points": 5.0},
		{"assignee": "Abhay", "done": false, "story_points": 3.0},
		{"assignee": "", "done": false},
	}

	items := buildWorkloadEntries(issues)
	require.Len(t, items, 2)
	assert.Equal(t, "Abhay", items[0].Assignee)
	assert.Equal(t, 2, items[0].IssueCount)
	assert.InDelta(t, 8.0, items[0].StoryPointsTotal, 0.01)
}

func reportIssueWithHistory(key, created string, points float64, histories []map[string]any) map[string]any {
	fields := map[string]any{
		"summary":        key + " summary",
		"created":        created,
		"updated":        created,
		"status":         map[string]any{"name": "To Do", "statusCategory": map[string]any{"name": "To Do", "key": "new"}},
		"customfield_1":  points,
		"resolutiondate": "",
	}
	rawHistories := make([]any, 0, len(histories))
	for _, history := range histories {
		rawHistories = append(rawHistories, history)
	}
	if len(histories) > 0 {
		last := histories[len(histories)-1]
		items := last["items"].([]any)
		status := normalizeMaybeString(items[0].(map[string]any)["toString"])
		fields["status"] = map[string]any{"name": status, "statusCategory": map[string]any{"name": status, "key": strings.ToLower(status)}}
		fields["updated"] = last["created"]
	}
	return map[string]any{
		"key":      key,
		"fields":   fields,
		"changelog": map[string]any{"histories": rawHistories},
	}
}

func statusHistory(id, created, from, to string) map[string]any {
	return map[string]any{
		"id":      id,
		"created": created,
		"items": []any{
			map[string]any{
				"field":      "status",
				"fromString": from,
				"toString":   to,
			},
		},
	}
}
