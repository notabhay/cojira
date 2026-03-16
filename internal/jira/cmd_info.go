package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewInfoCmd creates the "info" subcommand.
func NewInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <issue>",
		Short: "Show issue metadata",
		Args:  cobra.ExactArgs(1),
		RunE:  runInfo,
	}
	cmd.Flags().String("fields", "", "Fields to request (comma-separated)")
	cmd.Flags().Bool("summary", false, "Print a compact summary")
	cmd.Flags().Bool("full", false, "Include the description in human output")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runInfo(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	fieldsFlag, _ := cmd.Flags().GetString("fields")
	summaryFlag, _ := cmd.Flags().GetBool("summary")
	fullFlag, _ := cmd.Flags().GetBool("full")

	fields := fieldsFlag
	if fields == "" {
		fields = "summary,status,issuetype,assignee,reporter,priority,project,created,updated,labels,components,fixVersions,versions,duedate"
		if mode == "json" || fullFlag {
			fields += ",description"
		}
	}

	issue, err := client.GetIssue(issueID, fields, "")
	if err != nil {
		return err
	}

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
		"description": fd["description"],
		"labels":      safeStringSlice(fd, "labels"),
		"components":  extractNames(fd, "components"),
		"fixVersions": extractNames(fd, "fixVersions"),
		"versions":    extractNames(fd, "versions"),
	}
	if key, ok := issue["key"].(string); ok && key != "" {
		info["url"] = fmt.Sprintf("%s/browse/%s", client.BaseURL(), key)
	}

	labels := safeStringSlice(fd, "labels")

	summaryInfo := map[string]any{
		"key":      info["key"],
		"summary":  info["summary"],
		"status":   info["status"],
		"assignee": info["assignee"],
		"priority": info["priority"],
		"labels":   labels,
		"url":      info["url"],
	}

	if mode == "json" {
		result := info
		if summaryFlag {
			result = map[string]any{"summary": summaryInfo}
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "info",
			map[string]any{"issue": issueID},
			result, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		status := stringOr(info["status"], "-")
		assignee := stringOr(info["assignee"], "-")
		priority := stringOr(info["priority"], "-")
		fmt.Printf("%s: %s (Status: %s, Assignee: %s, Priority: %s)\n",
			summaryInfo["key"], summaryInfo["summary"], status, assignee, priority)
		return nil
	}

	if summaryFlag {
		labelsStr := "-"
		if len(labels) > 0 {
			labelsStr = strings.Join(labels, ", ")
		}
		fmt.Printf("%s: %s\n", summaryInfo["key"], summaryInfo["summary"])
		fmt.Printf("Status: %s | Assignee: %s | Priority: %s\n",
			summaryInfo["status"], summaryInfo["assignee"], summaryInfo["priority"])
		fmt.Printf("Labels: %s | URL: %s\n", labelsStr, summaryInfo["url"])
		return nil
	}

	labelSlice, _ := info["labels"].([]string)
	labelsStr := "-"
	if len(labelSlice) > 0 {
		labelsStr = strings.Join(labelSlice, ", ")
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
	if fullFlag {
		fmt.Printf("Description:\n%v\n", stringOr(info["description"], ""))
	}
	fmt.Printf("URL:       %v\n", info["url"])
	return nil
}

func stringOr(v any, fallback string) string {
	s, ok := v.(string)
	if !ok || s == "" {
		return fallback
	}
	return s
}
