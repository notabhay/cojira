package jira

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewGetCmd creates the "get" subcommand.
func NewGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <issue>",
		Short: "Fetch full issue JSON",
		Args:  cobra.ExactArgs(1),
		RunE:  runGet,
	}
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.Flags().String("fields", "", "Fields to request (comma-separated)")
	cmd.Flags().String("expand", "", "Expand options (comma-separated)")
	cmd.Flags().Bool("summary", false, "Print a compact summary")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runGet(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	fieldsFlag, _ := cmd.Flags().GetString("fields")
	expandFlag, _ := cmd.Flags().GetString("expand")
	outputFile, _ := cmd.Flags().GetString("output")
	summaryFlag, _ := cmd.Flags().GetBool("summary")

	issue, err := client.GetIssue(issueID, fieldsFlag, expandFlag)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(errorCode(err, "FETCH_FAILED"), err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "get",
				map[string]any{"issue": issueID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		return err
	}

	fd, _ := issue["fields"].(map[string]any)
	if fd == nil {
		fd = map[string]any{}
	}
	key, _ := issue["key"].(string)
	if key == "" {
		key = issueID
	}
	summaryInfo := map[string]any{
		"key":      key,
		"summary":  fd["summary"],
		"status":   safeString(fd, "status", "name"),
		"assignee": stringOr(safeString(fd, "assignee", "displayName"), "Unassigned"),
		"priority": stringOr(safeString(fd, "priority", "name"), "None"),
		"labels":   safeStringSlice(fd, "labels"),
		"url":      fmt.Sprintf("%s/browse/%s", client.BaseURL(), key),
	}

	jsonBytes, _ := json.MarshalIndent(issue, "", "  ")
	jsonStr := string(jsonBytes)

	if outputFile != "" {
		if err := writeFile(outputFile, jsonStr); err != nil {
			return err
		}
		if mode == "json" {
			result := map[string]any{"schema": "jira.issue.saved/v1", "saved_to": outputFile}
			if summaryFlag {
				result["summary"] = summaryInfo
			}
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "get",
				map[string]any{"issue": issueID},
				result, nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved %s to %s.\n", summaryInfo["key"], outputFile)
			return nil
		}
		fmt.Printf("Saved issue JSON to: %s\n", outputFile)
		if summaryFlag {
			printCompactSummary(summaryInfo)
		}
		return nil
	}

	if mode == "json" {
		result := deepCopyMap(issue)
		result["schema"] = "jira.issue/v1"
		result["url"] = summaryInfo["url"]
		if summaryFlag {
			result = map[string]any{"schema": "jira.issue.summary/v1", "summary": summaryInfo}
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "get",
			map[string]any{"issue": issueID},
			result, nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Fetched %s (content omitted in summary mode).\n", summaryInfo["key"])
		return nil
	}
	if summaryFlag {
		printCompactSummary(summaryInfo)
	} else {
		fmt.Println(jsonStr)
	}
	return nil
}

func printCompactSummary(info map[string]any) {
	labels, _ := info["labels"].([]string)
	labelsStr := "-"
	if len(labels) > 0 {
		labelsStr = strings.Join(labels, ", ")
	}
	fmt.Printf("%s: %s\n", info["key"], info["summary"])
	fmt.Printf("Status: %s | Assignee: %s | Priority: %s\n", info["status"], info["assignee"], info["priority"])
	fmt.Printf("Labels: %s | URL: %s\n", labelsStr, info["url"])
}
