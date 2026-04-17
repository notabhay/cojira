package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDashboardsCmd creates the "dashboards" subcommand.
func NewDashboardsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboards",
		Short: "List Jira dashboards",
		Args:  cobra.NoArgs,
		RunE:  runDashboards,
	}
	cmd.Flags().String("query", "", "Filter by dashboard name, description, or owner")
	cmd.Flags().String("owner", "", "Filter by owner display name or account identifier")
	cmd.Flags().Bool("favorite", false, "Show only favorite dashboards")
	cmd.Flags().Bool("all", false, "Fetch all dashboards")
	cmd.Flags().Int("limit", 20, "Maximum dashboards to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runDashboards(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	query, _ := cmd.Flags().GetString("query")
	owner, _ := cmd.Flags().GetString("owner")
	favoriteOnly, _ := cmd.Flags().GetBool("favorite")
	fetchAll, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")

	items, total, err := collectDashboards(client, fetchAll, limit, start, pageSize)
	if err != nil {
		return err
	}
	items = filterDashboards(items, query, owner, favoriteOnly)

	target := map[string]any{}
	if query != "" {
		target["query"] = query
	}
	if owner != "" {
		target["owner"] = owner
	}
	if favoriteOnly {
		target["favorite"] = true
	}

	result := map[string]any{
		"dashboards": items,
		"summary": map[string]any{
			"count": len(items),
			"total": total,
		},
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "dashboards", target, result, nil, nil, "", "", "", nil))
	}
	if len(items) == 0 {
		if mode == "summary" {
			fmt.Println("Found 0 dashboards.")
			return nil
		}
		fmt.Println("No dashboards found.")
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Found %d dashboard(s).\n", len(items))
		return nil
	}

	fmt.Printf("Dashboards (%d)\n\n", len(items))
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		favorite := ""
		if dashboardIsFavorite(item) {
			favorite = "yes"
		}
		rows = append(rows, []string{
			normalizeMaybeString(item["id"]),
			normalizeMaybeString(item["name"]),
			dashboardOwner(item),
			favorite,
			output.Truncate(normalizeMaybeString(item["description"]), 52),
		})
	}
	fmt.Println(output.TableString([]string{"ID", "NAME", "OWNER", "FAV", "DESCRIPTION"}, rows))
	return nil
}

func collectDashboards(client *Client, fetchAll bool, limit, start, pageSize int) ([]map[string]any, int, error) {
	if !fetchAll {
		resp, err := client.ListDashboards(limit, start)
		if err != nil {
			return nil, 0, err
		}
		items := dashboardsFromResponse(resp)
		total := intFromAny(resp["total"], len(items))
		if total == 0 {
			total = len(items)
		}
		return items, total, nil
	}

	if pageSize <= 0 {
		pageSize = 50
	}
	offset := start
	total := 0
	items := []map[string]any{}
	for {
		resp, err := client.ListDashboards(pageSize, offset)
		if err != nil {
			return nil, 0, err
		}
		pageItems := dashboardsFromResponse(resp)
		if total == 0 {
			total = intFromAny(resp["total"], len(pageItems))
		}
		items = append(items, pageItems...)
		offset += len(pageItems)
		if len(pageItems) == 0 || (total > 0 && offset >= total) {
			break
		}
	}
	if total == 0 {
		total = len(items)
	}
	return items, total, nil
}

func dashboardsFromResponse(resp map[string]any) []map[string]any {
	for _, key := range []string{"dashboards", "values", "results"} {
		if raw, ok := resp[key].([]any); ok {
			return coerceJSONArray(raw)
		}
	}
	return nil
}

func filterDashboards(items []map[string]any, query, owner string, favoriteOnly bool) []map[string]any {
	query = strings.ToLower(strings.TrimSpace(query))
	owner = strings.ToLower(strings.TrimSpace(owner))
	if query == "" && owner == "" && !favoriteOnly {
		return items
	}

	filtered := make([]map[string]any, 0, len(items))
	for _, item := range items {
		if favoriteOnly && !dashboardIsFavorite(item) {
			continue
		}
		if owner != "" {
			ownerFields := []string{
				strings.ToLower(dashboardOwner(item)),
				strings.ToLower(safeString(item, "owner", "accountId")),
				strings.ToLower(safeString(item, "owner", "accountID")),
				strings.ToLower(safeString(item, "owner", "name")),
			}
			matched := false
			for _, field := range ownerFields {
				if strings.Contains(field, owner) {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		if query != "" {
			haystack := strings.ToLower(strings.Join([]string{
				normalizeMaybeString(item["id"]),
				normalizeMaybeString(item["name"]),
				normalizeMaybeString(item["description"]),
				dashboardOwner(item),
			}, " "))
			if !strings.Contains(haystack, query) {
				continue
			}
		}
		filtered = append(filtered, item)
	}
	return filtered
}

func dashboardOwner(item map[string]any) string {
	for _, key := range []string{"owner", "view", "edit"} {
		if v := safeString(item, key, "displayName"); v != "" {
			return v
		}
	}
	return safeString(item, "owner", "name")
}

func dashboardIsFavorite(item map[string]any) bool {
	for _, key := range []string{"isFavourite", "isFavorite", "favourite", "favorite"} {
		if v, ok := item[key].(bool); ok {
			return v
		}
	}
	return false
}
