package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/notabhay/cojira/internal/board"
	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/confluence"
	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/meta"
	"github.com/notabhay/cojira/internal/version"
)

func main() {
	dotenv.LoadDefaultOnce()

	rootCmd := cli.NewRootCmd(version.Version)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	rootCmd.SetContext(ctx)

	// Build jira command and attach board subcommands.
	jiraCmd := jira.NewJiraCmd()
	board.RegisterBoardCommands(jiraCmd, jira.ClientFromCmd)

	// Register all top-level commands.
	rootCmd.AddCommand(confluence.NewConfluenceCmd())
	rootCmd.AddCommand(jiraCmd)
	rootCmd.AddCommand(meta.NewBootstrapCmd())
	rootCmd.AddCommand(meta.NewCompletionCmd(rootCmd))
	rootCmd.AddCommand(meta.NewConvertCmd())
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

		var cfgErr *config.ConfigError
		if errors.As(err, &cfgErr) {
			code = cfgErr.ExitCode
		}

		// Check for meta exitError (carries ExitCode() method).
		type exitCoder interface{ ExitCode() int }
		if ec, ok := err.(exitCoder); ok {
			code = ec.ExitCode()
		}

		type reported interface{ Reported() bool }
		if rep, ok := err.(reported); ok && rep.Reported() {
			os.Exit(code)
		}

		// Print the error since SilenceErrors suppresses cobra's default printing.
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(code)
	}
}
