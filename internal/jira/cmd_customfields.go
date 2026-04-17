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

// NewCustomFieldsCmd creates the "customfields" command group.
func NewCustomFieldsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "customfields",
		Short: "Inspect Jira custom fields by name or id",
	}
	cmd.AddCommand(
		newCustomFieldsMapCmd(),
		newCustomFieldsResolveCmd(),
	)
	return cmd
}

func newCustomFieldsMapCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "map [query]",
		Short: "List Jira custom fields matching a substring",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			query := ""
			if len(args) > 0 {
				query = strings.TrimSpace(args[0])
			}
			fields, err := client.ListFields()
			if err != nil {
				return err
			}
			items := filterCustomFields(fields, query)
			result := map[string]any{"query": query, "fields": items, "count": len(items)}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "customfields.map", map[string]any{"query": query}, result, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found %d custom field(s).\n", len(items))
				return nil
			}
			if len(items) == 0 {
				fmt.Println("No matching custom fields.")
				return nil
			}
			rows := make([][]string, 0, len(items))
			for _, item := range items {
				rows = append(rows, []string{
					output.Truncate(normalizeMaybeString(item["id"]), 22),
					output.Truncate(normalizeMaybeString(item["name"]), 52),
					output.Truncate(normalizeMaybeString(item["type"]), 18),
				})
			}
			fmt.Println(output.TableString([]string{"ID", "NAME", "TYPE"}, rows))
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newCustomFieldsResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <name-or-id>",
		Short: "Resolve a Jira field name to its actual field id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			query := strings.TrimSpace(args[0])
			resolver := newCreateFieldResolver(client, nil)
			resolved, err := resolver.ResolveWithEntry(query)
			if err != nil {
				return err
			}
			if resolved.ID == "" || strings.EqualFold(resolved.ID, query) && resolved.Entry == nil && !isCustomFieldID(query) {
				return &cerrors.CojiraError{
					Code:     cerrors.IdentUnresolved,
					Message:  fmt.Sprintf("Field %s was not found.", query),
					ExitCode: 1,
				}
			}
			result := map[string]any{
				"query": query,
				"id":    resolved.ID,
				"name":  normalizeMaybeString(resolved.Entry["name"]),
				"raw":   resolved.Entry,
			}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "customfields.resolve", map[string]any{"query": query}, result, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("%s resolves to %s.\n", query, resolved.ID)
				return nil
			}
			fmt.Printf("%s -> %s\n", query, resolved.ID)
			if result["name"] != "" {
				fmt.Printf("Name: %s\n", result["name"])
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func filterCustomFields(fields []map[string]any, query string) []map[string]any {
	query = strings.ToLower(strings.TrimSpace(query))
	items := make([]map[string]any, 0)
	for _, field := range fields {
		id := normalizeMaybeString(field["id"])
		if !isCustomFieldID(id) {
			continue
		}
		name := normalizeMaybeString(field["name"])
		if query != "" && !strings.Contains(strings.ToLower(id), query) && !strings.Contains(strings.ToLower(name), query) {
			continue
		}
		schema, _ := field["schema"].(map[string]any)
		items = append(items, map[string]any{
			"id":   id,
			"name": name,
			"type": normalizeMaybeString(schema["type"]),
		})
	}
	sort.Slice(items, func(i, j int) bool {
		return normalizeMaybeString(items[i]["name"]) < normalizeMaybeString(items[j]["name"])
	})
	return items
}
