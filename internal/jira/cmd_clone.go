package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCloneCmd creates the "clone" subcommand.
func NewCloneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone <issue>",
		Short: "Clone a Jira issue into a create payload and optionally create it",
		Args:  cobra.ExactArgs(1),
		RunE:  runClone,
	}
	cmd.Flags().String("project", "", "Target project key (defaults to the source issue project)")
	cmd.Flags().String("summary", "", "Override summary for the cloned issue")
	cmd.Flags().String("summary-prefix", "", "Prefix to prepend to the source summary when --summary is not set")
	cmd.Flags().String("type", "", "Override issue type")
	cmd.Flags().String("priority", "", "Override priority")
	cmd.Flags().String("description", "", "Override description")
	cmd.Flags().String("description-file", "", "Read description override from a text file")
	cmd.Flags().String("assignee", "", "Override assignee")
	cmd.Flags().String("reporter", "", "Override reporter")
	cmd.Flags().String("parent", "", "Override parent issue")
	cmd.Flags().Bool("keep-assignee", false, "Copy the source assignee when present")
	cmd.Flags().Bool("keep-reporter", false, "Copy the source reporter when present")
	cmd.Flags().Bool("keep-parent", false, "Copy the source parent when cloning into the same project")
	cmd.Flags().StringSlice("labels", nil, "Override labels")
	cmd.Flags().StringSlice("components", nil, "Override components")
	cmd.Flags().StringSlice("versions", nil, "Override affects versions")
	cmd.Flags().StringSlice("fix-versions", nil, "Override fix versions")
	cmd.Flags().StringArray("set", nil, "Additional field overrides (repeatable): field=value, field:=<json>, labels+=x, labels-=x")
	cmd.Flags().Bool("dry-run", false, "Preview without creating")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runClone(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	sourceIssueID := ResolveIssueIdentifier(args[0])
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	quiet, _ := cmd.Flags().GetBool("quiet")

	sourceIssue, err := client.GetIssue(sourceIssueID, "summary,description,issuetype,priority,labels,components,versions,fixVersions,project,parent,assignee,reporter", "")
	if err != nil {
		return err
	}

	payload, summaryText, projectKey, err := buildClonePayload(cmd, sourceIssue)
	if err != nil {
		return err
	}

	target := map[string]any{"issue": sourceIssueID}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "clone",
				target,
				map[string]any{
					"dry_run":     true,
					"summary":     summaryText,
					"project":     projectKey,
					"payload":     payload,
					"idempotency": map[string]any{"key": output.IdempotencyKey("jira.clone", sourceIssueID, payload)},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would clone %s into %s: %s.\n", sourceIssueID, projectKey, summaryText)
			return nil
		}
		if !quiet {
			r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would clone %s into %s", sourceIssueID, projectKey)}
			fmt.Println(r.Format())
		}
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "clone",
				target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Printf("Skipped duplicate clone for %s.\n", sourceIssueID)
		return nil
	}

	result, err := client.CreateIssue(payload)
	if err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.clone %s", sourceIssueID))
	}

	key := normalizeMaybeString(result["key"])
	issueID := normalizeMaybeString(result["id"])
	receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Cloned %s to %s", sourceIssueID, stringOr(key, issueID))}

	if mode == "json" {
		var issueURL any
		if key != "" {
			issueURL = fmt.Sprintf("%s/browse/%s", client.BaseURL(), key)
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "clone",
			target,
			map[string]any{
				"source":      sourceIssueID,
				"key":         key,
				"id":          issueID,
				"url":         issueURL,
				"receipt":     receipt.Format(),
				"idempotency": map[string]any{"key": output.IdempotencyKey("jira.clone", sourceIssueID, payload)},
			},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Cloned %s to %s.\n", sourceIssueID, stringOr(key, issueID))
		return nil
	}
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}

