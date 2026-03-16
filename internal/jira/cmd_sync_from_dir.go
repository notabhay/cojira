package jira

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewSyncFromDirCmd creates the "sync-from-dir" subcommand.
func NewSyncFromDirCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync-from-dir",
		Short: "Update issues from local ticket folders",
		Long:  "Update summaries/descriptions by reading local ticket specs.",
		RunE:  runSyncFromDir,
	}
	cmd.Flags().String("root", ".", "Root directory to scan (default: .)")
	cmd.Flags().String("pattern", "**/*-*-*", "Glob pattern for ticket folders")
	cmd.Flags().String("ticket-subdir", "0-ticket", "Subdirectory containing spec files")
	cmd.Flags().String("spec-glob", "0-*.txt", "Glob for spec files within ticket subdir")
	cmd.Flags().String("summary-template", "{title}", "Template for summary: {title}, {order}, {key}")
	cmd.Flags().Bool("skip-summary", false, "Do not update summary")
	cmd.Flags().Bool("skip-description", false, "Do not update description")
	cmd.Flags().Int("limit", 0, "Limit number of tickets processed")
	cmd.Flags().Float64("sleep", 0.0, "Delay between updates in seconds")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview without changes")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

var folderIssueKeyRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9_]+-\d+)$`)
var orderPrefixRe = regexp.MustCompile(`^(\d+)-`)

func runSyncFromDir(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	root, _ := cmd.Flags().GetString("root")
	pattern, _ := cmd.Flags().GetString("pattern")
	ticketSubdir, _ := cmd.Flags().GetString("ticket-subdir")
	specGlob, _ := cmd.Flags().GetString("spec-glob")
	summaryTemplate, _ := cmd.Flags().GetString("summary-template")
	skipSummary, _ := cmd.Flags().GetBool("skip-summary")
	skipDescription, _ := cmd.Flags().GetBool("skip-description")
	limit, _ := cmd.Flags().GetInt("limit")
	sleepSec, _ := cmd.Flags().GetFloat64("sleep")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	quiet, _ := cmd.Flags().GetBool("quiet")

	reqID := output.RequestID()

	if _, err := os.Stat(root); err != nil {
		return &cerrors.CojiraError{
			Code:     cerrors.FileNotFound,
			Message:  fmt.Sprintf("Root directory not found: %s", root),
			ExitCode: 1,
		}
	}

	// Find matching directories.
	matches, err := findMatchingDirs(root, pattern)
	if err != nil {
		return err
	}
	var dirs []string
	for _, m := range matches {
		info, err := os.Stat(m)
		if err == nil && info.IsDir() {
			dirs = append(dirs, m)
		}
	}
	sort.Strings(dirs)

	if limit > 0 && len(dirs) > limit {
		dirs = dirs[:limit]
	}

	if len(dirs) == 0 {
		fmt.Println("No matching ticket folders found.")
		return nil
	}

	success := 0
	var failures []failureEntry
	var items []map[string]any

	if dryRun && mode != "json" && !quiet && mode != "summary" {
		fmt.Print("[DRY-RUN MODE - no changes will be made]\n\n")
	}

	for idx, ticketDir := range dirs {
		dirName := filepath.Base(ticketDir)
		item := map[string]any{"op": "update", "target": map[string]any{"ticket_dir": ticketDir}, "ok": false}

		// Extract issue key from folder name.
		match := folderIssueKeyRe.FindStringSubmatch(dirName)
		if match == nil {
			message := "Unable to parse issue key from folder name."
			failures = append(failures, failureEntry{key: dirName, err: message})
			item["error"] = message
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %s", dirName, message)}
			item["receipt"] = r.Format()
			items = append(items, item)
			output.EmitProgress(mode, quiet, idx+1, len(dirs), dirName, "FAILED")
			continue
		}
		issueID := match[1]
		item["target"].(map[string]any)["issue"] = issueID

		// Extract order prefix.
		order := ""
		orderMatch := orderPrefixRe.FindStringSubmatch(dirName)
		if orderMatch != nil {
			order = orderMatch[1]
		}

		// Find spec files.
		specDir, err := safeJoinUnder(ticketDir, ticketSubdir)
		if err != nil {
			failures = append(failures, failureEntry{key: issueID, err: err.Error()})
			item["error"] = err.Error()
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", issueID, err)}
			item["receipt"] = r.Format()
			items = append(items, item)
			output.EmitProgress(mode, quiet, idx+1, len(dirs), issueID, "FAILED")
			continue
		}
		specMatches, _ := filepath.Glob(filepath.Join(specDir, specGlob))
		sort.Strings(specMatches)
		if len(specMatches) == 0 {
			message := fmt.Sprintf("No spec files found in %s", specDir)
			failures = append(failures, failureEntry{key: issueID, err: message})
			item["error"] = message
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %s", issueID, message)}
			item["receipt"] = r.Format()
			items = append(items, item)
			output.EmitProgress(mode, quiet, idx+1, len(dirs), issueID, "FAILED")
			continue
		}

		specFile := specMatches[0]
		item["target"].(map[string]any)["spec_file"] = specFile
		title := normalizeTitleFromFilename(specFile)

		payloadFields := map[string]any{}
		if !skipSummary {
			tmpl := summaryTemplate
			if tmpl == "" {
				tmpl = "{title}"
			}
			summaryText := tmpl
			summaryText = strings.ReplaceAll(summaryText, "{title}", title)
			summaryText = strings.ReplaceAll(summaryText, "{order}", order)
			summaryText = strings.ReplaceAll(summaryText, "{key}", issueID)
			payloadFields["summary"] = summaryText
		}

		if !skipDescription {
			content, e := readTextFile(specFile)
			if e != nil {
				failures = append(failures, failureEntry{key: issueID, err: e.Error()})
				item["error"] = e.Error()
				r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", issueID, e)}
				item["receipt"] = r.Format()
				items = append(items, item)
				output.EmitProgress(mode, quiet, idx+1, len(dirs), issueID, "FAILED")
				continue
			}
			payloadFields["description"] = content
		}

		if len(payloadFields) == 0 {
			message := "No fields to update (both summary and description skipped)."
			failures = append(failures, failureEntry{key: issueID, err: message})
			item["error"] = message
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %s", issueID, message)}
			item["receipt"] = r.Format()
			items = append(items, item)
			output.EmitProgress(mode, quiet, idx+1, len(dirs), issueID, "FAILED")
			continue
		}

		payload := map[string]any{"fields": payloadFields}

		var opErr error
		if dryRun {
			fieldKeys := make([]string, 0, len(payloadFields))
			for k := range payloadFields {
				fieldKeys = append(fieldKeys, k)
			}
			issue, e := client.GetIssue(issueID, strings.Join(fieldKeys, ","), "")
			if e != nil {
				opErr = e
			} else {
				diffs := previewPayloadDiff(issueID, issue, payload, mode == "json" || quiet)
				item["diffs"] = diffs
			}
		} else {
			if e := client.UpdateIssue(issueID, payload, !noNotify); e != nil {
				opErr = e
			} else {
				r := output.Receipt{OK: true, Message: fmt.Sprintf("Updated %s", issueID)}
				item["receipt"] = r.Format()
				if mode != "json" && !quiet && mode != "summary" {
					fmt.Println(r.Format())
				}
			}
		}

		if opErr != nil {
			item["ok"] = false
			item["error"] = opErr.Error()
			r := output.Receipt{OK: false, Message: fmt.Sprintf("%s: %v", issueID, opErr)}
			item["receipt"] = r.Format()
			if mode != "json" && !quiet && mode != "summary" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), r.Format())
			}
			failures = append(failures, failureEntry{key: issueID, err: opErr.Error()})
		} else {
			item["ok"] = true
			success++
		}

		items = append(items, item)
		status := "OK"
		if !item["ok"].(bool) {
			status = "FAILED"
		}
		output.EmitProgress(mode, quiet, idx+1, len(dirs), issueID, status)

		if sleepSec > 0 {
			time.Sleep(time.Duration(sleepSec * float64(time.Second)))
		}
	}

	summary := map[string]any{
		"total":   len(dirs),
		"ok":      success,
		"failed":  len(failures),
		"dry_run": dryRun,
	}

	if mode == "json" {
		var errs []any
		for _, f := range failures {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, fmt.Sprintf("%s: %s", f.key, f.err), "", "", nil)
			errs = append(errs, errObj)
		}
		return output.PrintJSON(output.BuildEnvelope(
			len(failures) == 0, "jira", "sync-from-dir",
			map[string]any{"root": root, "pattern": pattern},
			map[string]any{"items": items, "summary": summary, "request_id": reqID},
			nil, errs, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Sync-from-dir complete: %d succeeded, %d failed.\n", success, len(failures))
		if len(failures) > 0 {
			return &cerrors.CojiraError{ExitCode: 1}
		}
		return nil
	}

	if !quiet {
		fmt.Printf("\nSummary: %d succeeded, %d failed\n", success, len(failures))
		printFailures(failures)
	}
	if len(failures) > 0 {
		return &cerrors.CojiraError{ExitCode: 1}
	}
	return nil
}
