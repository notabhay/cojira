package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/cojira/cojira/internal/board"
	"github.com/cojira/cojira/internal/cli"
	"github.com/cojira/cojira/internal/confluence"
	"github.com/cojira/cojira/internal/dotenv"
	cerrors "github.com/cojira/cojira/internal/errors"
	"github.com/cojira/cojira/internal/jira"
	"github.com/cojira/cojira/internal/meta"
	"github.com/cojira/cojira/internal/version"
)

func main() {
	dotenv.LoadIfPresent(dotenv.DefaultSearchPaths())

	rootCmd := cli.NewRootCmd(version.Version)

	// Build jira command and attach board subcommands.
	jiraCmd := jira.NewJiraCmd()
	board.RegisterBoardCommands(jiraCmd, nil)

	// Register all top-level commands.
	rootCmd.AddCommand(confluence.NewConfluenceCmd())
	rootCmd.AddCommand(jiraCmd)
	rootCmd.AddCommand(meta.NewBootstrapCmd())
	rootCmd.AddCommand(meta.NewDescribeCmd(rootCmd))
	rootCmd.AddCommand(meta.NewDoCmd(rootCmd))
	rootCmd.AddCommand(meta.NewDoctorCmd())
	rootCmd.AddCommand(meta.NewInitCmd())
	rootCmd.AddCommand(meta.NewPlanCmd(rootCmd))

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
