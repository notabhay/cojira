package jira

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/notabhay/cojira/internal/undo"
	"github.com/spf13/cobra"
)

// NewUpdateCmd creates the "update" subcommand.
func NewUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <issue> [file]",
		Short: "Update issue fields",
		Long:  "Update an issue using a JSON payload or quick flags for summary/description.",
		Args:  cobra.RangeArgs(1, 2),
		RunE:  runUpdate,
	}
	cmd.Flags().String("summary", "", "Quick summary update")
	cmd.Flags().String("description", "", "Quick description update")
	cmd.Flags().String("description-file", "", "Read description from a text file")
	cmd.Flags().String("description-format", "raw", "Description format: raw, markdown, or adf")
	cmd.Flags().StringArray("set", nil, "Shorthand field update (repeatable): field=value, field:=<json>, labels+=x, labels-=x")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("diff", false, "Show field diffs and exit without updating")
	cmd.Flags().Bool("preview", false, "Alias for --diff")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runUpdate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	issueID := ResolveIssueIdentifier(args[0])
	var file string
	if len(args) > 1 {
		file = args[1]
	}

	summaryFlag, _ := cmd.Flags().GetString("summary")
	descFlag, _ := cmd.Flags().GetString("description")
	descFile, _ := cmd.Flags().GetString("description-file")
	descFormat, _ := cmd.Flags().GetString("description-format")
	setExprs, _ := cmd.Flags().GetStringArray("set")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	diffFlag, _ := cmd.Flags().GetBool("diff")
	previewFlag, _ := cmd.Flags().GetBool("preview")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")

	payload := map[string]any{}
	if file != "" {
		payload, err = readJSONFile(file)
		if err != nil {
			return err
		}
	}

	if descFlag != "" && descFile != "" {
		msg := "Use either --description or --description-file, not both."
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, msg, "", "", nil)
			ec := 2
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "update",
				map[string]any{"issue": issueID},
				nil, nil, []any{errObj}, "", "", "", &ec,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\n", msg)
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: msg, ExitCode: 2}
	}

	fields := map[string]any{}
	if existing, ok := payload["fields"].(map[string]any); ok {
		for k, v := range existing {
			fields[k] = v
		}
	}
	if summaryFlag != "" {
		fields["summary"] = summaryFlag
	}
	if descFile != "" {
		content, err := readTextFile(descFile)
		if err != nil {
			return err
		}
		fields["description"] = content
	}
	if descFlag != "" {
		fields["description"] = descFlag
	}

	// Parse --set expressions.
	type setOp struct {
		field, op, value string
	}
	var setOps []setOp
	for _, expr := range setExprs {
		f, o, v, err := ParseSetExpr(expr)
		if err != nil {
			return err
		}
		setOps = append(setOps, setOp{f, o, v})
	}

	// Determine referenced fields for pre-fetch.
	refFields := map[string]bool{}
	for k := range fields {
		refFields[k] = true
	}
	for _, s := range setOps {
		refFields[s.field] = true
	}
	if mode == "json" {
		refFields["updated"] = true
	}

	if len(refFields) == 0 {
		msg := "No update payload provided."
		hint := "Provide a payload file or use --summary/--description/--set."
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, msg, hint, "", nil)
			ec := 2
			return output.PrintJSON(output.BuildEnvelope(
				false, "jira", "update",
				map[string]any{"issue": issueID},
				nil, nil, []any{errObj}, "", "", "", &ec,
			))
		}
		_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Error: %s\nHint: %s\n", msg, hint)
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: msg, ExitCode: 2}
	}

	// Pre-fetch current issue.
	fieldList := make([]string, 0, len(refFields))
	for k := range refFields {
		fieldList = append(fieldList, k)
	}
	sort.Strings(fieldList)
	issueCurrent, err := client.GetIssue(issueID, strings.Join(fieldList, ","), "")
	if err != nil {
		return err
	}
	currentFields, _ := issueCurrent["fields"].(map[string]any)
	if currentFields == nil {
		currentFields = map[string]any{}
	}

	// Apply --set expressions.
	for _, s := range setOps {
		if err := applySetOp(s.field, s.op, s.value, fields, mergedFieldState(currentFields, fields)); err != nil {
			return err
		}
	}
	if err := normalizeJiraDescriptionField(fields, descFormat, jiraUsesADF()); err != nil {
		return err
	}

	payload["fields"] = fields

	diffs := previewPayloadDiff(issueID, issueCurrent, payload, quiet || mode == "json")
	diffFieldNames := make([]string, 0, len(diffs))
	for _, d := range diffs {
		diffFieldNames = append(diffFieldNames, d.Field)
	}

	if diffFlag || previewFlag {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "update",
				map[string]any{"issue": issueID},
				map[string]any{
					"preview":     true,
					"diffs":       diffs,
					"summary":     map[string]any{"field_count": len(diffs)},
					"idempotency": map[string]any{"key": output.IdempotencyKey("jira.update", issueID, payload)},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fieldsList := formatFieldList(diffFieldNames, 6)
			detail := ""
			if fieldsList != "" {
				detail = fmt.Sprintf(" (%s)", fieldsList)
			}
			fmt.Printf("Previewed update for %s%s.\n", issueID, detail)
			return nil
		}
		if !quiet {
			r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would update %s (%d field(s))", issueID, len(diffs))}
			fmt.Println(r.Format())
		}
		return nil
	}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "update",
				map[string]any{"issue": issueID},
				map[string]any{
					"dry_run":     true,
					"diffs":       diffs,
					"summary":     map[string]any{"field_count": len(diffs)},
					"idempotency": map[string]any{"key": output.IdempotencyKey("jira.update", issueID, payload)},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fieldsList := formatFieldList(diffFieldNames, 6)
			detail := ""
			if fieldsList != "" {
				detail = fmt.Sprintf(" (%s)", fieldsList)
			}
			fmt.Printf("Would update %s%s.\n", issueID, detail)
			return nil
		}
		if !quiet {
			r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would update %s (%d field(s))", issueID, len(diffs))}
			fmt.Println(r.Format())
		}
		return nil
	}

	if len(diffs) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "update",
				map[string]any{"issue": issueID},
				map[string]any{
					"updated":     false,
					"diffs":       []any{},
					"idempotency": map[string]any{"key": output.IdempotencyKey("jira.update", issueID, payload)},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("No changes for %s.\n", issueID)
			return nil
		}
		if !quiet {
			r := output.Receipt{OK: true, Message: fmt.Sprintf("No changes for %s", issueID)}
			fmt.Println(r.Format())
		}
		return nil
	}

	if idemKey != "" {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "jira", "update",
					map[string]any{"issue": issueID},
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			_, _ = fmt.Fprintf(cmd.ErrOrStderr(), "Skipped (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	if err := client.UpdateIssue(issueID, payload, !noNotify); err != nil {
		return err
	}
	recordUndoEntry(undo.NewGroupID("jira.update"), issueID, "jira.update", snapshotFieldValues(currentFields, diffFieldNames), "", "")

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.update %s", issueID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "update",
			map[string]any{"issue": issueID},
			map[string]any{
				"updated":     true,
				"diffs":       diffs,
				"url":         fmt.Sprintf("%s/browse/%s", client.BaseURL(), issueID),
				"idempotency": map[string]any{"key": output.IdempotencyKey("jira.update", issueID, payload)},
			},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fieldsList := formatFieldList(diffFieldNames, 6)
		detail := ""
		if fieldsList != "" {
			detail = fmt.Sprintf(" (%s)", fieldsList)
		}
		fmt.Printf("Updated %s%s.\n", issueID, detail)
		return nil
	}
	if !quiet {
		fieldsChanged := make([]string, 0, len(diffs))
		for _, d := range diffs {
			if len(fieldsChanged) < 6 {
				fieldsChanged = append(fieldsChanged, d.Field)
			}
		}
		more := ""
		if len(diffs) > 6 {
			more = fmt.Sprintf(" (+%d more)", len(diffs)-6)
		}
		changes := make([]output.Change, len(diffs))
		for i, d := range diffs {
			changes[i] = output.Change{
				Field:    d.Field,
				OldValue: fmt.Sprintf("%v", d.OldValue),
				NewValue: fmt.Sprintf("%v", d.NewValue),
			}
		}
		r := output.Receipt{
			OK:      true,
			Message: fmt.Sprintf("Updated %s: %s%s", issueID, strings.Join(fieldsChanged, ", "), more),
			Changes: changes,
		}
		fmt.Println(r.Format())
	}
	return nil
}

