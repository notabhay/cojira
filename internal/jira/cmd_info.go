package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewInfoCmd creates the "info" subcommand.
func NewInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <issue...>",
		Short: "Show issue metadata",
		Args:  cobra.ArbitraryArgs,
		RunE:  runInfo,
	}
	cmd.Flags().String("fields", "", "Fields to request (comma-separated)")
	cmd.Flags().Bool("summary", false, "Print a compact summary")
	cmd.Flags().String("jql", "", "Fetch issue metadata for the results of a JQL query")
	cmd.Flags().Int("limit", 20, "Max issues when using --jql (default: 20)")
	cmd.Flags().Bool("all", false, "Fetch all pages when using --jql")
	cmd.Flags().Int("page-size", 100, "Page size when using --jql --all (default: 100)")
	cmd.Flags().Int("concurrency", 4, "Number of concurrent issue fetches (default: 4, max: 10)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runInfo(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	fieldsFlag, _ := cmd.Flags().GetString("fields")
	summaryFlag, _ := cmd.Flags().GetBool("summary")
	jqlFlag, _ := cmd.Flags().GetString("jql")
	limit, _ := cmd.Flags().GetInt("limit")
	all, _ := cmd.Flags().GetBool("all")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	requestFields := fieldsFlag
	if requestFields == "" {
		requestFields = "summary,status,issuetype,assignee,reporter,priority,project,created,updated,labels,components,fixVersions,versions,duedate"
	}

	issueIDs, target, err := resolveInfoTargets(client, cmd, args, strings.TrimSpace(jqlFlag), limit, pageSize, all)
	if err != nil {
		return err
	}

	results, rawIssues, err := fetchInfoResults(client, issueIDs, requestFields, concurrency)
	if err != nil {
		return err
	}
	recordSearchRecents(client, rawIssues, "info")

	if mode == "json" {
		if len(results) == 1 && jqlFlag == "" && len(args) == 1 {
			result := results[0]
			if summaryFlag {
				result = map[string]any{"summary": results[0]["summary_info"]}
			}
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "info", target, result, nil, nil, "", "", "", nil))
		}
		payload := map[string]any{"issues": results, "summary": map[string]any{"count": len(results)}}
		if summaryFlag {
			summaries := make([]map[string]any, 0, len(results))
			for _, item := range results {
				summaries = append(summaries, item["summary_info"].(map[string]any))
			}
			payload["issues"] = summaries
		}
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "info", target, payload, nil, nil, "", "", "", nil))
	}

	if len(results) == 0 {
		if mode == "summary" {
			fmt.Println("Found 0 issues.")
			return nil
		}
		fmt.Println("No issues found.")
		return nil
	}

	if mode == "summary" {
		if len(results) == 1 {
			summaryInfo := results[0]["summary_info"].(map[string]any)
			fmt.Printf("%s: %s (Status: %s, Assignee: %s, Priority: %s)\n",
				summaryInfo["key"], summaryInfo["summary"], stringOr(summaryInfo["status"], "-"), stringOr(summaryInfo["assignee"], "-"), stringOr(summaryInfo["priority"], "-"))
			return nil
		}
		fmt.Printf("Fetched %d issue(s).\n", len(results))
		return nil
	}

	if summaryFlag || len(results) > 1 {
		for _, item := range results {
			summaryInfo := item["summary_info"].(map[string]any)
			labels, _ := summaryInfo["labels"].([]string)
			labelsStr := "-"
			if len(labels) > 0 {
				labelsStr = strings.Join(labels, ", ")
			}
			fmt.Printf("%s: %s\n", summaryInfo["key"], summaryInfo["summary"])
			fmt.Printf("Status: %s | Assignee: %s | Priority: %s\n", summaryInfo["status"], summaryInfo["assignee"], summaryInfo["priority"])
			fmt.Printf("Labels: %s | URL: %s\n\n", labelsStr, summaryInfo["url"])
		}
		return nil
	}

	info := results[0]
	labels, _ := info["labels"].([]string)
	labelsStr := "-"
	if len(labels) > 0 {
		labelsStr = strings.Join(labels, ", ")
	}
	components, _ := info["components"].([]string)
	componentsStr := "-"
	if len(components) > 0 {
		componentsStr = strings.Join(components, ", ")
	}
	fixVersions, _ := info["fixVersions"].([]string)
	fixVersionsStr := "-"
	if len(fixVersions) > 0 {
		fixVersionsStr = strings.Join(fixVersions, ", ")
	}
	versions, _ := info["versions"].([]string)
	versionsStr := "-"
	if len(versions) > 0 {
		versionsStr = strings.Join(versions, ", ")
	}

	fmt.Printf("Key:       %v\n", info["key"])
	fmt.Printf("ID:        %v\n", info["id"])
	fmt.Printf("Summary:   %v\n", info["summary"])
	fmt.Printf("Status:    %v\n", info["status"])
	fmt.Printf("Type:      %v\n", info["type"])
	fmt.Printf("Assignee:  %v\n", info["assignee"])
	fmt.Printf("Reporter:  %v\n", info["reporter"])
	fmt.Printf("Priority:  %v\n", info["priority"])
	fmt.Printf("Project:   %v\n", info["project"])
	fmt.Printf("Created:   %v\n", info["created"])
	fmt.Printf("Updated:   %v\n", info["updated"])
	fmt.Printf("Due:       %v\n", info["due"])
	fmt.Printf("Labels:    %s\n", labelsStr)
	fmt.Printf("Components: %s\n", componentsStr)
	fmt.Printf("FixVersions: %s\n", fixVersionsStr)
	fmt.Printf("Versions:  %s\n", versionsStr)
	fmt.Printf("URL:       %v\n", info["url"])
	return nil
}

