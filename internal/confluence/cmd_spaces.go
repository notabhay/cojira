package confluence

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewSpacesCmd creates the "spaces" subcommand.
func NewSpacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spaces [query]",
		Short: "List visible Confluence spaces",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSpaces,
	}
	cmd.Flags().Bool("all", false, "Fetch all spaces")
	cmd.Flags().Int("limit", 25, "Maximum spaces to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runSpaces(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")

	items := make([]map[string]any, 0)
	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.ListSpaces(pageSize, offset)
			if err != nil {
				return err
			}
			pageItems := extractResults(data)
			items = append(items, pageItems...)
			if len(pageItems) < pageSize {
				break
			}
			offset += len(pageItems)
		}
	} else {
		data, err := client.ListSpaces(limit, start)
		if err != nil {
			return err
		}
		items = extractResults(data)
	}

	if query != "" {
		needle := strings.ToLower(query)
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			key := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", item["key"])))
			name := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", item["name"])))
			if strings.Contains(key, needle) || strings.Contains(name, needle) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	target := map[string]any{}
	if query != "" {
		target["query"] = query
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "spaces",
			target,
			map[string]any{"spaces": items, "summary": map[string]any{"count": len(items)}},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		if query != "" {
			fmt.Printf("Found %d Confluence space(s) matching %q.\n", len(items), query)
		} else {
			fmt.Printf("Found %d Confluence space(s).\n", len(items))
		}
		return nil
	}

	if len(items) == 0 {
		fmt.Println("No spaces found.")
		return nil
	}

	fmt.Printf("Spaces (%d):\n\n", len(items))
	for _, item := range items {
		fmt.Printf("  %-12v %-18v %v\n", item["key"], item["type"], item["name"])
	}
	return nil
}

func extractResults(data map[string]any) []map[string]any {
	raw, _ := data["results"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}
