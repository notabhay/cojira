package jira

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewWhoamiCmd creates the "whoami" subcommand.
func NewWhoamiCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "whoami",
		Short: "Show current Jira user",
		Args:  cobra.NoArgs,
		RunE:  runWhoami,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runWhoami(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	me, err := client.GetMyself()
	if err != nil {
		return err
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "whoami",
			map[string]any{},
			me, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		name := firstOf(me, "displayName", "name", "accountId")
		if name == "" {
			name = "Unknown user"
		}
		email, _ := me["emailAddress"].(string)
		if email != "" {
			fmt.Printf("Authenticated as %s (%s).\n", name, email)
		} else {
			fmt.Printf("Authenticated as %s.\n", name)
		}
		return nil
	}

	fmt.Printf("Display Name: %v\n", me["displayName"])
	if v, ok := me["name"].(string); ok && v != "" {
		fmt.Printf("Username:     %s\n", v)
	}
	if v, ok := me["key"].(string); ok && v != "" {
		fmt.Printf("User Key:     %s\n", v)
	}
	if v, ok := me["accountId"].(string); ok && v != "" {
		fmt.Printf("Account ID:   %s\n", v)
	}
	if v, ok := me["emailAddress"].(string); ok && v != "" {
		fmt.Printf("Email:        %s\n", v)
	}
	if v, ok := me["timeZone"].(string); ok && v != "" {
		fmt.Printf("Time Zone:    %s\n", v)
	}
	return nil
}

func firstOf(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
