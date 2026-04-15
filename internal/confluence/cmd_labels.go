package confluence

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewLabelsCmd creates the "labels" subcommand.
func NewLabelsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "labels <page>",
		Short: "List or add Confluence labels",
		Args:  cobra.ExactArgs(1),
		RunE:  runLabels,
	}
	cmd.Flags().StringArray("add", nil, "Label to add (repeatable)")
	cmd.Flags().Bool("all", false, "Fetch all labels")
	cmd.Flags().Int("limit", 25, "Maximum labels to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cmd.Flags().Bool("plan", false, "Preview label additions without applying")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runLabels(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageID, err := ResolvePageID(client, args[0], defPageID)
	if err != nil {
		return err
	}

	labelsToAdd, _ := cmd.Flags().GetStringArray("add")
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	planMode, _ := cmd.Flags().GetBool("plan")

	existing, err := collectLabels(client, pageID, all, limit, start, pageSize)
	if err != nil {
		return err
	}

	if len(labelsToAdd) == 0 {
		return printLabels(mode, pageID, existing)
	}

	existingSet := map[string]bool{}
	for _, label := range existing {
		existingSet[strings.ToLower(label)] = true
	}

	var addList []string
	var skipped []string
	for _, label := range labelsToAdd {
		trimmed := strings.TrimSpace(label)
		if trimmed == "" {
			continue
		}
		if existingSet[strings.ToLower(trimmed)] {
			skipped = append(skipped, trimmed)
			continue
		}
		addList = append(addList, trimmed)
	}

	target := map[string]any{"page": args[0], "page_id": pageID}
	result := map[string]any{"add": addList, "skipped": skipped}

	if planMode {
		result["plan"] = true
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "labels", target, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would add %d label(s) to page %s.\n", len(addList), pageID)
			return nil
		}
		fmt.Printf("Would add %d label(s) to page %s.\n", len(addList), pageID)
		return nil
	}

	for _, label := range addList {
		if err := client.SetPageLabel(pageID, label); err != nil {
			return err
		}
	}

	result["added"] = len(addList)
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "labels", target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Added %d label(s) to page %s.\n", len(addList), pageID)
		return nil
	}
	fmt.Printf("Added labels to %s: %s\n", pageID, strings.Join(addList, ", "))
	return nil
}

func collectLabels(client *Client, pageID string, all bool, limit, start, pageSize int) ([]string, error) {
	var items []string
	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.GetPageLabels(pageID, pageSize, offset)
			if err != nil {
				return nil, err
			}
			pageItems := extractLabelNames(data)
			items = append(items, pageItems...)
			if len(pageItems) < pageSize {
				break
			}
			offset += len(pageItems)
		}
		return items, nil
	}

	data, err := client.GetPageLabels(pageID, limit, start)
	if err != nil {
		return nil, err
	}
	return extractLabelNames(data), nil
}

func extractLabelNames(data map[string]any) []string {
	raw, _ := data["results"].([]any)
	items := make([]string, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			name := strings.TrimSpace(fmt.Sprintf("%v", m["name"]))
			if name != "" && name != "<nil>" {
				items = append(items, name)
			}
		}
	}
	return items
}

func printLabels(mode, pageID string, labels []string) error {
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "labels",
			map[string]any{"page_id": pageID},
			map[string]any{"labels": labels, "summary": map[string]any{"count": len(labels)}},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Found %d label(s) on page %s.\n", len(labels), pageID)
		return nil
	}

	if len(labels) == 0 {
		fmt.Println("No labels found.")
		return nil
	}

	fmt.Printf("Labels on %s:\n\n", pageID)
	for _, label := range labels {
		fmt.Printf("  - %s\n", label)
	}
	return nil
}
