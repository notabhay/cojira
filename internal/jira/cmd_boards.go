package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewBoardsCmd creates the "boards" subcommand.
func NewBoardsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "boards [query]",
		Short: "List accessible Jira agile boards",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runBoards,
	}
	cmd.Flags().String("type", "", "Optional board type filter (scrum or kanban)")
	cmd.Flags().String("project", "", "Optional project key filter")
	cmd.Flags().Bool("all", false, "Fetch all boards")
	cmd.Flags().Int("limit", 20, "Maximum boards to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runBoards(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}
	boardType, _ := cmd.Flags().GetString("type")
	projectKey, _ := cmd.Flags().GetString("project")
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")

	items := make([]map[string]any, 0)
	total := 0

	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.ListBoards(boardType, query, projectKey, pageSize, offset)
			if err != nil {
				return err
			}
			raw, _ := data["values"].([]any)
			pageItems := coerceJSONArray(raw)
			total = intFromAny(data["total"], total)
			items = append(items, pageItems...)
			offset += len(pageItems)
			if len(pageItems) == 0 || (total > 0 && offset >= total) {
				break
			}
		}
	} else {
		data, err := client.ListBoards(boardType, query, projectKey, limit, start)
		if err != nil {
			return err
		}
		raw, _ := data["values"].([]any)
		items = coerceJSONArray(raw)
		total = intFromAny(data["total"], len(items))
	}

	target := map[string]any{}
	if query != "" {
		target["query"] = query
	}
	if boardType != "" {
		target["type"] = boardType
	}
	if projectKey != "" {
		target["project"] = projectKey
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "boards",
			target,
			map[string]any{
				"boards":  items,
				"summary": map[string]any{"count": len(items), "total": total},
			},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Found %d board(s).\n", len(items))
		return nil
	}

	if len(items) == 0 {
		fmt.Println("No boards found.")
		return nil
	}

	fmt.Printf("Boards (%d):\n\n", len(items))
	rows := make([][]string, 0, len(items))
	for _, board := range items {
		location, _ := board["location"].(map[string]any)
		project := normalizeMaybeString(location["projectKey"])
		if project == "" {
			project = normalizeMaybeString(location["displayName"])
		}
		rows = append(rows, []string{
			normalizeMaybeString(board["id"]),
			strings.ToUpper(normalizeMaybeString(board["type"])),
			project,
			output.Truncate(normalizeMaybeString(board["name"]), 48),
		})
	}
	fmt.Println(output.TableString([]string{"ID", "TYPE", "PROJECT", "NAME"}, rows))
	return nil
}
