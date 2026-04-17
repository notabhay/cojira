package confluence

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewTrashCmd creates the "trash" command group.
func NewTrashCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "trash",
		Short: "List, restore, or purge trashed Confluence content",
	}
	cmd.AddCommand(
		newTrashListCmd(),
		newTrashRestoreCmd(),
		newTrashPurgeCmd(),
	)
	return cmd
}

func newTrashListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List trashed Confluence pages",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			spaceKey, _ := cmd.Flags().GetString("space")
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			result, err := client.ListContent("page", spaceKey, "trashed", limit, start)
			if err != nil {
				return err
			}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "trash.list", map[string]any{"space": spaceKey}, result, nil, nil, "", "", "", nil))
			}
			fmt.Println("Listed trashed Confluence pages.")
			return nil
		},
	}
	cmd.Flags().String("space", "", "Optional space key filter")
	cmd.Flags().Int("limit", 25, "Maximum trashed pages to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newTrashRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <content-id>",
		Short: "Restore a trashed Confluence content item",
		Args:  cobra.ExactArgs(1),
		RunE:  runTrashRestore,
	}
	cmd.Flags().Int("version", 0, "Version number to restore from (defaults to latest available)")
	cmd.Flags().String("message", "", "Optional restore message")
	cmd.Flags().Bool("restore-title", true, "Restore the historical title when supported")
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func newTrashPurgeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "purge <content-id>",
		Short: "Permanently remove a trashed Confluence content item",
		Args:  cobra.ExactArgs(1),
		RunE:  runTrashPurge,
	}
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm permanently deleting the trashed content")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runTrashRestore(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	contentID := args[0]
	versionNumber, _ := cmd.Flags().GetInt("version")
	message, _ := cmd.Flags().GetString("message")
	restoreTitle, _ := cmd.Flags().GetBool("restore-title")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if versionNumber <= 0 {
		versions, err := client.ListContentVersions(contentID, 1, 0)
		if err != nil {
			return err
		}
		items := extractResults(versions)
		if len(items) == 0 {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "No historical versions were found for the trashed content.", ExitCode: 1}
		}
		versionNumber = intFromAny(items[0]["number"], 0)
		if versionNumber <= 0 {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Could not determine a restore version number.", ExitCode: 1}
		}
	}
	target := map[string]any{"content_id": contentID, "version": versionNumber}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "trash.restore", target, map[string]any{"dry_run": true}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "trash.restore", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	result, err := client.RestoreTrashedContent(contentID, versionNumber, message, restoreTitle)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.trash.restore %s", contentID))
	}
	return output.PrintJSON(output.BuildEnvelope(true, "confluence", "trash.restore", target, result, nil, nil, "", "", "", nil))
}

func runTrashPurge(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	contentID := args[0]
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	target := map[string]any{"content_id": contentID}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "trash.purge", target, map[string]any{"dry_run": true, "purged": false}, nil, nil, "", "", "", nil))
	}
	if !yes {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Refusing to purge trashed content without --yes.", ExitCode: 2}
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "trash.purge", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	if err := client.DeleteContent(contentID); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.trash.purge %s", contentID))
	}
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "trash.purge", target, map[string]any{"purged": true}, nil, nil, "", "", "", nil))
	}
	fmt.Printf("Purged trashed content %s.\n", contentID)
	return nil
}
