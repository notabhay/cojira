package jira

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

var (
	boardIDRe    = regexp.MustCompile(`^\d+$`)
	boardsPathRe = regexp.MustCompile(`/boards/(\d+)`)
	rapidViewRe  = regexp.MustCompile(`rapidView(?:Id)?=(\d+)`)
)

// resolveBoardIdentifier resolves a flexible board identifier to a board ID string.
// This duplicates board.ResolveBoardIdentifier to avoid a circular dependency.
func resolveBoardIdentifier(identifier string) string {
	raw := strings.TrimSpace(identifier)
	if raw == "" {
		return raw
	}
	if boardIDRe.MatchString(raw) {
		return raw
	}
	if strings.Contains(raw, "rapidView=") || strings.Contains(raw, "rapidViewId=") {
		qs, err := url.ParseQuery(strings.TrimLeft(raw, "?"))
		if err == nil {
			for _, key := range []string{"rapidView", "rapidViewId"} {
				if vals := qs[key]; len(vals) > 0 && vals[0] != "" {
					return vals[0]
				}
			}
		}
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		parsed, err := url.Parse(raw)
		if err == nil {
			qs := parsed.Query()
			for _, key := range []string{"rapidView", "rapidViewId"} {
				if vals := qs[key]; len(vals) > 0 && vals[0] != "" {
					return vals[0]
				}
			}
			if m := boardsPathRe.FindStringSubmatch(parsed.Path); m != nil {
				return m[1]
			}
		}
	}
	if m := rapidViewRe.FindStringSubmatch(raw); m != nil {
		return m[1]
	}
	return raw
}

// NewBoardIssuesCmd creates the "board-issues" subcommand.
func NewBoardIssuesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board-issues <board>",
		Short: "List issues on a board (Agile REST API)",
		Long:  "List issues on a Jira board using the Agile REST API.",
		Args:  cobra.ExactArgs(1),
		RunE:  runBoardIssues,
	}
	cmd.Flags().String("jql", "", "Additional JQL filter")
	cmd.Flags().Int("limit", 50, "Max results per page (default: 50)")
	cmd.Flags().Int("start", 0, "Start offset (default: 0)")
	cmd.Flags().Bool("all", false, "Fetch all issues on the board (paginate)")
	cmd.Flags().Int("max-issues", 10000, "Safety cap for --all (default: 10000; set 0 for unlimited)")
	cmd.Flags().String("fields", "", "Fields to request (comma-separated)")
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runBoardIssues(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	boardID := resolveBoardIdentifier(args[0])
	jqlFlag, _ := cmd.Flags().GetString("jql")
	if jqlFlag != "" {
		jqlFlag = FixJQLShellEscapes(jqlFlag)
	}
	pageSize, _ := cmd.Flags().GetInt("limit")
	startAt, _ := cmd.Flags().GetInt("start")
	fetchAll, _ := cmd.Flags().GetBool("all")
	maxIssues, _ := cmd.Flags().GetInt("max-issues")
	fieldsFlag, _ := cmd.Flags().GetString("fields")
	outputFile, _ := cmd.Flags().GetString("output")

	var issues []map[string]any
	var total int
	truncated := false
	curStart := startAt

	for {
		page, err := client.GetBoardIssues(boardID, jqlFlag, pageSize, curStart, fieldsFlag)
		if err != nil {
			return err
		}
		pageIssues, _ := page["issues"].([]any)
		for _, pi := range pageIssues {
			if m, ok := pi.(map[string]any); ok {
				issues = append(issues, m)
			}
		}
		if total == 0 {
			total = intFromAny(page["total"], len(pageIssues))
		}
		curStart += len(pageIssues)
		if !fetchAll {
			break
		}
		if maxIssues > 0 && len(issues) >= maxIssues {
			truncated = true
			break
		}
		if curStart >= total || len(pageIssues) == 0 {
			break
		}
	}

	data := map[string]any{
		"startAt":    startAt,
		"maxResults": pageSize,
		"total":      total,
		"fetched":    len(issues),
		"all":        fetchAll,
		"truncated":  truncated,
		"maxIssues":  maxIssues,
		"issues":     issues,
	}

	if outputFile != "" {
		jsonBytes, _ := json.MarshalIndent(data, "", "  ")
		if err := writeFile(outputFile, string(jsonBytes)); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "board-issues",
				map[string]any{"board": boardID},
				map[string]any{
					"saved_to":  outputFile,
					"fetched":   len(issues),
					"total":     total,
					"all":       fetchAll,
					"truncated": truncated,
					"maxIssues": maxIssues,
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved %d issue(s) to %s.\n", len(issues), outputFile)
			return nil
		}
		fmt.Printf("Saved board issues (%d) to: %s\n", len(issues), outputFile)
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "board-issues",
			map[string]any{"board": boardID},
			data, nil, nil, "", "", "", nil,
		))
	}

	if len(issues) == 0 {
		fmt.Println("No issues found on this board.")
		return nil
	}

	if mode == "summary" {
		if truncated {
			fmt.Printf("Found %d of %d issue(s) on board %s (stopped early).\n", len(issues), total, boardID)
		} else if len(issues) == total {
			fmt.Printf("Found %d issue(s) on board %s.\n", total, boardID)
		} else {
			fmt.Printf("Found %d of %d issue(s) on board %s.\n", len(issues), total, boardID)
		}
		return nil
	}

	if truncated {
		fmt.Printf("Found %d of %d issue(s) on board %s (stopped early):\n\n", len(issues), total, boardID)
	} else if len(issues) == total {
		fmt.Printf("Found %d issue(s) on board %s:\n\n", total, boardID)
	} else {
		fmt.Printf("Found %d of %d issue(s) on board %s:\n\n", len(issues), total, boardID)
	}
	for _, issue := range issues {
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
