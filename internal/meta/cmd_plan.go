// Package meta implements the cojira meta commands:
// bootstrap, describe, do, doctor, init, and plan.
package meta

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

// NewPlanCmd returns the "cojira plan" command which injects --plan
// (and therefore --dry-run + --diff) into any subcommand.
func NewPlanCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "plan <tool> <cmd> [args...]",
		Short: "Preview a cojira command without applying changes",
		Long:  "Preview a cojira command without applying changes. Equivalent to adding --dry-run --diff.",
		Args:  cobra.MinimumNArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			planned := injectPlan(args)
			if len(planned) == 0 {
				fmt.Fprintln(os.Stderr, "Error: Provide a command to plan.")
				return &exitError{Code: 2}
			}
			rootCmd.SetArgs(planned)
			return rootCmd.Execute()
		},
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
	}
	return cmd
}

// injectPlan inserts --plan after the first subcommand argument
// unless a plan/dry-run/preview/diff flag is already present.
func injectPlan(args []string) []string {
	if len(args) == 0 {
		return args
	}
	// Strip leading "--" separator.
	if args[0] == "--" {
		args = args[1:]
	}
	if len(args) == 0 {
		return args
	}
	for _, flag := range args {
		switch flag {
		case "--plan", "--dry-run", "--preview", "--diff":
			return args
		}
	}
	// Insert --plan after the first arg (the subcommand name).
	result := make([]string, 0, len(args)+1)
	result = append(result, args[0], "--plan")
	result = append(result, args[1:]...)
	return result
}

// exitError is a simple error that carries an exit code.
type exitError struct {
	Code int
}

func (e *exitError) Error() string {
	return fmt.Sprintf("exit code %d", e.Code)
}

// ExitCode returns the exit code carried by this error.
func (e *exitError) ExitCode() int {
	return e.Code
}
