package jira

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewUsersCmd creates the "users" subcommand.
func NewUsersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "users <query>",
		Short: "Search Jira users",
		Args:  cobra.ExactArgs(1),
		RunE:  runUsers,
	}
	cmd.Flags().Int("limit", 20, "Maximum number of users to return")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runUsers(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	query := args[0]
	limit, _ := cmd.Flags().GetInt("limit")

	users, err := client.SearchUsers(query, limit)
	if err != nil {
		return err
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "users",
			map[string]any{"query": query},
			map[string]any{
				"users":   users,
				"summary": map[string]any{"count": len(users)},
			},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		fmt.Printf("Found %d Jira user(s) matching %q.\n", len(users), query)
		return nil
	}

	if len(users) == 0 {
		fmt.Println("No users found.")
		return nil
	}

	fmt.Printf("Users (%d):\n\n", len(users))
	rows := make([][]string, 0, len(users))
	for _, user := range users {
		rows = append(rows, []string{
			output.Truncate(formatUserDisplay(user), 32),
			output.Truncate(normalizeMaybeString(user["accountId"]), 32),
			output.Truncate(normalizeMaybeString(user["name"]), 24),
		})
	}
	fmt.Println(output.TableString([]string{"DISPLAY", "ACCOUNT ID", "USERNAME"}, rows))
	return nil
}