func mergedFieldState(baseFields, pendingFields map[string]any) map[string]any {
	merged := map[string]any{}
	for k, v := range baseFields {
		merged[k] = v
	}
	for k, v := range pendingFields {
		merged[k] = v
	}
	return merged
}

// applySetOp applies a single --set operation to the fields map using currentFields for context.
func applySetOp(field, op, value string, fields, currentFields map[string]any) error {
	if op == OpJSONSet {
		var parsed any
		if err := json.Unmarshal([]byte(value), &parsed); err != nil {
			return &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  fmt.Sprintf("Invalid JSON for --set %s:=: %v", field, err),
				ExitCode: 1,
			}
		}
		fields[field] = parsed
		return nil
	}

	if field == "priority" && op == OpSet {
		fields[field] = keyedObjectValue(value, "name")
		return nil
	}

	if (field == "assignee" || field == "reporter") && op == OpSet {
		fields[field] = coerceUserFieldValue(value)
		return nil
	}

	if field == "labels" && (op == OpListAppend || op == OpListRemove) {
		cur := currentFields["labels"]
		var strs []string
		switch typed := cur.(type) {
		case nil:
		case []any:
			strs = make([]string, 0, len(typed))
			for _, x := range typed {
				strs = append(strs, fmt.Sprintf("%v", x))
			}
		case []string:
			strs = append(strs, typed...)
		default:
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "labels is not a list on this issue; cannot apply += or -=.",
				ExitCode: 1,
			}
		}
		result, err := MergeListOfStrings(strs, op, value)
		if err != nil {
			return err
		}
		fields["labels"] = result
		return nil
	}

	if field == "labels" && op == OpSet {
		fields[field] = splitCSVStrings(value)
		return nil
	}

	if (field == "components" || field == "versions" || field == "fixVersions") && (op == OpListAppend || op == OpListRemove) {
		cur := currentFields[field]
		var curDicts []map[string]any
		switch typed := cur.(type) {
		case nil:
		case []any:
			for _, x := range typed {
				if m, ok := x.(map[string]any); ok {
					curDicts = append(curDicts, m)
				}
			}
		case []map[string]any:
			curDicts = append(curDicts, typed...)
		default:
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("%s is not a list on this issue; cannot apply += or -=.", field),
				ExitCode: 1,
			}
		}
		result, err := MergeListByName(curDicts, op, value)
		if err != nil {
			return err
		}
		fields[field] = result
		return nil
	}

	if (field == "components" || field == "versions" || field == "fixVersions") && op == OpSet {
		fields[field] = splitCSVNamedObjects(value)
		return nil
	}

	if (field == "issuetype" || field == "resolution") && op == OpSet {
		fields[field] = keyedObjectValue(value, "name")
		return nil
	}

	if field == "project" && op == OpSet {
		fields[field] = keyedProjectValue(value)
		return nil
	}

	if field == "parent" && op == OpSet {
		fields[field] = keyedParentValue(value)
		return nil
	}

	if op == OpListAppend || op == OpListRemove {
		cur := currentFields[field]
		var strs []string
		switch typed := cur.(type) {
		case nil:
		case []any:
			strs = make([]string, 0, len(typed))
			for _, x := range typed {
				strs = append(strs, fmt.Sprintf("%v", x))
			}
		case []string:
			strs = append(strs, typed...)
		default:
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("%s is not a list on this issue; cannot apply += or -=.", field),
				ExitCode: 1,
			}
		}
		result, err := MergeListOfStrings(strs, op, value)
		if err != nil {
			return err
		}
		fields[field] = result
		return nil
	}

	// Default '=' assignment.
	fields[field] = value
	return nil
}

