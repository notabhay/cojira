package confluence

import (
	"fmt"
	"os"
	"strings"

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
		treeNodes, treeErr := collectPageTree(client, pageID, title, maxDepth)
		if treeErr != nil {
			errObj, _ := output.ErrorObj(cerrors.FetchFailed, treeErr.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "tree",
				map[string]any{"page": pageArg, "page_id": pageID},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		nodes := make([]map[string]any, 0, len(treeNodes))
		for _, node := range treeNodes {
			var parentID any
			if node.ParentID != "" {
				parentID = node.ParentID
			}
			nodes = append(nodes, map[string]any{"id": node.ID, "title": node.Title, "parent_id": parentID, "depth": node.Depth})
		}
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
		treeNodes, treeErr := collectPageTree(client, pageID, title, maxDepth)
		if treeErr != nil {
			return treeErr
		}
		var directChildren []string
		for _, node := range treeNodes {
			if node.Depth == 1 {
				directChildren = append(directChildren, node.Title)
			}
		}
		preview := strings.Join(directChildren, ", ")
		if len(directChildren) > 3 {
			preview = strings.Join(directChildren[:3], ", ") + fmt.Sprintf(" (+%d more)", len(directChildren)-3)
		}
		if preview != "" {
			fmt.Printf("Tree for %s (%s): %d page(s) visible to depth %d. Direct children: %s.\n", title, pageID, len(treeNodes), maxDepth, preview)
		} else {
			fmt.Printf("Tree for %s (%s): %d page(s) visible to depth %d.\n", title, pageID, len(treeNodes), maxDepth)
		}
		return nil
	}

	fmt.Printf("%s (%s)\n", title, pageID)
	children, err := client.GetChildren(pageID, 100)
	if err != nil {
		return err
	}
	for i, child := range children {
		isLast := i == len(children)-1
		childID, _ := child["id"].(string)
		childTitle, _ := child["title"].(string)
		if err := printTreeNode(client, childID, childTitle, 1, "", isLast, maxDepth); err != nil {
			return err
		}
	}
	return nil
}

func printTreeNode(client *Client, pageID, title string, depth int, prefix string, isLast bool, maxDepth int) error {
	connector := "├── "
	if isLast {
		connector = "└── "
	}
	fmt.Printf("%s%s%s (%s)\n", prefix, connector, title, pageID)

	if depth >= maxDepth {
		return nil
	}

	children, err := client.GetChildren(pageID, 100)
	if err != nil {
		return err
	}
	if len(children) == 0 {
		return nil
	}

	newPrefix := prefix + "│   "
	if isLast {
		newPrefix = prefix + "    "
	}
	for i, child := range children {
		isChildLast := i == len(children)-1
		childID, _ := child["id"].(string)
		childTitle, _ := child["title"].(string)
		if err := printTreeNode(client, childID, childTitle, depth+1, newPrefix, isChildLast, maxDepth); err != nil {
			return err
		}
	}
	return nil
}
