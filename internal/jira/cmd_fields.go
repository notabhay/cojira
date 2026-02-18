package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewFieldsCmd creates the "fields" subcommand.
func NewFieldsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "fields",
		Short: "List available fields",
		Args:  cobra.NoArgs,
		RunE:  runFields,
	}
	cmd.Flags().String("query", "", "Filter by id or name (substring match)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runFields(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	query, _ := cmd.Flags().GetString("query")

	fields, err := client.ListFields()
	if err != nil {
		return err
	}

	if query != "" {
		needle := strings.ToLower(query)
		var filtered []map[string]any
		for _, f := range fields {
			id, _ := f["id"].(string)
			name, _ := f["name"].(string)
			if strings.Contains(strings.ToLower(id), needle) || strings.Contains(strings.ToLower(name), needle) {
				filtered = append(filtered, f)
			}
		}
		fields = filtered
	}

	if mode == "json" {
		target := map[string]any{}
		if query != "" {
			target["query"] = query
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "fields",
			target, fields, nil, nil, "", "", "", nil,
		))
	}

	if len(fields) == 0 {
		queryStr := ""
		if query != "" {
			queryStr = fmt.Sprintf(" matching '%s'", query)
		}
		if mode == "summary" {
			fmt.Printf("Found 0 fields%s.\n", queryStr)
			return nil
		}
		fmt.Println("No fields found.")
		return nil
	}

	if mode == "summary" {
		queryStr := ""
		if query != "" {
			queryStr = fmt.Sprintf(" matching '%s'", query)
		}
		fmt.Printf("Found %d fields%s.\n", len(fields), queryStr)
		return nil
	}

	fmt.Print("Fields:\n\n")
	for _, field := range fields {
		fieldID, _ := field["id"].(string)
		name, _ := field["name"].(string)
		schema, _ := field["schema"].(map[string]any)
		fieldType := ""
		if schema != nil {
			if t, ok := schema["type"].(string); ok && t != "" {
				fieldType = t
			} else if c, ok := schema["custom"].(string); ok {
				fieldType = c
			}
		}
		custom := "system"
		if isCustom, ok := field["custom"].(bool); ok && isCustom {
			custom = "custom"
		}
		fmt.Printf("  %-22s %-6s %-14s %s\n", fieldID, custom, fieldType, name)
	}
	return nil
}