func resolveInfoTargets(client *Client, cmd *cobra.Command, args []string, jql string, limit, pageSize int, fetchAll bool) ([]string, map[string]any, error) {
	if jql != "" {
		issues, total, err := searchAllIssues(client, applyDefaultScope(cmd, jql), limit, 0, pageSize, "summary,status,assignee,priority", "", fetchAll)
		if err != nil {
			return nil, nil, err
		}
		keys := make([]string, 0, len(issues))
		for _, issue := range issues {
			if key := normalizeMaybeString(issue["key"]); key != "" {
				keys = append(keys, key)
			}
		}
		return keys, map[string]any{"jql": jql, "total": total}, nil
	}
	if len(args) == 0 {
		return nil, nil, &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide one or more issues, or use --jql.", ExitCode: 2}
	}
	keys := make([]string, 0, len(args))
	for _, arg := range args {
		keys = append(keys, ResolveIssueIdentifier(arg))
	}
	target := map[string]any{"issues": keys}
	if len(keys) == 1 {
		target = map[string]any{"issue": keys[0]}
	}
	return keys, target, nil
}

func fetchInfoResults(client *Client, issueIDs []string, fields string, concurrency int) ([]map[string]any, []map[string]any, error) {
	type infoResult struct {
		info map[string]any
		raw  map[string]any
		err  error
	}
	results := cli.RunParallel(len(issueIDs), concurrency, func(index int) infoResult {
		issue, err := client.GetIssue(issueIDs[index], fields, "")
		if err != nil {
			return infoResult{err: err}
		}
		return infoResult{
			info: summarizeIssueInfo(client, issue),
			raw:  issue,
		}
	})

	infos := make([]map[string]any, 0, len(results))
	raws := make([]map[string]any, 0, len(results))
	for _, result := range results {
		if result.err != nil {
			return nil, nil, result.err
		}
		infos = append(infos, result.info)
		raws = append(raws, result.raw)
	}
	return infos, raws, nil
}

func summarizeIssueInfo(client *Client, issue map[string]any) map[string]any {
	fd, _ := issue["fields"].(map[string]any)
	if fd == nil {
		fd = map[string]any{}
	}
	info := map[string]any{
		"id":          issue["id"],
		"key":         issue["key"],
		"summary":     fd["summary"],
		"status":      safeString(fd, "status", "name"),
		"type":        safeString(fd, "issuetype", "name"),
		"assignee":    safeString(fd, "assignee", "displayName"),
		"reporter":    safeString(fd, "reporter", "displayName"),
		"priority":    safeString(fd, "priority", "name"),
		"project":     safeString(fd, "project", "key"),
		"created":     fd["created"],
		"updated":     fd["updated"],
		"due":         fd["duedate"],
		"labels":      safeStringSlice(fd, "labels"),
		"components":  extractNames(fd, "components"),
		"fixVersions": extractNames(fd, "fixVersions"),
		"versions":    extractNames(fd, "versions"),
	}
	if key := normalizeMaybeString(issue["key"]); key != "" && client != nil {
		info["url"] = fmt.Sprintf("%s/browse/%s", client.BaseURL(), key)
	}
	info["summary_info"] = map[string]any{
		"key":      info["key"],
		"summary":  info["summary"],
		"status":   info["status"],
		"assignee": info["assignee"],
		"priority": info["priority"],
		"labels":   info["labels"],
		"url":      info["url"],
	}
	return info
}

func stringOr(v any, fallback string) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return fallback
	}
	return s
}
