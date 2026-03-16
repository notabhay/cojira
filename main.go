package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/notabhay/cojira/internal/board"
	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/confluence"
	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/meta"
	"github.com/notabhay/cojira/internal/version"
	"github.com/spf13/cobra"
)

func buildRootCmd() *cobra.Command {
	dotenv.LoadIfPresent(dotenv.DefaultSearchPaths())

	rootCmd := cli.NewRootCmd(version.Version)

	// Build jira command and attach board subcommands.
	jiraCmd := jira.NewJiraCmd()
	board.RegisterBoardCommands(jiraCmd, jira.ClientFromCmd)

	// Register all top-level commands.
	rootCmd.AddCommand(confluence.NewConfluenceCmd())
	rootCmd.AddCommand(jiraCmd)
	rootCmd.AddCommand(meta.NewBootstrapCmd())
	rootCmd.AddCommand(meta.NewDescribeCmd(rootCmd))
	rootCmd.AddCommand(meta.NewDoCmd(rootCmd))
	rootCmd.AddCommand(meta.NewDoctorCmd())
	rootCmd.AddCommand(meta.NewInitCmd())
	rootCmd.AddCommand(meta.NewPlanCmd(rootCmd))
	return rootCmd
}

func main() {
	rootCmd := buildRootCmd()
	if err := cli.Execute(rootCmd); err != nil {
		code := 1

		// Check for CojiraError exit code.
		var ce *cerrors.CojiraError
		if errors.As(err, &ce) {
			code = ce.ExitCode
		}

		// Check for meta exitError (carries ExitCode() method).
		type exitCoder interface{ ExitCode() int }
		if ec, ok := err.(exitCoder); ok {
			code = ec.ExitCode()
		}

		// Print the error since SilenceErrors suppresses cobra's default printing.
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(code)
	}
}
