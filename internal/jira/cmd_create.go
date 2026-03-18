package jira

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCreateCmd creates the "create" subcommand.
func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create [file]",
		Short: "Create issue from JSON payload",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runCreate,
	}
	cmd.Flags().Bool("stdin", false, "Read the create payload from stdin")
	cmd.Flags().String("inline", "", "Inline JSON payload for issue creation")
	cmd.Flags().String("template", "", "JSON template file with ${VAR} placeholders")
	cmd.Flags().StringArray("var", nil, "Template variable override: KEY=VALUE")
	cmd.Flags().String("clone", "", "Clone an existing issue into a new create payload")
	cmd.Flags().String("clone-mode", "portable", "Clone mode: portable or full")
	cmd.Flags().StringArray("include-field", nil, "Include additional field(s) when using --clone")
	cmd.Flags().StringArray("exclude-field", nil, "Exclude field(s) when using --clone")
	cmd.Flags().String("project", "", "Project key override")
	cmd.Flags().String("type", "", "Issue type name override")
	cmd.Flags().String("issue-type", "", "Alias for --type")
	cmd.Flags().String("summary", "", "Issue summary override")
	cmd.Flags().String("description", "", "Issue description override")
	cmd.Flags().String("description-file", "", "Read description override from a text file")
	cmd.Flags().String("priority", "", "Priority override")
	cmd.Flags().String("parent", "", "Parent issue key override for sub-tasks")
	cmd.Flags().String("assignee", "", "Assignee override: accountId, accountId:xxx, name:xxx, or null")
	cmd.Flags().StringArray("component", nil, "Component override by name (repeatable)")
	cmd.Flags().StringArray("label", nil, "Label override (repeatable)")
	cmd.Flags().StringArray("set", nil, "Shorthand field override (repeatable): field=value, field:=<json>, labels+=x, labels-=x")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().String("emit", "", "Emit a scalar result instead of the normal output: key, id, url, receipt")
	cmd.Flags().Bool("dry-run", false, "Preview without creating")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

type createStoredResult struct {
	Key     string `json:"key,omitempty"`
	ID      string `json:"id,omitempty"`
	URL     string `json:"url,omitempty"`
	Receipt string `json:"receipt,omitempty"`
}

func runCreate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	if mode == "key" && !cli.SupportsKeyOutput(cmd) {
		return cli.KeyModeUnsupportedError(cmd)
	}
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	file := ""
	if len(args) > 0 {
		file = args[0]
	}
	useStdin, _ := cmd.Flags().GetBool("stdin")
	inlineJSON, _ := cmd.Flags().GetString("inline")
	templateFile, _ := cmd.Flags().GetString("template")
	templateVars, _ := cmd.Flags().GetStringArray("var")
	cloneIssue, _ := cmd.Flags().GetString("clone")
	cloneMode, _ := cmd.Flags().GetString("clone-mode")
	includeFields, _ := cmd.Flags().GetStringArray("include-field")
	excludeFields, _ := cmd.Flags().GetStringArray("exclude-field")
	projectFlag, _ := cmd.Flags().GetString("project")
	typeFlag, _ := cmd.Flags().GetString("type")
	typeAliasFlag, _ := cmd.Flags().GetString("issue-type")
	summaryFlag, _ := cmd.Flags().GetString("summary")
	descriptionFlag, _ := cmd.Flags().GetString("description")
	descriptionFile, _ := cmd.Flags().GetString("description-file")
	priorityFlag, _ := cmd.Flags().GetString("priority")
	parentFlag, _ := cmd.Flags().GetString("parent")
	assigneeFlag, _ := cmd.Flags().GetString("assignee")
	componentsFlag, _ := cmd.Flags().GetStringArray("component")
	labelsFlag, _ := cmd.Flags().GetStringArray("label")
	setExprs, _ := cmd.Flags().GetStringArray("set")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	emitFlag, _ := cmd.Flags().GetString("emit")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	typeName := strings.TrimSpace(typeFlag)
	if typeName == "" {
		typeName = strings.TrimSpace(typeAliasFlag)
	}

	resolution, err := resolveCreatePayload(client, createInput{
		File:            file,
		UseStdin:        useStdin,
		InlineJSON:      inlineJSON,
		TemplateFile:    templateFile,
		TemplateVars:    templateVars,
		CloneIssue:      cloneIssue,
		CloneMode:       cloneMode,
		IncludeFields:   includeFields,
		ExcludeFields:   excludeFields,
		Project:         projectFlag,
		IssueType:       typeName,
		Summary:         summaryFlag,
		Description:     descriptionFlag,
		DescriptionFile: descriptionFile,
		Priority:        priorityFlag,
		Parent:          parentFlag,
		Assignee:        assigneeFlag,
		Components:      componentsFlag,
		Labels:          labelsFlag,
		SetExprs:        setExprs,
	})
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(errorCode(err, cerrors.OpFailed), err.Error(), "", "", nil)
			target := map[string]any{}
			switch {
			case file != "":
				absFile, _ := filepath.Abs(file)
				cwd, _ := os.Getwd()
				target["file"] = file
				target["absolute_file"] = absFile
				target["cwd"] = cwd
			case templateFile != "":
				absTemplate, _ := filepath.Abs(templateFile)
				cwd, _ := os.Getwd()
				target["template"] = templateFile
				target["absolute_template"] = absTemplate
				target["cwd"] = cwd
			case cloneIssue != "":
				target["clone"] = cloneIssue
				target["clone_mode"] = normalizedCloneMode(cloneMode)
			case inlineJSON != "":
				target["inline"] = true
			case useStdin:
				target["stdin"] = true
			default:
				target["source"] = "quick"
			}
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "create",
				target,
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		return err
	}

	return executeResolvedCreate(cmd, mode, client, resolution, noNotify, dryRun, idemKey, emitFlag)
}

