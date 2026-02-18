package jira

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cojira/cojira/internal/cli"
	"github.com/cojira/cojira/internal/config"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewSyncCmd creates the "sync" subcommand.
func NewSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync",
		Short: "Sync reporter's issues to local folders",
		Long:  "Fetch issues reported by a user and write issue.json snapshots to disk.",
		RunE:  runSync,
	}
	cmd.Flags().String("project", "", "Project key (required unless set via JIRA_PROJECT)")
	cmd.Flags().String("reporter", "currentUser()", "Reporter (default: currentUser())")
	cmd.Flags().String("base-dir", "0-JIRA", "Base output directory (default: 0-JIRA)")
	cmd.Flags().String("since", "", "Only include issues created on/after this date")
	cmd.Flags().String("updated-since", "", "Only include issues updated on/after this date")
	cmd.Flags().Int("limit", 100, "Page size for search (default: 100)")
	cmd.Flags().Float64("sleep", 1.0, "Delay between fetches in seconds")
	cmd.Flags().Bool("force", false, "Overwrite existing issue.json files")
	cmd.Flags().Bool("plan", false, "Preview without writing files")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runSync(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	projectFlag, _ := cmd.Flags().GetString("project")
	reporter, _ := cmd.Flags().GetString("reporter")
	baseDir, _ := cmd.Flags().GetString("base-dir")
	since, _ := cmd.Flags().GetString("since")
	updatedSince, _ := cmd.Flags().GetString("updated-since")
	limit, _ := cmd.Flags().GetInt("limit")
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	force, _ := cmd.Flags().GetBool("force")
	plan, _ := cmd.Flags().GetBool("plan")
	quiet, _ := cmd.Flags().GetBool("quiet")

	project := strings.TrimSpace(projectFlag)
	if project == "" {
		project = strings.TrimSpace(os.Getenv("JIRA_PROJECT"))
	}
	if project == "" {
		cfg, err := config.LoadProjectConfig(nil)
		if err == nil && cfg != nil {
			if dp, ok := cfg.GetValue([]string{"jira", "default_project"}, "").(string); ok && dp != "" {
				project = dp
			}
		}
	}
	if project == "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Project key is required.", ExitCode: 2}
	}

	reporter = strings.TrimSpace(reporter)
	if reporter == "" {
		reporter = "currentUser()"
	}

	reqID := output.RequestID()

	// Build JQL.
	jqlParts := []string{
		fmt.Sprintf("project = %s", JQLValue(project)),
		fmt.Sprintf("reporter = %s", JQLValue(reporter)),
	}
	if since != "" {
		jqlParts = append(jqlParts, fmt.Sprintf("created >= %s", JQLValue(since)))
	}
	if updatedSince != "" {
		jqlParts = append(jqlParts, fmt.Sprintf("updated >= %s", JQLValue(updatedSince)))
	}
	jql := strings.Join(jqlParts, " AND ") + " ORDER BY created DESC"

	outputDir := filepath.Join(baseDir, project)

	// Collect all keys.
	var allKeys []string
	startAt := 0
	for {
		data, err := client.Search(jql, limit, startAt, "summary,created,updated", "")
		if err != nil {
			return err
		}
		issues, _ := data["issues"].([]any)
		total := intFromAny(data["total"], 0)
		for _, i := range issues {
			if m, ok := i.(map[string]any); ok {
				if key, ok := m["key"].(string); ok && key != "" {
					allKeys = append(allKeys, key)
				}
			}
		}
		startAt += len(issues)
		if startAt >= total || len(issues) == 0 {
			break
		}
	}

	if plan {
		summary := map[string]any{
			"fetched":    0,
			"skipped":    0,
			"failed":     0,
			"total":      len(allKeys),
			"output_dir": outputDir,
			"jql":        jql,
			"dry_run":    true,
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "sync",
				map[string]any{"project": project, "reporter": reporter, "base_dir": baseDir},
				map[string]any{"summary": summary, "request_id": reqID},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would sync %d issue(s) to %s.\n", len(allKeys), outputDir)
			return nil
		}
		fmt.Printf("Plan: would sync %d issue(s) to %s.\n", len(allKeys), outputDir)
		return nil
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	fetched := 0
	skipped := 0
	failureCount := 0
	var failureDetails []failureEntry

	for idx, key := range allKeys {
		ticketDir := filepath.Join(outputDir, key)
		if err := os.MkdirAll(ticketDir, 0o755); err != nil {
			return err
		}
		outPath := filepath.Join(ticketDir, "issue.json")

		if !force {
			if info, err := os.Stat(outPath); err == nil && !info.IsDir() {
				skipped++
				continue
			}
		}

		var fetchErr error
		for attempt := 0; attempt < 5; attempt++ {
			issue, e := client.GetIssue(key, "", "")
			if e != nil {
				msg := strings.ToLower(e.Error())
				if strings.Contains(msg, "rate limit") || strings.Contains(msg, "429") {
					time.Sleep(time.Duration(1<<uint(attempt)) * time.Second)
					continue
				}
				fetchErr = e
				break
			}
			b, _ := json.MarshalIndent(issue, "", "  ")
			if e := os.WriteFile(outPath, b, 0o644); e != nil {
				fetchErr = e
				break
			}
			fetched++
			fetchErr = nil
			break
		}
		if fetchErr != nil {
			failureCount++
			if mode != "json" && !quiet {
				_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Failed to fetch %s: %v\n", key, fetchErr)
			}
			failureDetails = append(failureDetails, failureEntry{key: key, err: fetchErr.Error()})
		}

		if idx < len(allKeys)-1 && sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	summary := map[string]any{
		"fetched":    fetched,
		"skipped":    skipped,
		"failed":     failureCount,
		"total":      len(allKeys),
		"output_dir": outputDir,
		"jql":        jql,
	}

	if mode == "json" {
		var failuresJSON []any
		for _, f := range failureDetails {
			failuresJSON = append(failuresJSON, map[string]any{"issue": f.key, "error": f.err})
		}
		var errs []any
		for _, f := range failureDetails {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, fmt.Sprintf("%s: %s", f.key, f.err), "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			failureCount == 0, "jira", "sync",
			map[string]any{"project": project, "reporter": reporter, "base_dir": baseDir},
			map[string]any{"summary": summary, "failures": failuresJSON, "request_id": reqID},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Sync complete: %d fetched, %d skipped, %d failed.\n", fetched, skipped, failureCount)
		if failureCount > 0 {
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("Sync complete: %d fetched, %d skipped, %d failed, total %d\n", fetched, skipped, failureCount, len(allKeys))
		printFailures(failureDetails)
		fmt.Printf("Output: %s\n", outputDir)
	}
	if failureCount > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}
