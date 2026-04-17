package jira

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewGetCmd creates the "get" subcommand.
func NewGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <issue...>",
		Short: "Fetch full issue JSON",
		Args:  cobra.ArbitraryArgs,
		RunE:  runGet,
	}
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cmd.Flags().String("fields", "", "Fields to request (comma-separated)")
	cmd.Flags().String("expand", "", "Expand options (comma-separated)")
	cmd.Flags().Bool("summary", false, "Print a compact summary")
	cmd.Flags().String("jql", "", "Fetch full issue JSON for the results of a JQL query")
	cmd.Flags().Int("limit", 20, "Max issues when using --jql (default: 20)")
	cmd.Flags().Bool("all", false, "Fetch all pages when using --jql")
	cmd.Flags().Int("page-size", 100, "Page size when using --jql --all (default: 100)")
	cmd.Flags().Int("concurrency", 4, "Number of concurrent issue fetches (default: 4, max: 10)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runGet(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	fieldsFlag, _ := cmd.Flags().GetString("fields")
	expandFlag, _ := cmd.Flags().GetString("expand")
	outputFile, _ := cmd.Flags().GetString("output")
	summaryFlag, _ := cmd.Flags().GetBool("summary")
	jqlFlag, _ := cmd.Flags().GetString("jql")
	limit, _ := cmd.Flags().GetInt("limit")
	all, _ := cmd.Flags().GetBool("all")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	concurrency, _ := cmd.Flags().GetInt("concurrency")

	issueIDs, target, err := resolveInfoTargets(client, cmd, args, strings.TrimSpace(jqlFlag), limit, pageSize, all)
	if err != nil {
		return err
	}
	type getResult struct {
		issue map[string]any
		err   error
	}
	fetched := cli.RunParallel(len(issueIDs), concurrency, func(index int) getResult {
		issue, err := client.GetIssue(issueIDs[index], fieldsFlag, expandFlag)
		if err != nil {
			return getResult{err: err}
		}
		return getResult{issue: issue}
	})
	results := make([]map[string]any, 0, len(fetched))
	summaries := make([]map[string]any, 0, len(fetched))
	for _, item := range fetched {
		if item.err != nil {
			return item.err
		}
		results = append(results, item.issue)
		summaries = append(summaries, summarizeIssueInfo(client, item.issue)["summary_info"].(map[string]any))
	}
	recordSearchRecents(client, results, "get")

	if outputFile != "" {
		var payload any
		if len(results) == 1 && jqlFlag == "" && len(args) == 1 {
			payload = results[0]
		} else {
			payload = map[string]any{"issues": results}
		}
		jsonBytes, _ := json.MarshalIndent(payload, "", "  ")
		if err := writeFile(outputFile, string(jsonBytes)); err != nil {
			return err
		}
		if mode == "json" {
			result := map[string]any{"saved_to": outputFile}
			if summaryFlag {
				result["summary"] = summaries
			}
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "get", target, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Saved %d issue(s) to %s.\n", len(results), outputFile)
			return nil
		}
		fmt.Printf("Saved issue JSON to: %s\n", outputFile)
		if summaryFlag {
			for _, summary := range summaries {
				printCompactSummary(summary)
			}
		}
		return nil
	}

	if mode == "json" {
		if len(results) == 1 && jqlFlag == "" && len(args) == 1 {
			result := any(results[0])
			if summaryFlag {
				result = map[string]any{"summary": summaries[0]}
			}
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "get", target, result, nil, nil, "", "", "", nil))
		}
		result := map[string]any{"issues": results, "summary": map[string]any{"count": len(results)}}
		if summaryFlag {
			result["issues"] = summaries
		}
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "get", target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Fetched %d issue(s).\n", len(results))
		return nil
	}
	if summaryFlag {
		for _, summary := range summaries {
			printCompactSummary(summary)
			fmt.Println()
		}
		return nil
	}
	if len(results) == 1 && jqlFlag == "" && len(args) == 1 {
		jsonBytes, _ := json.MarshalIndent(results[0], "", "  ")
		fmt.Println(string(jsonBytes))
		return nil
	}
	for _, issue := range results {
		jsonBytes, _ := json.MarshalIndent(issue, "", "  ")
		fmt.Println(string(jsonBytes))
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

func ensureGetArgsOrJQL(args []string, jql string) error {
	if len(args) == 0 && strings.TrimSpace(jql) == "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide one or more issues, or use --jql.", ExitCode: 2}
	}
	return nil
}
