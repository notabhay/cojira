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

// NewFindCmd creates the "find" subcommand.
func NewFindCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "find <query>",
		Short: "Search for pages by title or CQL",
		Args:  cobra.ExactArgs(1),
		RunE:  runFind,
	}
	cmd.Flags().StringP("space", "s", "", "Limit to space")
	cmd.Flags().IntP("limit", "l", 20, "Max results")
	cmd.Flags().Int("start", 0, "Start offset (default: 0)")
	cmd.Flags().Int("page-size", 100, "Page size when fetching --all results (default: 100)")
	cmd.Flags().Bool("all", false, "Fetch all pages of results")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func runFind(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	query := strings.TrimSpace(args[0])
	space, _ := cmd.Flags().GetString("space")
	if space == "" {
		space = defaultSpace(cfgData)
	}
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	fetchAll, _ := cmd.Flags().GetBool("all")
	if pageSize <= 0 {
		pageSize = 100
	}

	// Build CQL query.
	var cql string
	cqlOperators := []string{"=", "~", " AND ", " OR ", " ORDER BY "}
	isCQL := false
	for _, op := range cqlOperators {
		if strings.Contains(query, op) {
			isCQL = true
			break
		}
	}
	if isCQL {
		cql = query
	} else if space != "" {
		cql = fmt.Sprintf(`space="%s" AND title~"%s"`, escapeCQLString(space), escapeCQLString(query))
	} else {
		cql = fmt.Sprintf(`title~"%s"`, escapeCQLString(query))
	}

	initialLimit := limit
	if fetchAll {
		initialLimit = pageSize
	}
	data, err := client.CQL(cql, initialLimit, start)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.SearchFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "find",
				map[string]any{"query": query, "space": space},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error searching: %v\n", err)
		return err
	}

	pages, _ := data["results"].([]any)
	if fetchAll {
		collected := make([]any, 0, len(pages))
		collected = append(collected, pages...)
		nextStart := start + len(pages)
		for len(pages) > 0 {
			if limit > 0 && len(collected) >= limit {
				collected = collected[:limit]
				break
			}
			pageLimit := pageSize
			if limit > 0 {
				remaining := limit - len(collected)
				if remaining <= 0 {
					break
				}
				if remaining < pageLimit {
					pageLimit = remaining
				}
			}
			page, err := client.CQL(cql, pageLimit, nextStart)
			if err != nil {
				return err
			}
			pageResults, _ := page["results"].([]any)
			if len(pageResults) == 0 {
				break
			}
			collected = append(collected, pageResults...)
			nextStart += len(pageResults)
			pages = pageResults
		}
		data["results"] = collected
		pages = collected
	}

	if len(pages) == 0 {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "find",
				map[string]any{"query": query, "space": space},
				[]any{}, nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Found 0 page(s) for query: %s\n", query)
			return nil
		}
		fmt.Println("No pages found.")
		return nil
	}

	if mode == "json" {
		var results []any
		for _, p := range pages {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			content, _ := pm["content"].(map[string]any)
			if content == nil {
				continue
			}
			results = append(results, map[string]any{
				"id":    content["id"],
				"title": content["title"],
				"space": getNestedString(content, "space", "key"),
				"url":   pm["url"],
			})
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "find",
			map[string]any{"query": query, "space": space, "cql": cql, "all": fetchAll, "start": start},
			results, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Found %d page(s) for query: %s\n", len(pages), query)
		return nil
	}

	fmt.Printf("Found %d page(s):\n\n", len(pages))
	for _, p := range pages {
		pm, ok := p.(map[string]any)
		if !ok {
			continue
		}
		content, _ := pm["content"].(map[string]any)
		if content == nil {
			continue
		}
		spaceKey := getNestedString(content, "space", "key")
		if spaceKey == "" {
			spaceKey = "?"
		}
		fmt.Printf("  %12v  [%s]  %v\n", content["id"], spaceKey, content["title"])
	}
	return nil
}

func escapeCQLString(value string) string {
	v := strings.TrimSpace(value)
	v = strings.ReplaceAll(v, `\`, `\\`)
	v = strings.ReplaceAll(v, `"`, `\"`)
	return v
}
