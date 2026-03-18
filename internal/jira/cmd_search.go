package jira

import (
	"encoding/json"
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewSearchCmd creates the "search" subcommand.
func NewSearchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "search <jql>",
		Short: "Search issues using JQL",
		Long:  "Search issues using JQL and display a compact list.",
		Args:  cobra.ExactArgs(1),
		RunE:  runSearch,
	}
	cmd.Flags().Int("limit", 20, "Max results (default: 20)")
	cmd.Flags().Int("start", 0, "Start offset (default: 0)")
	cmd.Flags().String("fields", "", "Fields to request (comma-separated)")
	cmd.Flags().String("expand", "", "Expand options (comma-separated)")
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runSearch(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	jql := applyDefaultScope(cmd, args[0])
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	fields, _ := cmd.Flags().GetString("fields")
	expand, _ := cmd.Flags().GetString("expand")
	outputFile, _ := cmd.Flags().GetString("output")

	data, err := client.Search(jql, limit, start, fields, expand)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(errorCode(err, "SEARCH_FAILED"), err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "search",
				map[string]any{"jql": jql, "start": start, "limit": limit},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		return err
	}

	issues, _ := data["issues"].([]any)

	if outputFile != "" {
		jsonBytes, _ := json.MarshalIndent(data, "", "  ")
		if err := writeFile(outputFile, string(jsonBytes)); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "search",
				map[string]any{"jql": jql},
				map[string]any{"schema": "jira.search.saved/v1", "saved_to": outputFile, "total": intFromAny(data["total"], len(issues))},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved %d issue(s) to %s.\n", len(issues), outputFile)
			return nil
		}
		fmt.Printf("Saved search results (%d issues) to: %s\n", len(issues), outputFile)
		return nil
	}

	if mode == "json" {
		result := deepCopyMap(data)
		result["schema"] = "jira.search/v1"
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "search",
			map[string]any{"jql": jql, "start": start, "limit": limit},
			result, nil, nil, "", "", "", nil,
		))
	}

	if len(issues) == 0 {
		if mode == "summary" {
			fmt.Printf("Found 0 issues for JQL: %s\n", jql)
			return nil
		}
		fmt.Println("No issues found.")
		return nil
	}

	if mode == "summary" {
		fmt.Printf("Found %d issue(s) for JQL: %s\n", len(issues), jql)
		return nil
	}

	fmt.Printf("Found %d issue(s):\n\n", len(issues))
	for _, i := range issues {
		issue, ok := i.(map[string]any)
		if !ok {
			continue
		}
		fd, _ := issue["fields"].(map[string]any)
		if fd == nil {
			fd = map[string]any{}
		}
		key, _ := issue["key"].(string)
		summary, _ := fd["summary"].(string)
		status := safeString(fd, "status", "name")
		assignee := safeString(fd, "assignee", "displayName")
		fmt.Printf("  %-12s [%s] %s (assignee: %s)\n", key, status, summary, assignee)
	}
	return nil
}

// applyDefaultScope applies the default JQL scope from project config if present.
func applyDefaultScope(cmd *cobra.Command, jql string) string {
	cfg, err := config.LoadProjectConfig(nil)
	if err != nil || cfg == nil {
		return FixJQLShellEscapes(jql)
	}
	scope, _ := cfg.GetValue([]string{"jira", "default_jql_scope"}, "").(string)
	return ApplyDefaultJQLScope(jql, scope)
}