func keyedObjectValue(value string, defaultKey string) map[string]any {
	trimmed := strings.TrimSpace(value)
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) == 2 {
		prefix := strings.ToLower(strings.TrimSpace(parts[0]))
		raw := strings.TrimSpace(parts[1])
		switch prefix {
		case "id", "name", "key":
			return map[string]any{prefix: raw}
		}
	}
	return map[string]any{defaultKey: trimmed}
}

func coerceUserFieldValue(value string) any {
	trimmed := strings.TrimSpace(value)
	lower := strings.ToLower(trimmed)
	if lower == "null" || lower == "none" || trimmed == "" {
		return nil
	}

	if key, raw := splitTypedUserRef(trimmed); key != "" {
		return map[string]any{key: raw}
	}
	if strings.Contains(trimmed, "@") {
		return map[string]any{"emailAddress": trimmed}
	}
	return map[string]any{"name": trimmed}
}

func keyedProjectValue(value string) map[string]any {
	trimmed := strings.TrimSpace(value)
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) == 2 {
		prefix := strings.ToLower(strings.TrimSpace(parts[0]))
		raw := strings.TrimSpace(parts[1])
		if prefix == "id" || prefix == "key" {
			return map[string]any{prefix: raw}
		}
	}
	if isDigits(trimmed) {
		return map[string]any{"id": trimmed}
	}
	return map[string]any{"key": trimmed}
}

func keyedParentValue(value string) map[string]any {
	trimmed := strings.TrimSpace(value)
	parts := strings.SplitN(trimmed, ":", 2)
	if len(parts) == 2 {
		prefix := strings.ToLower(strings.TrimSpace(parts[0]))
		raw := strings.TrimSpace(parts[1])
		if prefix == "id" || prefix == "key" {
			return map[string]any{prefix: raw}
		}
	}
	if isDigits(trimmed) {
		return map[string]any{"id": trimmed}
	}
	return map[string]any{"key": trimmed}
}

func isDigits(value string) bool {
	if value == "" {
		return false
	}
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func splitCSVStrings(value string) []string {
	parts := strings.Split(value, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

func splitCSVNamedObjects(value string) []map[string]any {
	parts := splitCSVStrings(value)
	out := make([]map[string]any, 0, len(parts))
	for _, part := range parts {
		out = append(out, map[string]any{"name": part})
	}
	return out
}
