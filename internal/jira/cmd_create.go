package jira

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCreateCmd creates the "create" subcommand.
func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <file>",
		Short: "Create issue from JSON payload",
		Args:  cobra.ExactArgs(1),
		RunE:  runCreate,
	}
	cmd.Flags().Bool("dry-run", false, "Preview without creating")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runCreate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	file := args[0]
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	payload, err := readJSONFile(file)
	if err != nil {
		absFile, _ := filepath.Abs(file)
		cwd, _ := os.Getwd()
		if mode == "json" {
			code := cerrors.FileNotFound
			if ce, ok := err.(*cerrors.CojiraError); ok && ce.Code != "" {
				code = ce.Code
			}
			errObj, _ := output.ErrorObj(code, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "create",
				map[string]any{"file": file, "absolute_file": absFile, "cwd": cwd},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		return err
	}

	fields, _ := payload["fields"].(map[string]any)
	summaryText, _ := fields["summary"].(string)
	var project string
	if proj, ok := fields["project"].(map[string]any); ok {
		project, _ = proj["key"].(string)
	}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "create",
				map[string]any{"file": file},
				map[string]any{
					"dry_run":     true,
					"summary":     summaryText,
					"project":     project,
					"idempotency": map[string]any{"key": output.IdempotencyKey("jira.create", payload)},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			detail := ""
			if project != "" {
				detail = fmt.Sprintf(" (project %s)", project)
			}
			summaryPart := ""
			if summaryText != "" {
				summaryPart = fmt.Sprintf(": %s", summaryText)
			}
			fmt.Printf("Would create Jira issue%s%s.\n", detail, summaryPart)
			return nil
		}
		quiet, _ := cmd.Flags().GetBool("quiet")
		if !quiet {
			r := output.Receipt{OK: true, DryRun: true, Message: "Would create Jira issue"}
			fmt.Println(r.Format())
		}
		return nil
	}

	if idemKey != "" {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "jira", "create",
					map[string]any{},
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	result, err := client.CreateIssue(payload)
	if err != nil {
		return err
	}

	var warnings []any
	if idemKey != "" {
		if recErr := idempotency.Record(idemKey, "jira.create"); recErr != nil {
			warnMsg := fmt.Sprintf("Issue was created, but the idempotency key could not be saved: %v", recErr)
			warnings = append(warnings, warnMsg)
			if mode != "json" && mode != "summary" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", warnMsg)
			}
		}
	}

	key, _ := result["key"].(string)
	issueID, _ := result["id"].(string)
	receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Created issue %s", stringOr(key, issueID))}

	if mode == "json" {
		var issueURL any
		if key != "" {
			issueURL = fmt.Sprintf("%s/browse/%s", client.BaseURL(), key)
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "create",
			map[string]any{},
			map[string]any{
				"key":         key,
				"id":          issueID,
				"url":         issueURL,
				"file":        file,
				"receipt":     receipt.Format(),
				"idempotency": map[string]any{"key": output.IdempotencyKey("jira.create", payload)},
			},
			warnings, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		summaryPart := ""
		if summaryText != "" {
			summaryPart = fmt.Sprintf(": %s", summaryText)
		}
		fmt.Printf("Created %s%s.\n", stringOr(key, issueID), summaryPart)
		return nil
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}
