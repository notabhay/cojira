package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/notabhay/cojira/internal/board"
	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/config"
	"github.com/notabhay/cojira/internal/confluence"
	"github.com/notabhay/cojira/internal/dotenv"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/meta"
	"github.com/notabhay/cojira/internal/output"
	"github.com/notabhay/cojira/internal/version"
)

func emitStructuredError(err error, exitCode int) bool {
	mode := output.GetMode()
	if mode != "json" && mode != "ndjson" {
		return false
	}

	command := "execute"
	for _, arg := range os.Args[1:] {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		command = arg
		break
	}

	errObj := map[string]any{
		"code":         cerrors.Error,
		"message":      err.Error(),
		"user_message": cerrors.DefaultUserMessage(cerrors.Error, err.Error()),
		"recovery":     cerrors.DefaultRecovery(cerrors.Error),
	}

	var ce *cerrors.CojiraError
	if errors.As(err, &ce) {
		obj, buildErr := output.ErrorObj(ce.Code, ce.Message, ce.Hint, ce.UserMessage, ce.Recovery)
		if buildErr == nil {
			errObj = obj
		}
	} else {
		var cfgErr *config.ConfigError
		if errors.As(err, &cfgErr) {
			obj, buildErr := output.ErrorObj(cerrors.ConfigInvalid, cfgErr.Message, cerrors.HintSetup(), cfgErr.UserMessage, nil)
			if buildErr == nil {
				errObj = obj
			}
		}
	}

	env := output.BuildEnvelope(
		false,
		"cojira",
		command,
		map[string]any{"argv": os.Args[1:]},
		nil,
		nil,
		[]any{errObj},
		"",
		"",
		"",
		&exitCode,
	)
	_ = output.PrintJSON(env)
	return true
}

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
	rootCmd.AddCommand(meta.NewApplyCmd())
	rootCmd.AddCommand(meta.NewAuthCmd())
	rootCmd.AddCommand(meta.NewCacheCmd())
	rootCmd.AddCommand(meta.NewCompletionCmd(rootCmd))
	rootCmd.AddCommand(meta.NewConvertCmd())
	rootCmd.AddCommand(meta.NewDescribeCmd(rootCmd))
	rootCmd.AddCommand(meta.NewDoCmd(rootCmd))
	rootCmd.AddCommand(meta.NewDoctorCmd())
	rootCmd.AddCommand(meta.NewDryRunCmd(rootCmd))
	rootCmd.AddCommand(meta.NewEventsCmd())
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
			code = cfgErr.ExitCode()
		}

		// Check for meta exitError (carries ExitCode() method).
		type exitCoder interface{ ExitCode() int }
		if ec, ok := err.(exitCoder); ok {
			code = ec.ExitCode()
		}

		type reported interface{ Reported() bool }
		if rep, ok := err.(reported); ok && rep.Reported() {
			if output.CurrentEventStreamID() != "" {
				output.EmitError("", err.Error(), map[string]any{"exit_code": code})
			}
			os.Exit(code)
		}

		if emitStructuredError(err, code) {
			if output.CurrentEventStreamID() != "" {
				output.EmitError("", err.Error(), map[string]any{"exit_code": code})
			}
			os.Exit(code)
		}

		// Print the error since SilenceErrors suppresses cobra's default printing.
		if output.CurrentEventStreamID() != "" {
			output.EmitError("", err.Error(), map[string]any{"exit_code": code})
		}
		fmt.Fprintf(os.Stderr, "Error: %s\n", err.Error())
		os.Exit(code)
	}
}
