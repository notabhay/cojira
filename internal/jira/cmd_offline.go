package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewOfflineCmd creates the "offline" command group.
func NewOfflineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "offline",
		Short: "Browse locally synced Jira issue snapshots without network access",
	}
	cmd.AddCommand(
		newOfflineSearchCmd(),
		newOfflineInfoCmd(),
		newOfflineRecentCmd(),
	)
	return cmd
}

func newOfflineSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <text>",
		Short: "Search local issue.json snapshots",
		Args:  cobra.ExactArgs(1),
		RunE:  runOfflineSearch,
	}
	cmd.Flags().String("base-dir", "", "Base sync directory (defaults to jira.offline.base_dir or 0-JIRA)")
	cmd.Flags().Int("limit", 20, "Max results (default: 20)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runOfflineSearch(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	baseDir, _ := cmd.Flags().GetString("base-dir")
	limit, _ := cmd.Flags().GetInt("limit")
	query := strings.ToLower(strings.TrimSpace(args[0]))
	rows, err := searchOfflineIssues(baseDir, func(issue map[string]any) bool {
		fields, _ := issue["fields"].(map[string]any)
		text := strings.ToLower(strings.Join([]string{
			normalizeMaybeString(issue["key"]),
			normalizeMaybeString(fields["summary"]),
			safeString(fields, "status", "name"),
			safeString(fields, "assignee", "displayName"),
			strings.Join(safeStringSlice(fields, "labels"), " "),
		}, " "))
		return strings.Contains(text, query)
	}, limit)
	if err != nil {
		return err
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "offline.search", map[string]any{"query": args[0]}, map[string]any{"issues": rows}, nil, nil, "", "", "", nil))
	}
	if len(rows) == 0 {
		if mode == "summary" {
			fmt.Printf("Found 0 offline issues for %s.\n", args[0])
			return nil
		}
		fmt.Println("No offline issues found.")
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Found %d offline issue(s) for %s.\n", len(rows), args[0])
		return nil
	}
	fmt.Printf("Offline issues for %s:\n\n", args[0])
	printIssueSearchRows(rows)
	return nil
}

func newOfflineInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <issue>",
		Short: "Show metadata from a local issue.json snapshot",
		Args:  cobra.ExactArgs(1),
		RunE:  runOfflineInfo,
	}
	cmd.Flags().String("base-dir", "", "Base sync directory (defaults to jira.offline.base_dir or 0-JIRA)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runOfflineInfo(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	baseDir, _ := cmd.Flags().GetString("base-dir")
	key := ResolveIssueIdentifier(args[0])
	rows, err := searchOfflineIssues(baseDir, func(issue map[string]any) bool {
		return strings.EqualFold(normalizeMaybeString(issue["key"]), key)
	}, 1)
	if err != nil {
		return err
	}
	if len(rows) == 0 {
		return &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Offline issue %s was not found.", key), ExitCode: 1}
	}
	info := summarizeIssueInfo(nil, rows[0])
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "offline.info", map[string]any{"issue": key}, info, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		summaryInfo := info["summary_info"].(map[string]any)
		fmt.Printf("%s: %s (Status: %s, Assignee: %s)\n", summaryInfo["key"], summaryInfo["summary"], summaryInfo["status"], summaryInfo["assignee"])
		return nil
	}
	fmt.Println(output.TableString([]string{"KEY", "STATUS", "ASSIGNEE", "PRIORITY", "SUMMARY"}, [][]string{{
		normalizeMaybeString(info["key"]),
		output.StatusBadge(normalizeMaybeString(info["status"])),
		stringOr(info["assignee"], "Unassigned"),
		stringOr(info["priority"], "-"),
		output.Truncate(normalizeMaybeString(info["summary"]), 72),
	}}))
	return nil
}

func newOfflineRecentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "List locally tracked recent issues with offline references",
		Args:  cobra.NoArgs,
		RunE:  runOfflineRecent,
	}
	cmd.Flags().Int("limit", 20, "Max results (default: 20)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runOfflineRecent(cmd *cobra.Command, _ []string) error {
	return runRecent(cmd, nil)
}

func searchOfflineIssues(baseDir string, predicate func(map[string]any) bool, limit int) ([]map[string]any, error) {
	files, err := offlineIssueFiles(baseDir)
	if err != nil {
		return nil, err
	}
	rows := make([]map[string]any, 0)
	for _, path := range files {
		issue, err := loadOfflineIssue(path)
		if err != nil {
			return nil, err
		}
		if !predicate(issue) {
			continue
		}
		rows = append(rows, issue)
		if limit > 0 && len(rows) >= limit {
			break
		}
	}
	return rows, nil
}
