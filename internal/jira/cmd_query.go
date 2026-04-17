package jira

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/config"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewQueryCmd creates the "query" command group for saved JQL snippets.
func NewQueryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "query",
		Short: "Manage saved Jira queries stored in .cojira.json",
	}
	cmd.AddCommand(
		newQueryListCmd(),
		newQueryRunCmd(),
		newQuerySaveCmd(),
		newQueryDeleteCmd(),
	)
	return cmd
}

func newQueryListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List saved queries from .cojira.json",
		Args:  cobra.NoArgs,
		RunE:  runQueryList,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runQueryList(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.ProjectConfig{Data: map[string]any{}}
	}
	queries := savedQueriesFromConfig(cfg)
	names := sortedStringKeys(queries)
	items := make([]map[string]any, 0, len(names))
	for _, name := range names {
		items = append(items, map[string]any{"name": name, "jql": queries[name]})
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "query.list", nil, map[string]any{"queries": items}, nil, nil, "", "", "", nil))
	}
	if len(items) == 0 {
		if mode == "summary" {
			fmt.Println("Found 0 saved queries.")
			return nil
		}
		fmt.Println("No saved queries.")
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Found %d saved queries.\n", len(items))
		return nil
	}
	fmt.Println("Saved queries:")
	fmt.Println()
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			normalizeMaybeString(item["name"]),
			output.Truncate(normalizeMaybeString(item["jql"]), 96),
		})
	}
	fmt.Println(output.TableString([]string{"NAME", "JQL"}, rows))
	return nil
}

func newQueryRunCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "run <name-or-jql>",
		Short: "Run a saved query by name or a raw JQL string",
		Args:  cobra.ExactArgs(1),
		RunE:  runQueryRun,
	}
	cmd.Flags().Int("limit", 20, "Max results (default: 20)")
	cmd.Flags().Bool("all", false, "Fetch all pages of results")
	cmd.Flags().Int("page-size", 100, "Page size when fetching --all results (default: 100)")
	cmd.Flags().String("fields", "", "Fields to request (comma-separated)")
	cmd.Flags().String("expand", "", "Expand options (comma-separated)")
	cmd.Flags().Bool("summary-only", false, "Print only query summary metadata in JSON mode")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runQueryRun(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil {
		return err
	}
	if cfg == nil {
		cfg = &config.ProjectConfig{Data: map[string]any{}}
	}
	nameOrJQL := strings.TrimSpace(args[0])
	queries := savedQueriesFromConfig(cfg)
	jql := nameOrJQL
	queryName := ""
	if stored, ok := queries[nameOrJQL]; ok {
		jql = stored
		queryName = nameOrJQL
	}
	jql = applyDefaultScope(cmd, jql)

	limit, _ := cmd.Flags().GetInt("limit")
	fetchAll, _ := cmd.Flags().GetBool("all")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	fields, _ := cmd.Flags().GetString("fields")
	expand, _ := cmd.Flags().GetString("expand")
	summaryOnly, _ := cmd.Flags().GetBool("summary-only")

	results, total, err := searchAllIssues(client, jql, limit, 0, pageSize, fields, expand, fetchAll)
	if err != nil {
		return err
	}
	recordSearchRecents(client, results, "saved-query")

	target := map[string]any{"jql": jql}
	if queryName != "" {
		target["name"] = queryName
	}
	if mode == "json" {
		result := map[string]any{
			"query": map[string]any{
				"name":        queryName,
				"jql":         jql,
				"returned":    len(results),
				"total":       total,
				"fetched_all": fetchAll,
			},
			"issues": results,
		}
		if summaryOnly {
			result = map[string]any{"query": result["query"]}
		}
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "query.run", target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		label := jql
		if queryName != "" {
			label = queryName
		}
		fmt.Printf("Query %s returned %d issue(s).\n", label, len(results))
		return nil
	}
	label := jql
	if queryName != "" {
		label = queryName
	}
	fmt.Printf("Query %s returned %d of %d issue(s):\n\n", label, len(results), total)
	printIssueSearchRows(results)
	return nil
}

func newQuerySaveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "save <name> <jql>",
		Short: "Save a named query into the nearest .cojira.json",
		Args:  cobra.ExactArgs(2),
		RunE:  runQuerySave,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runQuerySave(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	name := strings.TrimSpace(args[0])
	jql := strings.TrimSpace(args[1])
	if name == "" || jql == "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Query name and JQL are required.", ExitCode: 2}
	}
	cfg, err := config.LoadWritableProjectConfig()
	if err != nil {
		return err
	}
	jiraSection := cfg.GetSection("jira")
	if len(jiraSection) == 0 {
		jiraSection = map[string]any{}
		cfg.Data["jira"] = jiraSection
	}
	queries, _ := jiraSection["saved_queries"].(map[string]any)
	if queries == nil {
		queries = map[string]any{}
		jiraSection["saved_queries"] = queries
	}
	queries[name] = jql
	if err := config.WriteProjectConfig(cfg.Path, cfg.Data); err != nil {
		return err
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "query.save", map[string]any{"name": name}, map[string]any{"path": cfg.Path, "jql": jql}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Saved query %s.\n", name)
		return nil
	}
	fmt.Printf("Saved query %s to %s.\n", name, cfg.Path)
	return nil
}

func newQueryDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <name>",
		Short: "Delete a named query from the nearest .cojira.json",
		Args:  cobra.ExactArgs(1),
		RunE:  runQueryDelete,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runQueryDelete(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	name := strings.TrimSpace(args[0])
	cfg, err := config.LoadWritableProjectConfig()
	if err != nil {
		return err
	}
	jiraSection := cfg.GetSection("jira")
	queries, _ := jiraSection["saved_queries"].(map[string]any)
	if queries == nil {
		return &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Saved query %s was not found.", name), ExitCode: 1}
	}
	if _, ok := queries[name]; !ok {
		return &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Saved query %s was not found.", name), ExitCode: 1}
	}
	delete(queries, name)
	if len(queries) == 0 {
		delete(jiraSection, "saved_queries")
	}
	if err := config.WriteProjectConfig(cfg.Path, cfg.Data); err != nil {
		return err
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "query.delete", map[string]any{"name": name}, map[string]any{"path": cfg.Path}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Deleted query %s.\n", name)
		return nil
	}
	fmt.Printf("Deleted query %s from %s.\n", name, cfg.Path)
	return nil
}

// NewMineCmd creates the "mine" shortcut command.
func NewMineCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "mine",
		Short: "Show issues assigned to the current user",
		Args:  cobra.NoArgs,
		RunE:  runMine,
	}
	cmd.Flags().Int("limit", 20, "Max results (default: 20)")
	cmd.Flags().Bool("all", false, "Fetch all pages of results")
	cmd.Flags().Bool("include-done", false, "Include resolved or done issues")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runMine(cmd *cobra.Command, _ []string) error {
	includeDone, _ := cmd.Flags().GetBool("include-done")
	jql := "assignee = currentUser() ORDER BY updated DESC"
	if !includeDone {
		jql = "assignee = currentUser() AND resolution = Unresolved ORDER BY updated DESC"
	}
	cmd.SetArgs(nil)
	return runShortcutQuery(cmd, jql)
}

// NewRecentCmd creates the "recent" shortcut command.
func NewRecentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "recent",
		Short: "List recently viewed or returned issues from local state",
		Args:  cobra.NoArgs,
		RunE:  runRecent,
	}
	cmd.Flags().Int("limit", 20, "Max results (default: 20)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runRecent(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	limit, _ := cmd.Flags().GetInt("limit")
	items, err := listRecentIssues(limit)
	if err != nil {
		return err
	}
	rows := make([]map[string]any, 0, len(items))
	for _, item := range items {
		rows = append(rows, map[string]any{
			"key":      item.Key,
			"summary":  item.Summary,
			"status":   item.Status,
			"assignee": item.Assignee,
			"source":   item.Source,
			"url":      item.URL,
			"seen_at":  item.SeenAt.Format(time.RFC3339),
		})
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "recent", nil, map[string]any{"issues": rows}, nil, nil, "", "", "", nil))
	}
	if len(rows) == 0 {
		if mode == "summary" {
			fmt.Println("Found 0 recent issues.")
			return nil
		}
		fmt.Println("No recent issues.")
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Found %d recent issue(s).\n", len(rows))
		return nil
	}
	fmt.Println("Recent issues:")
	fmt.Println()
	tableRows := make([][]string, 0, len(rows))
	for _, row := range rows {
		tableRows = append(tableRows, []string{
			normalizeMaybeString(row["key"]),
			output.StatusBadge(normalizeMaybeString(row["status"])),
			stringOr(row["assignee"], "Unassigned"),
			output.Truncate(normalizeMaybeString(row["summary"]), 52),
			normalizeMaybeString(row["source"]),
		})
	}
	fmt.Println(output.TableString([]string{"KEY", "STATUS", "ASSIGNEE", "SUMMARY", "SOURCE"}, tableRows))
	return nil
}

func runShortcutQuery(cmd *cobra.Command, jql string) error {
	mode, _ := cmd.Flags().GetString("output-mode")
	limit, _ := cmd.Flags().GetInt("limit")
	all, _ := cmd.Flags().GetBool("all")
	queryCmd := &cobra.Command{}
	queryCmd.Flags().String("output-mode", mode, "")
	queryCmd.Flags().Int("limit", limit, "")
	queryCmd.Flags().Int("page-size", 100, "")
	queryCmd.Flags().Bool("all", all, "")
	queryCmd.Flags().String("fields", "", "")
	queryCmd.Flags().String("expand", "", "")
	queryCmd.Flags().Bool("summary-only", false, "")
	queryCmd.SetContext(cmd.Context())
	return runQueryRun(queryCmd, []string{jql})
}

func sortedStringKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
