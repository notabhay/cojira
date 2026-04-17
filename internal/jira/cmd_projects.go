package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewProjectsCmd creates the "projects" subcommand.
func NewProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "projects [query]",
		Short: "List visible Jira projects",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runProjects,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runProjects(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}

	projects, err := client.ListProjects()
	if err != nil {
		return err
	}

	if query != "" {
		needle := strings.ToLower(query)
		filtered := make([]map[string]any, 0, len(projects))
		for _, project := range projects {
			key := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", project["key"])))
			name := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", project["name"])))
			if strings.Contains(key, needle) || strings.Contains(name, needle) {
				filtered = append(filtered, project)
			}
		}
		projects = filtered
	}

	target := map[string]any{}
	if query != "" {
		target["query"] = query
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "projects",
			target,
			map[string]any{
				"projects": projects,
				"summary":  map[string]any{"count": len(projects)},
			},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		if query != "" {
			fmt.Printf("Found %d project(s) matching %q.\n", len(projects), query)
		} else {
			fmt.Printf("Found %d project(s).\n", len(projects))
		}
		return nil
	}

	if len(projects) == 0 {
		fmt.Println("No projects found.")
		return nil
	}

	fmt.Printf("Projects (%d):\n\n", len(projects))
	rows := make([][]string, 0, len(projects))
	for _, project := range projects {
		rows = append(rows, []string{
			normalizeMaybeString(project["key"]),
			output.Truncate(normalizeMaybeString(project["projectTypeKey"]), 16),
			output.Truncate(normalizeMaybeString(project["name"]), 56),
		})
	}
	fmt.Println(output.TableString([]string{"KEY", "TYPE", "NAME"}, rows))
	return nil
}
