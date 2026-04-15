package confluence

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDeleteCmd creates the "delete" subcommand.
func NewDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <page>",
		Short: "Delete a Confluence page",
		Args:  cobra.ExactArgs(1),
		RunE:  runDelete,
	}
	cmd.Flags().Bool("dry-run", false, "Preview deletion without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm destructive deletion")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runDelete(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		return err
	}

	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	target := map[string]any{"page": pageArg, "page_id": pageID}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "delete", target, map[string]any{"dry_run": true}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would delete page %s.\n", pageID)
			return nil
		}
		fmt.Printf("Would delete page %s.\n", pageID)
		return nil
	}
	if !yes {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Deletion is destructive. Preview with --dry-run first, then rerun with --yes.", ExitCode: 2}
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "delete", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate delete for %s.\n", pageID)
		return nil
	}

	if err := client.DeleteContent(pageID); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.delete %s", pageID))
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "delete", target, map[string]any{"deleted": true}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Deleted page %s.\n", pageID)
		return nil
	}
	fmt.Printf("Deleted page %s.\n", pageID)
	return nil
}
