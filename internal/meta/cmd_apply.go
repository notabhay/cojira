package meta

import (
	"fmt"
	"os"
	"os/exec"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewApplyCmd returns the "apply" command.
func NewApplyCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:           "apply <plan-id>",
		Short:         "Execute a previously recorded dry-run plan",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			yes, _ := cmd.Flags().GetBool("yes")
			forceContext, _ := cmd.Flags().GetBool("force-context")
			if !yes {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Refusing to apply a recorded plan without --yes.",
					ExitCode: 2,
				}
			}

			plan, path, err := loadRecordedPlan(args[0])
			if err != nil {
				if os.IsNotExist(err) {
					return &cerrors.CojiraError{
						Code:     cerrors.FileNotFound,
						Message:  fmt.Sprintf("Recorded plan not found: %s", args[0]),
						ExitCode: 1,
					}
				}
				return err
			}

			if !forceContext && plan.ContextFingerprint != "" && plan.ContextFingerprint != currentContextFingerprint(plan.Profile) {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Recorded plan context does not match the current Jira/Confluence profile or base URLs. Use --force-context to override.",
					ExitCode: 2,
				}
			}

			mode := output.GetMode()
			executable, err := currentExecutable()
			if err != nil {
				return err
			}
			child := exec.CommandContext(cmd.Context(), executable, plan.ApplyArgs...)
			child.Env = os.Environ()

			if mode != "json" && mode != "ndjson" {
				child.Stdout = cmd.OutOrStdout()
				child.Stderr = cmd.ErrOrStderr()
				if err := child.Run(); err != nil {
					if exitErr, ok := err.(*exec.ExitError); ok {
						return &exitError{Code: exitErr.ExitCode()}
					}
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Applied recorded plan %s from %s\n", plan.ID, path)
				return nil
			}

			stdout, stderr, exitCode, err := runRecordedSubprocess(cmd, plan.ApplyArgs)
			if err != nil {
				return err
			}
			result := map[string]any{
				"id":        plan.ID,
				"path":      path,
				"args":      plan.ApplyArgs,
				"exit_code": exitCode,
				"stdout":    stdout,
				"stderr":    stderr,
			}
			if exitCode != 0 {
				errObj, _ := output.ErrorObj(cerrors.OpFailed, strings.TrimSpace(firstNonEmpty(stderr, stdout, "apply failed")), "", "", nil)
				ec := exitCode
				return output.PrintJSON(output.BuildEnvelope(false, "cojira", "apply", map[string]any{"plan_id": plan.ID}, result, nil, []any{errObj}, "", "", "", &ec))
			}
			return output.PrintJSON(output.BuildEnvelope(true, "cojira", "apply", map[string]any{"plan_id": plan.ID}, result, nil, nil, "", "", "", nil))
		},
	}
	cmd.Flags().Bool("yes", false, "Confirm executing the recorded plan")
	cmd.Flags().Bool("force-context", false, "Apply even if the current profile/base URLs differ from the recorded context")
	return cmd
}
