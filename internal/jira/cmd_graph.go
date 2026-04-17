package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewGraphCmd creates the "graph" subcommand.
func NewGraphCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "graph <issue>",
		Short: "Show dependency and hierarchy relationships for an issue",
		Args:  cobra.ExactArgs(1),
		RunE:  runGraph,
	}
	cmd.Flags().Int("depth", 2, "Traversal depth (default: 2)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

// NewBlockedCmd creates the "blocked" subcommand.
func NewBlockedCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "blocked <issue>",
		Short: "List transitive blockers for an issue",
		Args:  cobra.ExactArgs(1),
		RunE:  runBlocked,
	}
	cmd.Flags().Int("depth", 5, "Traversal depth (default: 5)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

// NewCriticalPathCmd creates the "critical-path" subcommand.
func NewCriticalPathCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "critical-path <issue>",
		Short: "Compute the longest blocker chain for an issue",
		Args:  cobra.ExactArgs(1),
		RunE:  runCriticalPath,
	}
	cmd.Flags().Int("depth", 8, "Traversal depth (default: 8)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

type graphNode struct {
	Key       string          `json:"key"`
	Summary   string          `json:"summary,omitempty"`
	Status    string          `json:"status,omitempty"`
	Kind      string          `json:"kind,omitempty"`
	Neighbors []graphNeighbor `json:"neighbors,omitempty"`
}

type graphNeighbor struct {
	Key      string `json:"key"`
	Relation string `json:"relation"`
}

func runGraph(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	depth, _ := cmd.Flags().GetInt("depth")
	root := ResolveIssueIdentifier(args[0])
	graph, err := collectIssueGraph(client, root, depth)
	if err != nil {
		return err
	}
	result := map[string]any{"root": root, "nodes": graph}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "graph", map[string]any{"issue": root}, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Graph for %s contains %d issue(s).\n", root, len(graph))
		return nil
	}
	fmt.Printf("Graph for %s:\n\n", root)
	nodeRows := make([][]string, 0, len(graph))
	for _, node := range graph {
		rels := make([]string, 0, len(node.Neighbors))
		for _, neighbor := range node.Neighbors {
			rels = append(rels, fmt.Sprintf("%s→%s", neighbor.Relation, neighbor.Key))
		}
		nodeRows = append(nodeRows, []string{
			node.Key,
			output.StatusBadge(node.Status),
			node.Kind,
			output.Truncate(node.Summary, 40),
			output.Truncate(strings.Join(rels, ", "), 64),
		})
	}
	fmt.Println(output.TableString([]string{"KEY", "STATUS", "TYPE", "SUMMARY", "RELATIONSHIPS"}, nodeRows))
	return nil
}

func runBlocked(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	depth, _ := cmd.Flags().GetInt("depth")
	root := ResolveIssueIdentifier(args[0])
	graph, err := collectIssueGraph(client, root, depth)
	if err != nil {
		return err
	}
	blockers := collectBlockers(root, graph)
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "blocked", map[string]any{"issue": root}, map[string]any{"issue": root, "blockers": blockers}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("%s has %d blocker(s).\n", root, len(blockers))
		return nil
	}
	if len(blockers) == 0 {
		fmt.Printf("%s has no detected blockers.\n", root)
		return nil
	}
	fmt.Printf("Blockers for %s:\n\n", root)
	rows := make([][]string, 0, len(blockers))
	for _, node := range blockers {
		rows = append(rows, []string{
			node.Key,
			output.StatusBadge(node.Status),
			output.Truncate(node.Summary, 64),
		})
	}
	fmt.Println(output.TableString([]string{"KEY", "STATUS", "SUMMARY"}, rows))
	return nil
}

func runCriticalPath(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	depth, _ := cmd.Flags().GetInt("depth")
	root := ResolveIssueIdentifier(args[0])
	graph, err := collectIssueGraph(client, root, depth)
	if err != nil {
		return err
	}
	chain := longestBlockerChain(root, graph, map[string]bool{})
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "critical-path", map[string]any{"issue": root}, map[string]any{"issue": root, "path": chain, "length": len(chain)}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Critical path for %s has length %d.\n", root, len(chain))
		return nil
	}
	if len(chain) == 0 {
		fmt.Printf("No critical path found for %s.\n", root)
		return nil
	}
	fmt.Printf("Critical path for %s:\n\n", root)
	rows := make([][]string, 0, len(chain))
	for idx, node := range chain {
		rows = append(rows, []string{
			fmt.Sprintf("%d", idx+1),
			node.Key,
			output.StatusBadge(node.Status),
			output.Truncate(node.Summary, 64),
		})
	}
	fmt.Println(output.TableString([]string{"STEP", "KEY", "STATUS", "SUMMARY"}, rows))
	return nil
}

