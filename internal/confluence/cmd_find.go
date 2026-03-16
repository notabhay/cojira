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
		cql = fmt.Sprintf(`space=%s AND title~"%s"`, space, query)
	} else {
		cql = fmt.Sprintf(`title~"%s"`, query)
	}

	data, err := client.CQL(cql, limit, 0)
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
			map[string]any{"query": query, "space": space, "cql": cql},
			results, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		if len(pages) == 0 {
			fmt.Printf("Found 0 page(s) for query: %s\n", query)
			return nil
		}
		preview := make([]string, 0, 3)
		for _, p := range pages {
			pm, ok := p.(map[string]any)
			if !ok {
				continue
			}
			content, _ := pm["content"].(map[string]any)
			if content == nil {
				continue
			}
			preview = append(preview, fmt.Sprintf("[%s] %v", getNestedString(content, "space", "key"), content["title"]))
			if len(preview) == 3 {
				break
			}
		}
		if len(preview) > 0 {
			fmt.Printf("Found %d page(s) for query: %s. First results: %s\n", len(pages), query, strings.Join(preview, "; "))
		} else {
			fmt.Printf("Found %d page(s) for query: %s\n", len(pages), query)
		}
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
