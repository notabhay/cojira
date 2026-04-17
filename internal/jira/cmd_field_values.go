package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewFieldValuesCmd creates the "field-values" subcommand.
func NewFieldValuesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "field-values <issue> <field>",
		Short: "List allowed values for a Jira field on an issue edit screen",
		Args:  cobra.ExactArgs(2),
		RunE:  runFieldValues,
	}
	cmd.Flags().String("format", "detailed", "Output format: detailed or enum")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runFieldValues(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	issueID := ResolveIssueIdentifier(args[0])
	fieldName := strings.TrimSpace(args[1])
	format, _ := cmd.Flags().GetString("format")
	fieldID, entry, source, err := resolveFieldValueMetadata(client, issueID, fieldName)
	if err != nil {
		return err
	}

	enumValues := coerceAllowedValueEnums(entry["allowedValues"])
	if strings.EqualFold(strings.TrimSpace(format), "enum") {
		if mode == "json" || mode == "ndjson" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "field-values", map[string]any{"issue": issueID, "field": fieldName, "format": "enum"}, enumValues, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Field %s has %d allowed value(s) for %s.\n", fieldID, len(enumValues), issueID)
			return nil
		}
		for _, item := range enumValues {
			fmt.Println(item)
		}
		return nil
	}

	result := map[string]any{
		"field_id":      fieldID,
		"allowedValues": coerceAllowedValues(entry["allowedValues"]),
		"schema":        entry["schema"],
		"name":          normalizeMaybeString(entry["name"]),
		"required":      entry["required"],
		"source":        source,
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "field-values", map[string]any{"issue": issueID, "field": fieldName}, result, nil, nil, "", "", "", nil))
	}
	allowed, _ := result["allowedValues"].([]map[string]any)
	if len(allowed) == 0 {
		if mode == "summary" {
			fmt.Printf("Field %s has no enumerated allowed values for %s.\n", fieldID, issueID)
			return nil
		}
		fmt.Printf("Field %s has no enumerated allowed values for %s.\n", fieldID, issueID)
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Field %s has %d allowed value(s) for %s.\n", fieldID, len(allowed), issueID)
		return nil
	}
	fmt.Printf("Allowed values for %s on %s:\n\n", fieldID, issueID)
	rows := make([][]string, 0, len(allowed))
	for _, item := range allowed {
		label := normalizeMaybeString(item["label"])
		value := normalizeMaybeString(item["value"])
		if value == "" {
			value = normalizeMaybeString(item["id"])
		}
		rows = append(rows, []string{
			output.Truncate(value, 24),
			output.Truncate(label, 48),
			normalizeMaybeString(item["id"]),
		})
	}
	fmt.Println(output.TableString([]string{"VALUE", "LABEL", "ID"}, rows))
	return nil
}

func resolveFieldValueMetadata(client *Client, issueID, fieldName string) (string, map[string]any, string, error) {
	editMeta, err := client.GetEditMeta(issueID)
	if err != nil {
		return "", nil, "", err
	}
	fields, _ := editMeta["fields"].(map[string]any)
	if len(fields) > 0 {
		if fieldID, entry := findEditMetaField(fields, fieldName); fieldID != "" {
			return fieldID, entry, "editmeta", nil
		}
	}

	issue, err := client.GetIssue(issueID, "project,issuetype", "")
	if err != nil {
		return "", nil, "", err
	}
	projectKey, issueTypeID := issueProjectAndType(issue)
	if projectKey == "" || issueTypeID == "" {
		return "", nil, "", &cerrors.CojiraError{Code: cerrors.FetchFailed, Message: fmt.Sprintf("No edit metadata was returned for %s, and the issue type could not be resolved for create metadata fallback.", issueID), ExitCode: 1}
	}

	createMetaFields, err := client.GetCreateMetaIssueTypeFields(projectKey, issueTypeID)
	if err != nil {
		return "", nil, "", err
	}
	if len(createMetaFields) == 0 {
		return "", nil, "", &cerrors.CojiraError{Code: cerrors.FetchFailed, Message: fmt.Sprintf("No edit metadata was returned for %s, and create metadata fallback was empty for %s/%s.", issueID, projectKey, issueTypeID), ExitCode: 1}
	}
	if fieldID, entry := findCreateMetaField(createMetaFields, fieldName); fieldID != "" {
		return fieldID, entry, "createmeta", nil
	}
	return "", nil, "", &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Field %s was not found in edit metadata or create metadata for %s.", fieldName, issueID), ExitCode: 1}
}

func findEditMetaField(fields map[string]any, query string) (string, map[string]any) {
	needle := strings.ToLower(strings.TrimSpace(query))
	if entry, ok := fields[query].(map[string]any); ok {
		return query, entry
	}
	for fieldID, raw := range fields {
		entry, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		name := strings.ToLower(normalizeMaybeString(entry["name"]))
		if strings.EqualFold(fieldID, query) || name == needle {
			return fieldID, entry
		}
	}
	return "", nil
}

func findCreateMetaField(fields []map[string]any, query string) (string, map[string]any) {
	needle := strings.ToLower(strings.TrimSpace(query))
	for _, entry := range fields {
		fieldID := normalizeMaybeString(entry["fieldId"])
		name := strings.ToLower(normalizeMaybeString(entry["name"]))
		if strings.EqualFold(fieldID, query) || name == needle {
			return fieldID, entry
		}
	}
	return "", nil
}

func issueProjectAndType(issue map[string]any) (string, string) {
	fields, _ := issue["fields"].(map[string]any)
	if fields == nil {
		return "", ""
	}
	project, _ := fields["project"].(map[string]any)
	issueType, _ := fields["issuetype"].(map[string]any)
	return normalizeMaybeString(project["key"]), normalizeMaybeString(issueType["id"])
}

func coerceAllowedValues(raw any) []map[string]any {
	arr, _ := raw.([]any)
	result := make([]map[string]any, 0, len(arr))
	for _, item := range arr {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		label := normalizeMaybeString(entry["name"])
		if label == "" {
			label = normalizeMaybeString(entry["value"])
		}
		if label == "" {
			label = normalizeMaybeString(entry["displayName"])
		}
		result = append(result, map[string]any{
			"id":    entry["id"],
			"value": stringOr(entry["value"], normalizeMaybeString(entry["key"])),
			"label": label,
		})
	}
	return result
}

func coerceAllowedValueEnums(raw any) []string {
	items := coerceAllowedValues(raw)
	values := make([]string, 0, len(items))
	for _, item := range items {
		value := normalizeMaybeString(item["value"])
		if value == "" {
			value = normalizeMaybeString(item["label"])
		}
		if value == "" {
			continue
		}
		values = append(values, value)
	}
	return values
}