func buildClonePayload(cmd *cobra.Command, sourceIssue map[string]any) (map[string]any, string, string, error) {
	fields, _ := sourceIssue["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}

	payloadFields := map[string]any{
		"summary":     normalizeMaybeString(fields["summary"]),
		"description": fields["description"],
		"issuetype":   cloneNamedObject(fields["issuetype"]),
		"priority":    cloneNamedObject(fields["priority"]),
		"labels":      safeStringSlice(fields, "labels"),
		"components":  cloneNamedObjects(fields["components"]),
		"versions":    cloneNamedObjects(fields["versions"]),
		"fixVersions": cloneNamedObjects(fields["fixVersions"]),
		"project":     cloneProjectField(fields["project"]),
	}

	summaryOverride, _ := cmd.Flags().GetString("summary")
	summaryPrefix, _ := cmd.Flags().GetString("summary-prefix")
	if strings.TrimSpace(summaryOverride) != "" {
		payloadFields["summary"] = summaryOverride
	} else if strings.TrimSpace(summaryPrefix) != "" {
		payloadFields["summary"] = summaryPrefix + normalizeMaybeString(fields["summary"])
	}

	keepParent, _ := cmd.Flags().GetBool("keep-parent")
	if keepParent {
		if parent := cloneParentField(fields["parent"]); parent != nil {
			payloadFields["parent"] = parent
		}
	}

	keepAssignee, _ := cmd.Flags().GetBool("keep-assignee")
	if keepAssignee {
		if assignee := cloneUserField(fields["assignee"]); assignee != nil {
			payloadFields["assignee"] = assignee
		}
	}

	keepReporter, _ := cmd.Flags().GetBool("keep-reporter")
	if keepReporter {
		if reporter := cloneUserField(fields["reporter"]); reporter != nil {
			payloadFields["reporter"] = reporter
		}
	}

	if _, err := applyCreateFlags(cmd, payloadFields); err != nil {
		return nil, "", "", err
	}

	projectKey := safeString(payloadFields, "project", "key")
	sourceProjectKey := safeString(fields, "project", "key")
	if projectKey == "" {
		projectKey = sourceProjectKey
	}

	if keepParent && projectKey != "" && sourceProjectKey != "" && projectKey != sourceProjectKey {
		delete(payloadFields, "parent")
	}

	summaryText := normalizeMaybeString(payloadFields["summary"])
	if strings.TrimSpace(summaryText) == "" {
		return nil, "", "", fmt.Errorf("cloned issue summary cannot be empty")
	}

	payload := map[string]any{"fields": payloadFields}
	return payload, summaryText, projectKey, nil
}

func cloneNamedObject(raw any) map[string]any {
	item, _ := raw.(map[string]any)
	if item == nil {
		return nil
	}
	if name := normalizeMaybeString(item["name"]); name != "" {
		return map[string]any{"name": name}
	}
	if id := normalizeMaybeString(item["id"]); id != "" {
		return map[string]any{"id": id}
	}
	return nil
}

func cloneNamedObjects(raw any) []map[string]any {
	items, ok := raw.([]any)
	if !ok {
		return nil
	}
	out := make([]map[string]any, 0, len(items))
	for _, rawItem := range items {
		if item := cloneNamedObject(rawItem); item != nil {
			out = append(out, item)
		}
	}
	return out
}

func cloneProjectField(raw any) map[string]any {
	item, _ := raw.(map[string]any)
	if item == nil {
		return nil
	}
	if key := normalizeMaybeString(item["key"]); key != "" {
		return map[string]any{"key": key}
	}
	if id := normalizeMaybeString(item["id"]); id != "" {
		return map[string]any{"id": id}
	}
	return nil
}

func cloneParentField(raw any) map[string]any {
	item, _ := raw.(map[string]any)
	if item == nil {
		return nil
	}
	if key := normalizeMaybeString(item["key"]); key != "" {
		return map[string]any{"key": key}
	}
	if id := normalizeMaybeString(item["id"]); id != "" {
		return map[string]any{"id": id}
	}
	return nil
}

func cloneUserField(raw any) map[string]any {
	item, _ := raw.(map[string]any)
	if item == nil {
		return nil
	}
	for _, key := range []string{"accountId", "name", "key", "emailAddress"} {
		if value := normalizeMaybeString(item[key]); value != "" {
			return map[string]any{key: value}
		}
	}
	return nil
}
