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

// NewJQLCmd creates the "jql" command group.
func NewJQLCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "jql",
		Short: "Validate, suggest, and build JQL",
	}
	cmd.AddCommand(
		newJQLValidateCmd(),
		newJQLSuggestCmd(),
		newJQLBuildCmd(),
	)
	return cmd
}

func newJQLValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <jql>",
		Short: "Validate a JQL query with Jira",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			jql := FixJQLShellEscapes(args[0])
			data, err := client.ValidateJQL(jql)
			if err != nil {
				return err
			}
			result := normalizeJQLValidationResult(jql, data)
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "jql.validate", map[string]any{"jql": jql}, result, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				if result["valid"] == true {
					fmt.Println("JQL is valid.")
				} else {
					fmt.Println("JQL is invalid.")
				}
				return nil
			}
			fmt.Printf("Valid: %t\n", result["valid"] == true)
			if errorsList, ok := result["errors"].([]string); ok && len(errorsList) > 0 {
				fmt.Println("Errors:")
				for _, item := range errorsList {
					fmt.Printf("- %s\n", item)
				}
			}
			if warningsList, ok := result["warnings"].([]string); ok && len(warningsList) > 0 {
				fmt.Println("Warnings:")
				for _, item := range warningsList {
					fmt.Printf("- %s\n", item)
				}
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newJQLSuggestCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "suggest [needle]",
		Short: "List JQL fields, functions, and keywords matching a substring",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			needle := ""
			if len(args) > 0 {
				needle = strings.TrimSpace(args[0])
			}
			limit, _ := cmd.Flags().GetInt("limit")
			data, err := client.GetJQLAutoCompleteData()
			if err != nil {
				return err
			}
			result := filterJQLSuggestions(data, needle, limit)
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "jql.suggest", map[string]any{"needle": needle, "limit": limit}, result, nil, nil, "", "", "", nil))
			}
			total := len(result["fields"].([]string)) + len(result["functions"].([]string)) + len(result["keywords"].([]string))
			if mode == "summary" {
				fmt.Printf("Found %d JQL suggestion(s).\n", total)
				return nil
			}
			fmt.Printf("JQL suggestions for %q:\n", needle)
			printSuggestionBlock("Fields", result["fields"].([]string))
			printSuggestionBlock("Functions", result["functions"].([]string))
			printSuggestionBlock("Keywords", result["keywords"].([]string))
			return nil
		},
	}
	cmd.Flags().Int("limit", 20, "Maximum suggestions per category")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newJQLBuildCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "build",
		Short: "Build a JQL query from common flags",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			query, clauses, err := buildJQLFromFlags(cmd)
			if err != nil {
				return err
			}
			result := map[string]any{"jql": query, "clauses": clauses}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "jira", "jql.build", map[string]any{}, result, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Println(query)
				return nil
			}
			fmt.Println(query)
			return nil
		},
	}
	cmd.Flags().StringSlice("project", nil, "Project key filter (repeatable)")
	cmd.Flags().StringSlice("status", nil, "Status filter (repeatable)")
	cmd.Flags().StringSlice("type", nil, "Issue type filter (repeatable)")
	cmd.Flags().StringSlice("label", nil, "Label filter (repeatable)")
	cmd.Flags().String("assignee", "", "Assignee filter")
	cmd.Flags().String("reporter", "", "Reporter filter")
	cmd.Flags().String("text", "", "Full-text search string")
	cmd.Flags().StringArray("clause", nil, "Additional raw JQL clause (repeatable)")
	cmd.Flags().Bool("unresolved", false, "Add resolution = Unresolved")
	cmd.Flags().String("order-by", "updated DESC", "ORDER BY clause (empty to omit)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func normalizeJQLValidationResult(jql string, data map[string]any) map[string]any {
	item := data
	if queries, ok := data["queries"].([]any); ok && len(queries) > 0 {
		if first, ok := queries[0].(map[string]any); ok {
			item = first
		}
	} else if queries, ok := data["queries"].([]map[string]any); ok && len(queries) > 0 {
		item = queries[0]
	}
	errorsList := stringList(item["errors"])
	warningsList := stringList(item["warnings"])
	return map[string]any{
		"jql":      jql,
		"valid":    len(errorsList) == 0,
		"errors":   errorsList,
		"warnings": warningsList,
		"raw":      item,
	}
}