func collectIssueGraph(client *Client, root string, depth int) ([]graphNode, error) {
	if depth < 1 {
		depth = 1
	}
	type queueItem struct {
		key   string
		depth int
	}
	queue := []queueItem{{key: root, depth: 0}}
	seen := map[string]bool{}
	nodes := make([]graphNode, 0)
	for len(queue) > 0 {
		item := queue[0]
		queue = queue[1:]
		if seen[item.key] {
			continue
		}
		seen[item.key] = true

		issue, err := client.GetIssue(item.key, "summary,status,issuetype,issuelinks,parent,subtasks", "")
		if err != nil {
			return nil, err
		}
		recordSearchRecents(client, []map[string]any{issue}, "graph")
		node := summarizeGraphIssue(issue)
		nodes = append(nodes, node)
		if item.depth >= depth {
			continue
		}
		for _, neighbor := range node.Neighbors {
			if !seen[neighbor.Key] {
				queue = append(queue, queueItem{key: neighbor.Key, depth: item.depth + 1})
			}
		}
	}
	return nodes, nil
}

func summarizeGraphIssue(issue map[string]any) graphNode {
	fields, _ := issue["fields"].(map[string]any)
	if fields == nil {
		fields = map[string]any{}
	}
	node := graphNode{
		Key:       normalizeMaybeString(issue["key"]),
		Summary:   normalizeMaybeString(fields["summary"]),
		Status:    safeString(fields, "status", "name"),
		Kind:      safeString(fields, "issuetype", "name"),
		Neighbors: []graphNeighbor{},
	}
	rawLinks, _ := fields["issuelinks"].([]any)
	for _, raw := range rawLinks {
		link, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		linkType, _ := link["type"].(map[string]any)
		if outward, ok := link["outwardIssue"].(map[string]any); ok {
			node.Neighbors = append(node.Neighbors, graphNeighbor{
				Key:      normalizeMaybeString(outward["key"]),
				Relation: normalizeMaybeString(linkType["outward"]),
			})
		}
		if inward, ok := link["inwardIssue"].(map[string]any); ok {
			node.Neighbors = append(node.Neighbors, graphNeighbor{
				Key:      normalizeMaybeString(inward["key"]),
				Relation: normalizeMaybeString(linkType["inward"]),
			})
		}
	}
	if parent, ok := fields["parent"].(map[string]any); ok {
		node.Neighbors = append(node.Neighbors, graphNeighbor{Key: normalizeMaybeString(parent["key"]), Relation: "parent"})
	}
	rawSubtasks, _ := fields["subtasks"].([]any)
	for _, raw := range rawSubtasks {
		subtask, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		node.Neighbors = append(node.Neighbors, graphNeighbor{Key: normalizeMaybeString(subtask["key"]), Relation: "subtask"})
	}
	return node
}

func collectBlockers(root string, nodes []graphNode) []graphNode {
	byKey := make(map[string]graphNode, len(nodes))
	for _, node := range nodes {
		byKey[node.Key] = node
	}
	seen := map[string]bool{}
	var result []graphNode
	var walk func(string)
	walk = func(issue string) {
		node, ok := byKey[issue]
		if !ok {
			return
		}
		for _, neighbor := range node.Neighbors {
			if !looksLikeBlocker(neighbor.Relation) || seen[neighbor.Key] {
				continue
			}
			seen[neighbor.Key] = true
			if blocker, ok := byKey[neighbor.Key]; ok {
				result = append(result, blocker)
				walk(blocker.Key)
			}
		}
	}
	walk(root)
	return result
}

func longestBlockerChain(root string, nodes []graphNode, stack map[string]bool) []graphNode {
	byKey := make(map[string]graphNode, len(nodes))
	for _, node := range nodes {
		byKey[node.Key] = node
	}
	var dfs func(string) []graphNode
	dfs = func(issue string) []graphNode {
		if stack[issue] {
			return nil
		}
		stack[issue] = true
		defer delete(stack, issue)
		node, ok := byKey[issue]
		if !ok {
			return nil
		}
		best := []graphNode{node}
		for _, neighbor := range node.Neighbors {
			if !looksLikeBlocker(neighbor.Relation) {
				continue
			}
			candidate := dfs(neighbor.Key)
			if len(candidate) > 0 && len(candidate)+1 > len(best) {
				best = append([]graphNode{node}, candidate...)
			}
		}
		return best
	}
	return dfs(root)
}

func looksLikeBlocker(relation string) bool {
	lower := strings.ToLower(strings.TrimSpace(relation))
	return strings.Contains(lower, "blocked by") ||
		strings.Contains(lower, "depends on") ||
		strings.Contains(lower, "is parent of") ||
		strings.Contains(lower, "parent")
}
