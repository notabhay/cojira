package jira

import (
	"fmt"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates the "validate" subcommand.
func NewValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Basic sanity check for a Jira JSON payload",
		Args:  cobra.ExactArgs(1),
		RunE:  runValidate,
	}
	cmd.Flags().String("kind", "", "Optional payload kind hint (create, update, batch)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runValidate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	file := args[0]
	kind, _ := cmd.Flags().GetString("kind")

	payload, err := readJSONFile(file)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fields, hasFields := payload["fields"].(map[string]any)
	if kind == "" {
		if _, ok := payload["operations"].([]any); ok {
			kind = "batch"
		} else if hasFields {
			if _, ok := fields["project"].(map[string]any); ok {
				kind = "create"
			} else {
				kind = "update"
			}
		} else {
			kind = "unknown"
		}
	}

	var warnings []any
	if hasFields {
		if warn := validateFieldShapes(fields); len(warn) > 0 {
			for _, item := range warn {
				warnings = append(warnings, item)
			}
		}
	}
	if kind == "batch" {
		ops, ok := payload["operations"].([]any)
		if !ok || len(ops) == 0 {
			return &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  "Batch payload must contain a non-empty operations array.",
				ExitCode: 1,
			}
		}
	}
	if kind == "create" {
		missing := validateRequiredFields(fields, "project", "summary", "issuetype")
		if len(missing) > 0 {
			return &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  fmt.Sprintf("Create payload is missing required fields: %s", strings.Join(missing, ", ")),
				ExitCode: 1,
			}
		}
	}
	if kind == "update" && !hasFields {
		return &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  "Update payload must contain a top-level fields object.",
			ExitCode: 1,
		}
	}

	result := map[string]any{
		"valid":      true,
		"kind":       kind,
		"has_fields": hasFields,
		"keys":       keys,
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "validate",
			map[string]any{"file": file, "kind": kind},
			result, warnings, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		keysStr := "none"
		if len(keys) > 0 {
			keysStr = strings.Join(keys, ", ")
		}
		fmt.Printf("Sanity check passed for Jira payload (%s). Keys: %s\n", kind, keysStr)
		return nil
	}

	fmt.Println("Sanity check passed for Jira payload.")
	fmt.Printf("Kind: %s\n", kind)
	keysStr := "(none)"
	if len(keys) > 0 {
		keysStr = strings.Join(keys, ", ")
	}
	fmt.Printf("Keys: %s\n", keysStr)
	if !hasFields {
		fmt.Println("Warning: No top-level 'fields' object found.")
	}
	for _, warning := range warnings {
		fmt.Printf("Warning: %v\n", warning)
	}
	return nil
}

func validateRequiredFields(fields map[string]any, required ...string) []string {
	var missing []string
	for _, name := range required {
		if _, ok := fields[name]; !ok {
			missing = append(missing, name)
		}
	}
	return missing
}

func validateFieldShapes(fields map[string]any) []string {
	var warnings []string
	objectFields := []string{"priority", "issuetype", "assignee", "reporter", "parent", "resolution", "project"}
	for _, field := range objectFields {
		value, ok := fields[field]
		if !ok {
			continue
		}
		switch value.(type) {
		case map[string]any, nil:
		default:
			warnings = append(warnings, fmt.Sprintf("Field %q is usually an object payload, not %T.", field, value))
		}
	}
	listObjectFields := []string{"components", "versions", "fixVersions"}
	for _, field := range listObjectFields {
		value, ok := fields[field]
		if !ok {
			continue
		}
		items, ok := value.([]any)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("Field %q is usually a list of objects.", field))
			continue
		}
		for _, item := range items {
			if _, ok := item.(map[string]any); !ok {
				warnings = append(warnings, fmt.Sprintf("Field %q should contain objects, not %T.", field, item))
				break
			}
		}
	}
	return warnings
}