func filterJQLSuggestions(data map[string]any, needle string, limit int) map[string]any {
	return map[string]any{
		"fields":    limitStrings(filterJQLSuggestionValues(data["visibleFieldNames"], needle), limit),
		"functions": limitStrings(filterJQLSuggestionValues(data["visibleFunctionNames"], needle), limit),
		"keywords":  limitStrings(filterJQLSuggestionValues(data["jqlReservedWords"], needle), limit),
	}
}

func filterJQLSuggestionValues(raw any, needle string) []string {
	items, _ := raw.([]any)
	result := make([]string, 0, len(items))
	needle = strings.ToLower(strings.TrimSpace(needle))
	for _, item := range items {
		text := suggestionLabel(item)
		if text == "" {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(text), needle) {
			continue
		}
		result = append(result, text)
	}
	sort.Strings(result)
	return result
}

func suggestionLabel(item any) string {
	switch typed := item.(type) {
	case string:
		return strings.TrimSpace(typed)
	case map[string]any:
		for _, key := range []string{"displayName", "value", "name"} {
			if text := normalizeMaybeString(typed[key]); text != "" {
				return text
			}
		}
	}
	return normalizeMaybeString(item)
}

func limitStrings(items []string, limit int) []string {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func printSuggestionBlock(title string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("\n%s:\n", title)
	for _, item := range items {
		fmt.Printf("- %s\n", item)
	}
}

func stringList(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			item = strings.TrimSpace(item)
			if item != "" {
				out = append(out, item)
			}
		}
		return out
	case []any:
		out := make([]string, 0, len(typed))
		for _, item := range typed {
			text := normalizeMaybeString(item)
			if text == "" {
				continue
			}
			out = append(out, text)
		}
		return out
	}
	return []string{}
}

func buildJQLFromFlags(cmd *cobra.Command) (string, []string, error) {
	clauses := make([]string, 0)
	addListClause := func(field string, values []string) {
		values = normalizeStringSlice(values)
		if len(values) == 0 {
			return
		}
		if len(values) == 1 {
			clauses = append(clauses, fmt.Sprintf("%s = %s", field, quoteJQLValue(values[0])))
			return
		}
		quoted := make([]string, 0, len(values))
		for _, value := range values {
			quoted = append(quoted, quoteJQLValue(value))
		}
		clauses = append(clauses, fmt.Sprintf("%s IN (%s)", field, strings.Join(quoted, ", ")))
	}

	projects, _ := cmd.Flags().GetStringSlice("project")
	statuses, _ := cmd.Flags().GetStringSlice("status")
	types, _ := cmd.Flags().GetStringSlice("type")
	labels, _ := cmd.Flags().GetStringSlice("label")
	assignee, _ := cmd.Flags().GetString("assignee")
	reporter, _ := cmd.Flags().GetString("reporter")
	text, _ := cmd.Flags().GetString("text")
	rawClauses, _ := cmd.Flags().GetStringArray("clause")
	unresolved, _ := cmd.Flags().GetBool("unresolved")
	orderBy, _ := cmd.Flags().GetString("order-by")

	addListClause("project", projects)
	addListClause("status", statuses)
	addListClause("issuetype", types)
	addListClause("labels", labels)
	if strings.TrimSpace(assignee) != "" {
		clauses = append(clauses, fmt.Sprintf("assignee = %s", quoteJQLValue(assignee)))
	}
	if strings.TrimSpace(reporter) != "" {
		clauses = append(clauses, fmt.Sprintf("reporter = %s", quoteJQLValue(reporter)))
	}
	if strings.TrimSpace(text) != "" {
		clauses = append(clauses, fmt.Sprintf("text ~ %s", quoteJQLValue(text)))
	}
	if unresolved {
		clauses = append(clauses, "resolution = Unresolved")
	}
	for _, clause := range rawClauses {
		clause = strings.TrimSpace(clause)
		if clause != "" {
			clauses = append(clauses, clause)
		}
	}
	if len(clauses) == 0 {
		return "", nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "At least one clause is required to build JQL.",
			ExitCode: 2,
		}
	}
	query := strings.Join(clauses, " AND ")
	if strings.TrimSpace(orderBy) != "" {
		query += " ORDER BY " + strings.TrimSpace(orderBy)
	}
	return query, clauses, nil
}

func quoteJQLValue(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return `""`
	}
	if value == "currentUser()" || value == "Unresolved" {
		return value
	}
	if strings.ContainsAny(value, " -:/\"") {
		return `"` + strings.ReplaceAll(value, `"`, `\"`) + `"`
	}
	return value
}