func executeResolvedCreate(cmd *cobra.Command, mode string, client *Client, resolution createResolution, noNotify, dryRun bool, idemKey string, emitFlag string) error {
	payload := resolution.Payload
	fingerprint := output.IdempotencyKey("jira.create", payload)
	effectiveIdemKey := idemKey
	if effectiveIdemKey == "" {
		effectiveIdemKey = fingerprint
	}
	quiet, _ := cmd.Flags().GetBool("quiet")

	if dryRun {
		receipt := output.Receipt{OK: true, DryRun: true, Message: "Would create Jira issue"}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "create",
				resolution.SourceTarget,
				map[string]any{
					"dry_run":     true,
					"summary":     resolution.Summary,
					"project":     resolution.Project,
					"payload":     payload,
					"receipt":     receipt.Format(),
					"idempotency": map[string]any{"requested_key": idemKey, "effective_key": effectiveIdemKey, "fingerprint": fingerprint},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if emitFlag != "" || mode == "key" {
			if emitFlag == "receipt" {
				fmt.Println(receipt.Format())
			} else if !quiet {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Dry-run: no create result is available to emit.")
			}
			return nil
		}
		if mode == "summary" {
			detail := ""
			if resolution.Project != "" {
				detail = fmt.Sprintf(" (project %s)", resolution.Project)
			}
			summaryPart := ""
			if resolution.Summary != "" {
				summaryPart = fmt.Sprintf(": %s", resolution.Summary)
			}
			fmt.Printf("Would create Jira issue%s%s.\n", detail, summaryPart)
			return nil
		}
		if !quiet {
			fmt.Println(receipt.Format())
		}
		return nil
	}

	if idemKey != "" {
		var stored createStoredResult
		found, loadErr := idempotency.LoadValue(idemKey, &stored)
		if loadErr != nil {
			return loadErr
		}
		if found {
			if stored.Key == "" && stored.ID == "" && mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "jira", "create",
					resolution.SourceTarget,
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used", "idempotency": map[string]any{"requested_key": idemKey, "effective_key": effectiveIdemKey, "fingerprint": fingerprint}},
					nil, nil, "", "", "", nil,
				))
			}
			if stored.Key != "" || stored.ID != "" {
				return emitCreateResult(cmd, mode, emitFlag, resolution.SourceTarget, stored, true, idemKey, fingerprint)
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	result, err := client.CreateIssue(payload, !noNotify)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(errorCode(err, cerrors.CreateFailed), err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "create",
				resolution.SourceTarget,
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		return err
	}

	var warnings []any
	key, _ := result["key"].(string)
	issueID, _ := result["id"].(string)
	issueURL := ""
	if key != "" {
		issueURL = fmt.Sprintf("%s/browse/%s", client.BaseURL(), key)
	}
	receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Created issue %s", stringOr(key, issueID))}
	stored := createStoredResult{
		Key:     key,
		ID:      issueID,
		URL:     issueURL,
		Receipt: receipt.Format(),
	}

	if idemKey != "" {
		if recErr := idempotency.RecordKindValue(idemKey, "result", "jira.create", stored); recErr != nil {
			warnMsg := fmt.Sprintf("Issue was created, but the idempotency key could not be saved: %v", recErr)
			warnings = append(warnings, warnMsg)
			if mode != "json" && mode != "summary" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "Warning:", warnMsg)
			}
		}
	}
	return emitCreateResult(cmd, mode, emitFlag, resolution.SourceTarget, stored, false, idemKey, fingerprint, warnings)
}

func emitCreateResult(cmd *cobra.Command, mode string, emit string, target map[string]any, result createStoredResult, skipped bool, requestedKey string, fingerprint string, warnings ...[]any) error {
	mergedWarnings := []any{}
	for _, group := range warnings {
		mergedWarnings = append(mergedWarnings, group...)
	}

	if mode == "key" || emit != "" {
		value, ok := selectCreateEmission(mode, emit, result)
		if !ok {
			if mode == "key" || emit == "key" || emit == "id" || emit == "url" {
				_, _ = fmt.Fprintln(cmd.ErrOrStderr(), "No create result is available to emit.")
				return nil
			}
		}
		if value != "" {
			fmt.Println(value)
		}
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "create",
			target,
			map[string]any{
				"key":         result.Key,
				"id":          result.ID,
				"url":         result.URL,
				"receipt":     result.Receipt,
				"skipped":     skipped,
				"idempotency": map[string]any{"requested_key": requestedKey, "effective_key": firstNonEmpty(requestedKey, fingerprint), "fingerprint": fingerprint},
			},
			mergedWarnings, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Created %s.\n", stringOr(result.Key, result.ID))
		return nil
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(result.Receipt)
	}
	return nil
}

func selectCreateEmission(mode string, emit string, result createStoredResult) (string, bool) {
	selection := strings.TrimSpace(emit)
	if mode == "key" {
		selection = "key"
	}
	switch selection {
	case "key":
		return result.Key, result.Key != ""
	case "id":
		return result.ID, result.ID != ""
	case "url":
		return result.URL, result.URL != ""
	case "receipt":
		return result.Receipt, result.Receipt != ""
	default:
		return "", false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func errorCode(err error, fallback string) string {
	if err == nil {
		return fallback
	}
	if ce, ok := err.(*cerrors.CojiraError); ok && ce.Code != "" {
		return ce.Code
	}
	return fallback
}
