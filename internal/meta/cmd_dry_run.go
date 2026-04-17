package meta

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDryRunCmd returns the "dry-run" command group.
func NewDryRunCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dry-run",
		Short: "Record previewable command executions for later apply",
	}
	cmd.AddCommand(newDryRunRecordCmd(rootCmd))
	return cmd
}

func newDryRunRecordCmd(rootCmd *cobra.Command) *cobra.Command {
	cmd := &cobra.Command{
		Use:                "record <tool> <cmd> [args...]",
		Short:              "Preview a command, persist the preview, and save it for later apply",
		Args:               cobra.MinimumNArgs(2),
		DisableFlagParsing: true,
		SilenceUsage:       true,
		SilenceErrors:      true,
		RunE: func(cmd *cobra.Command, args []string) error {
			childArgs := stripLeadingDoubleDash(args)
			if containsPreviewFlag(childArgs) {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Do not include --plan, --dry-run, --preview, or --diff when using dry-run record.",
					ExitCode: 2,
				}
			}

			profile, err := cli.SelectedProfile(cmd)
			if err != nil {
				return err
			}
			plannedArgs := injectPlan(rootCmd, childArgs)
			execPlanned := prependProfileArg(plannedArgs, profile)
			execApply := prependProfileArg(childArgs, profile)
			stdout, stderr, exitCode, err := runRecordedSubprocess(cmd, execPlanned)
			if err != nil {
				return err
			}
			if exitCode != 0 {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  strings.TrimSpace(firstNonEmpty(stderr, stdout, "planned command failed")),
					ExitCode: exitCode,
				}
			}

			executable, err := currentExecutable()
			if err != nil {
				return err
			}
			plan := recordedPlan{
				ID:                 newRecordedPlanID(childArgs),
				RecordedAt:         output.UTCNowISO(),
				Args:               append([]string{}, childArgs...),
				PlannedArgs:        append([]string{}, execPlanned...),
				ApplyArgs:          append([]string{}, execApply...),
				CommandPath:        resolveCommandPath(rootCmd, childArgs),
				Profile:            profile,
				ContextFingerprint: currentContextFingerprint(profile),
				Executable:         executable,
				Preview: recordedPlanPreview{
					ExitCode: exitCode,
					Stdout:   stdout,
					Stderr:   stderr,
				},
			}
			path, err := saveRecordedPlan(plan)
			if err != nil {
				return err
			}

			fmt.Fprintf(cmd.OutOrStdout(), "Recorded plan %s at %s\n", plan.ID, path)
			if strings.TrimSpace(stdout) != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Preview saved (%d stdout bytes)\n", len(stdout))
			}
			if strings.TrimSpace(stderr) != "" {
				fmt.Fprintf(cmd.OutOrStdout(), "Preview stderr saved (%d bytes)\n", len(stderr))
			}
			return nil
		},
	}
	return cmd
}

func currentExecutable() (string, error) {
	if override := strings.TrimSpace(os.Getenv("COJIRA_EXECUTABLE_OVERRIDE")); override != "" {
		return override, nil
	}
	return os.Executable()
}

func stripLeadingDoubleDash(args []string) []string {
	if len(args) > 0 && args[0] == "--" {
		return args[1:]
	}
	return args
}

func containsPreviewFlag(args []string) bool {
	for _, arg := range args {
		switch arg {
		case "--plan", "--dry-run", "--preview", "--diff":
			return true
		}
	}
	return false
}

func prependProfileArg(args []string, profile string) []string {
	if strings.TrimSpace(profile) == "" {
		return append([]string{}, args...)
	}
	out := []string{"--profile", profile}
	out = append(out, args...)
	return out
}

func resolveCommandPath(rootCmd *cobra.Command, args []string) []string {
	depth := matchedCommandDepth(rootCmd, args)
	if depth <= 0 {
		return nil
	}
	path := make([]string, 0, depth)
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			break
		}
		path = append(path, arg)
		if len(path) == depth {
			break
		}
	}
	return path
}

func runRecordedSubprocess(cmd *cobra.Command, args []string) (string, string, int, error) {
	executable, err := currentExecutable()
	if err != nil {
		return "", "", 0, err
	}
	child := exec.CommandContext(cmd.Context(), executable, args...)
	child.Env = os.Environ()
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	child.Stdout = &stdout
	child.Stderr = &stderr
	err = child.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return stdout.String(), stderr.String(), 0, err
		}
	}
	return stdout.String(), stderr.String(), exitCode, nil
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}
