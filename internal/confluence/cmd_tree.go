package confluence

import (
	"fmt"
	"os"
	"sync"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

type pageTreeNode struct {
	ID       string
	Title    string
	Children []pageTreeNode
}

// NewTreeCmd creates the "tree" subcommand.
func NewTreeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "tree <page>",
		Short: "Show page hierarchy",
		Args:  cobra.ExactArgs(1),
		RunE:  runTree,
	}
	cmd.Flags().IntP("depth", "d", 3, "Max depth (default: 3)")
	cmd.Flags().Int("concurrency", 1, "Number of concurrent tree fetch workers (default: 1, max: 10)")
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
	concurrency, _ := cmd.Flags().GetInt("concurrency")
	concurrency = cli.ClampConcurrency(concurrency)

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
	tree := fetchPageTree(client, pageID, title, 0, maxDepth, newConfluenceSemaphore(concurrency))

	if mode == "json" {
		nodes := []map[string]any{
			{"id": pageID, "title": title, "parent_id": nil, "depth": 0},
		}
		flattenPageTree(tree, pageID, 1, &nodes)
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
		fmt.Printf("Tree for %s (%s): %d direct child(ren), depth %d.\n", title, pageID, len(tree.Children), maxDepth)
		return nil
	}

	printPageTree(tree, 0, "", true)
	return nil
}

func newConfluenceSemaphore(concurrency int) chan struct{} {
	if concurrency <= 1 {
		return nil
	}
	return make(chan struct{}, concurrency)
}

func withConfluenceSemaphore(sem chan struct{}, fn func()) {
	if sem == nil {
		fn()
		return
	}
	sem <- struct{}{}
	defer func() { <-sem }()
	fn()
}

func getChildrenConcurrent(client *Client, pageID string, sem chan struct{}) ([]map[string]any, error) {
	var (
		children []map[string]any
		err      error
	)
	withConfluenceSemaphore(sem, func() {
		children, err = client.GetChildren(pageID, 100)
	})
	return children, err
}

func countPageTree(client *Client, pageID string, sem chan struct{}) int {
	children, err := getChildrenConcurrent(client, pageID, sem)
	if err != nil || len(children) == 0 {
		return 1
	}

	counts := make([]int, len(children))
	if len(children) == 1 || sem == nil {
		for i, child := range children {
			childID, _ := child["id"].(string)
			if childID == "" {
				continue
			}
			counts[i] = countPageTree(client, childID, sem)
		}
	} else {
		var wg sync.WaitGroup
		for i, child := range children {
			i := i
			child := child
			wg.Add(1)
			go func() {
				defer wg.Done()
				childID, _ := child["id"].(string)
				if childID == "" {
					return
				}
				counts[i] = countPageTree(client, childID, sem)
			}()
		}
		wg.Wait()
	}

	total := 1
	for _, childCount := range counts {
		total += childCount
	}
	return total
}

func fetchPageTree(client *Client, pageID, title string, depth, maxDepth int, sem chan struct{}) pageTreeNode {
	node := pageTreeNode{ID: pageID, Title: title}
	if depth >= maxDepth {
		return node
	}

	children, err := getChildrenConcurrent(client, pageID, sem)
	if err != nil || len(children) == 0 {
		return node
	}

	results := make([]pageTreeNode, len(children))
	if len(children) == 1 || sem == nil {
		for i, child := range children {
			childID, _ := child["id"].(string)
			childTitle, _ := child["title"].(string)
			results[i] = fetchPageTree(client, childID, childTitle, depth+1, maxDepth, sem)
		}
		node.Children = results
		return node
	}

	var wg sync.WaitGroup
	for i, child := range children {
		i := i
		child := child
		wg.Add(1)
		go func() {
			defer wg.Done()
			childID, _ := child["id"].(string)
			childTitle, _ := child["title"].(string)
			results[i] = fetchPageTree(client, childID, childTitle, depth+1, maxDepth, sem)
		}()
	}
	wg.Wait()
	node.Children = results
	return node
}

func flattenPageTree(node pageTreeNode, parentID string, depth int, nodes *[]map[string]any) {
	for _, child := range node.Children {
		*nodes = append(*nodes, map[string]any{
			"id":        child.ID,
			"title":     child.Title,
			"parent_id": parentID,
			"depth":     depth,
		})
		flattenPageTree(child, child.ID, depth+1, nodes)
	}
}

func printPageTree(node pageTreeNode, depth int, prefix string, isLast bool) {
	if depth == 0 {
		fmt.Printf("%s (%s)\n", node.Title, node.ID)
	} else {
		connector := "├── "
		if isLast {
			connector = "└── "
		}
		fmt.Printf("%s%s%s (%s)\n", prefix, connector, node.Title, node.ID)
	}

	newPrefix := prefix + "│   "
	if isLast {
		newPrefix = prefix + "    "
	}
	for i, child := range node.Children {
		isChildLast := i == len(node.Children)-1
		printPageTree(child, depth+1, newPrefix, isChildLast)
	}
}
