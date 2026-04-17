package jira

import (
	"errors"
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDashboardCmd creates the "dashboard" subcommand.
func NewDashboardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dashboard <dashboard>",
		Short: "Show Jira dashboard details",
		Args:  cobra.ExactArgs(1),
		RunE:  runDashboard,
	}
	cmd.Flags().Bool("gadgets", false, "Include dashboard gadgets")
	cmd.Flags().Int("gadget-limit", 100, "Maximum gadgets to fetch when using --gadgets")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runDashboard(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	dashboardID := stringsTrim(args[0])
	includeGadgets, _ := cmd.Flags().GetBool("gadgets")
	gadgetLimit, _ := cmd.Flags().GetInt("gadget-limit")

	dashboard, err := client.GetDashboard(dashboardID)
	if err != nil {
		return err
	}

	result := map[string]any{"dashboard": dashboard}
	if includeGadgets {
		gadgets, total, err := collectDashboardGadgets(client, dashboardID, gadgetLimit)
		if err != nil {
			var ce *cerrors.CojiraError
			if errors.As(err, &ce) && ce.Code == cerrors.HTTPError && strings.Contains(ce.Message, "HTTP 404") {
				result["gadgets"] = []map[string]any{}
				result["gadgets_summary"] = map[string]any{"count": 0, "total": 0, "supported": false}
			} else {
				return err
			}
		} else {
			result["gadgets"] = gadgets
			result["gadgets_summary"] = map[string]any{"count": len(gadgets), "total": total, "supported": true}
		}
	}

	target := map[string]any{"dashboard": dashboardID}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "dashboard", target, result, nil, nil, "", "", "", nil))
	}
	name := normalizeMaybeString(dashboard["name"])
	if name == "" {
		name = dashboardID
	}
	if mode == "summary" {
		if includeGadgets {
			gadgets, _ := result["gadgets"].([]map[string]any)
			gadgetsSummary, _ := result["gadgets_summary"].(map[string]any)
			if gadgetsSummary != nil && gadgetsSummary["supported"] == false {
				fmt.Printf("Dashboard %s. Gadget enumeration is unavailable on this Jira instance.\n", name)
				return nil
			}
			fmt.Printf("Dashboard %s with %d gadget(s).\n", name, len(gadgets))
			return nil
		}
		fmt.Printf("Dashboard %s.\n", name)
		return nil
	}

	fmt.Printf("Dashboard %s (%s)\n", name, dashboardID)
	detailRows := [][]string{}
	if desc := normalizeMaybeString(dashboard["description"]); desc != "" {
		detailRows = append(detailRows, []string{"Description", output.Truncate(compactWhitespace(desc), 96)})
	}
	if owner := dashboardOwner(dashboard); owner != "" {
		detailRows = append(detailRows, []string{"Owner", owner})
	}
	if dashboardIsFavorite(dashboard) {
		detailRows = append(detailRows, []string{"Favorite", "yes"})
	}
	if permalink := normalizeMaybeString(dashboard["view"]); permalink != "" {
		detailRows = append(detailRows, []string{"View", output.Truncate(permalink, 96)})
	}
	if edit := normalizeMaybeString(dashboard["edit"]); edit != "" {
		detailRows = append(detailRows, []string{"Edit", output.Truncate(edit, 96)})
	}
	if len(detailRows) > 0 {
		fmt.Println()
		fmt.Println(output.TableString([]string{"FIELD", "VALUE"}, detailRows))
	}
	if includeGadgets {
		gadgets, _ := result["gadgets"].([]map[string]any)
		gadgetsSummary, _ := result["gadgets_summary"].(map[string]any)
		if gadgetsSummary != nil && gadgetsSummary["supported"] == false {
			fmt.Println("\nGadgets: unavailable on this Jira instance.")
		} else {
			fmt.Printf("\nGadgets (%d)\n", len(gadgets))
			if len(gadgets) == 0 {
				fmt.Println("No gadgets found.")
				return nil
			}
			rows := make([][]string, 0, len(gadgets))
			for _, gadget := range gadgets {
				title := normalizeMaybeString(gadget["title"])
				if title == "" {
					title = normalizeMaybeString(gadget["moduleKey"])
				}
				rows = append(rows, []string{
					normalizeMaybeString(gadget["id"]),
					output.Truncate(title, 48),
					output.Truncate(normalizeMaybeString(gadget["moduleKey"]), 40),
				})
			}
			fmt.Println(output.TableString([]string{"ID", "TITLE", "MODULE"}, rows))
		}
	}
	return nil
}

func collectDashboardGadgets(client *Client, dashboardID string, limit int) ([]map[string]any, int, error) {
	if limit <= 0 {
		limit = 100
	}
	resp, err := client.ListDashboardGadgets(dashboardID, limit, 0)
	if err != nil {
		return nil, 0, err
	}
	items := dashboardGadgetsFromResponse(resp)
	total := intFromAny(resp["total"], len(items))
	if total == 0 {
		total = len(items)
	}
	return items, total, nil
}

func dashboardGadgetsFromResponse(resp map[string]any) []map[string]any {
	for _, key := range []string{"gadgets", "values", "results"} {
		if raw, ok := resp[key].([]any); ok {
			return coerceJSONArray(raw)
		}
	}
	return nil
}

func stringsTrim(v string) string {
	return normalizeMaybeString(v)
}
