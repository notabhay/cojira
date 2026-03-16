package confluence

import "fmt"

type pageTreeNode struct {
	ID       string
	Title    string
	ParentID string
	Depth    int
}

func collectPageTree(client *Client, rootID, rootTitle string, maxDepth int) ([]pageTreeNode, error) {
	nodes := []pageTreeNode{{ID: rootID, Title: rootTitle, ParentID: "", Depth: 0}}
	if maxDepth <= 0 {
		return nodes, nil
	}
	if err := appendPageTreeNodes(client, rootID, 1, maxDepth, &nodes); err != nil {
		return nil, err
	}
	return nodes, nil
}

func appendPageTreeNodes(client *Client, parentID string, depth, maxDepth int, nodes *[]pageTreeNode) error {
	if depth > maxDepth {
		return nil
	}
	children, err := client.GetChildren(parentID, 100)
	if err != nil {
		return fmt.Errorf("could not fetch children for page %s: %w", parentID, err)
	}
	for _, child := range children {
		childID, _ := child["id"].(string)
		childTitle, _ := child["title"].(string)
		*nodes = append(*nodes, pageTreeNode{
			ID:       childID,
			Title:    childTitle,
			ParentID: parentID,
			Depth:    depth,
		})
		if depth < maxDepth {
			if err := appendPageTreeNodes(client, childID, depth+1, maxDepth, nodes); err != nil {
				return err
			}
		}
	}
	return nil
}

func collectDescendantIDs(client *Client, rootID string) ([]string, error) {
	var out []string
	stack := []string{rootID}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		children, err := client.GetChildren(pid, 100)
		if err != nil {
			return nil, fmt.Errorf("could not fetch children for page %s: %w", pid, err)
		}
		for _, child := range children {
			childID, _ := child["id"].(string)
			if childID != "" {
				out = append(out, childID)
				stack = append(stack, childID)
			}
		}
	}
	return out, nil
}
