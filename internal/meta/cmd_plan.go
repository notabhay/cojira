// Package meta implements the cojira meta commands:
// bootstrap, describe, do, doctor, init, and plan.
package meta

import (
	"fmt"
	"os"
	"strings"

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
			planned := injectPlan(rootCmd, args)
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

// injectPlan inserts --plan after the deepest matched subcommand
// unless a plan/dry-run/preview/diff flag is already present.
func injectPlan(rootCmd *cobra.Command, args []string) []string {
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
	insertAt := matchedCommandDepth(rootCmd, args)
	if insertAt <= 0 {
		insertAt = 1
	}
	result := make([]string, 0, len(args)+1)
	result = append(result, args[:insertAt]...)
	result = append(result, "--plan")
	result = append(result, args[insertAt:]...)
	return result
}

func matchedCommandDepth(rootCmd *cobra.Command, args []string) int {
	if rootCmd == nil {
		return 0
	}
	current := rootCmd
	depth := 0
	for _, token := range args {
		if strings.HasPrefix(token, "-") {
			break
		}
		var next *cobra.Command
		for _, sub := range current.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			if sub.Name() == token || sub.HasAlias(token) {
				next = sub
				break
			}
		}
		if next == nil {
			break
		}
		current = next
		depth++
	}
	return depth
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

func (e *exitError) Reported() bool {
	return true
}
