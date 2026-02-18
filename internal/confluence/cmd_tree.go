package confluence

import (
	"fmt"
	"os"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewTreeCmd creates the "tree" subcommand.
func NewTreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree <page>",
		Short: "Show page hierarchy",
		Args:  cobra.ExactArgs(1),
		RunE:  runTree,
	}
	cmd.Flags().IntP("depth", "d", 3, "Max depth (default: 3)")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runTree(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	maxDepth, _ := cmd.Flags().GetInt("depth")

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "tree",
				map[string]any{"page": pageArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		return err
	}

	page, err := client.GetPageByID(pageID, "")
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "tree",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error fetching page %s: %v\n", pageID, err)
		return err
	}

	title, _ := page["title"].(string)

	if mode == "json" {
		nodes := []map[string]any{
			{"id": pageID, "title": title, "parent_id": nil, "depth": 0},
		}
		collectTreeJSON(client, pageID, pageID, 1, maxDepth, &nodes)
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "tree",
			map[string]any{"page": pageArg, "page_id": pageID},
			map[string]any{
				"root":      map[string]any{"id": pageID, "title": title},
				"nodes":     nodes,
				"max_depth": maxDepth,
			},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		children, _ := client.GetChildren(pageID, 100)
		fmt.Printf("Tree for %s (%s): %d direct child(ren), depth %d.\n", title, pageID, len(children), maxDepth)
		return nil
	}

	fmt.Printf("%s (%s)\n", title, pageID)
	children, _ := client.GetChildren(pageID, 100)
	for i, child := range children {
		isLast := i == len(children)-1
		childID, _ := child["id"].(string)
		childTitle, _ := child["title"].(string)
		printTreeNode(client, childID, childTitle, 1, "", isLast, maxDepth)
	}
	return nil
}

func collectTreeJSON(client *Client, rootID, parentID string, depth, maxDepth int, nodes *[]map[string]any) {
	if depth > maxDepth {
		return
	}
	children, err := client.GetChildren(parentID, 100)
	if err != nil {
		return
	}
	for _, child := range children {
		childID, _ := child["id"].(string)
		childTitle, _ := child["title"].(string)
		*nodes = append(*nodes, map[string]any{
			"id":        childID,
			"title":     childTitle,
			"parent_id": parentID,
			"depth":     depth,
		})
		if depth < maxDepth {
			collectTreeJSON(client, rootID, childID, depth+1, maxDepth, nodes)
		}
	}
}

func printTreeNode(client *Client, pageID, title string, depth int, prefix string, isLast bool, maxDepth int) {
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	fmt.Printf("%s%s%s (%s)\n", prefix, connector, title, pageID)

	if depth >= maxDepth {
		return
	}

	children, err := client.GetChildren(pageID, 100)
	if err != nil || len(children) == 0 {
		return
	}

	newPrefix := prefix + "│   "
	if isLast {
		newPrefix = prefix + "    "
	}
	for i, child := range children {
		isChildLast := i == len(children)-1
		childID, _ := child["id"].(string)
		childTitle, _ := child["title"].(string)
		printTreeNode(client, childID, childTitle, depth+1, newPrefix, isChildLast, maxDepth)
	}
}
