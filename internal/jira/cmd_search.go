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
	cmd.Flags().Int("page-size", 100, "Page size when fetching --all results (default: 100)")
	cmd.Flags().Bool("all", false, "Fetch all pages of results")
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
	pageSize, _ := cmd.Flags().GetInt("page-size")
	fetchAll, _ := cmd.Flags().GetBool("all")
	fields, _ := cmd.Flags().GetString("fields")
	expand, _ := cmd.Flags().GetString("expand")
	outputFile, _ := cmd.Flags().GetString("output")

	if pageSize <= 0 {
		pageSize = 100
	}

	data, err := client.Search(jql, limit, start, fields, expand)
	if err != nil {
		return err
	}

	issues, _ := data["issues"].([]any)
	total := intFromAny(data["total"], len(issues))
	if fetchAll {
		collected := make([]any, 0, len(issues))
		collected = append(collected, issues...)
		nextStart := start + len(issues)
		for nextStart < total {
			pageLimit := pageSize
			if limit > 0 {
				remaining := limit - len(collected)
				if remaining <= 0 {
					break
				}
				if remaining < pageLimit {
					pageLimit = remaining
				}
			}
			page, err := client.Search(jql, pageLimit, nextStart, fields, expand)
			if err != nil {
				return err
			}
			pageIssues, _ := page["issues"].([]any)
			if len(pageIssues) == 0 {
				break
			}
			collected = append(collected, pageIssues...)
			nextStart += len(pageIssues)
		}
		data["issues"] = collected
		issues = collected
	}

	if outputFile != "" {
		jsonBytes, _ := json.MarshalIndent(data, "", "  ")
		if err := writeFile(outputFile, string(jsonBytes)); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "search",
				map[string]any{"jql": jql},
				map[string]any{"saved_to": outputFile, "total": total, "fetched": len(issues), "all": fetchAll},
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
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "search",
			map[string]any{"jql": jql, "start": start, "limit": limit, "all": fetchAll},
			data, nil, nil, "", "", "", nil,
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
		if fetchAll {
			fmt.Printf("Found %d of %d issue(s) for JQL: %s\n", len(issues), total, jql)
		} else {
			fmt.Printf("Found %d issue(s) for JQL: %s\n", len(issues), jql)
		}
		return nil
	}

	if fetchAll {
		fmt.Printf("Found %d of %d issue(s):\n\n", len(issues), total)
	} else {
		fmt.Printf("Found %d issue(s):\n\n", len(issues))
	}
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
