package confluence

import (
	"fmt"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewRestoreCmd creates the "restore" subcommand.
func NewRestoreCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "restore <page>",
		Short: "Restore a Confluence page from a historical version",
		Args:  cobra.ExactArgs(1),
		RunE:  runRestore,
	}
	cmd.Flags().Int("from-version", 0, "Historical version number to restore from")
	cmd.Flags().Bool("dry-run", false, "Preview the restore without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm destructive restore")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runRestore(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	defPageID := defaultPageID(cfgData)
	pageArg := args[0]
	fromVersion, _ := cmd.Flags().GetInt("from-version")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if fromVersion <= 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--from-version is required and must be > 0.", ExitCode: 2}
	}

	pageID, err := ResolvePageID(client, pageArg, defPageID)
	if err != nil {
		return err
	}

	currentPage, err := client.GetPageByID(pageID, "body.storage,version")
	if err != nil {
		return err
	}
	historicalPage, err := client.GetPageVersion(pageID, fromVersion, "body.storage,version")
	if err != nil {
		return err
	}

	currentBody := getNestedString(currentPage, "body", "storage", "value")
	restoreBody := getNestedString(historicalPage, "body", "storage", "value")
	currentTitle, _ := currentPage["title"].(string)
	restoreTitle, _ := historicalPage["title"].(string)
	currentVersion := int(getNestedFloat(currentPage, "version", "number"))
	diffText, additions, deletions := computeUnifiedDiff(currentBody, restoreBody, pageID)
	titleChanged := currentTitle != restoreTitle

	target := map[string]any{"page": pageArg, "page_id": pageID}
	result := map[string]any{
		"from_version":  fromVersion,
		"to_version":    currentVersion + 1,
		"from_title":    restoreTitle,
		"to_title":      currentTitle,
		"title_changed": titleChanged,
		"diff":          diffText,
		"summary": map[string]any{
			"additions":     additions,
			"deletions":     deletions,
			"title_changed": titleChanged,
		},
	}

	if dryRun {
		result["dry_run"] = true
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "restore", target, result, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would restore page %s from version %d.\n", pageID, fromVersion)
			return nil
		}
		fmt.Printf("Would restore page %s from version %d.\n", pageID, fromVersion)
		if titleChanged {
			fmt.Printf("Title: %q -> %q\n", currentTitle, restoreTitle)
		}
		if diffText == "" {
			fmt.Println("No content changes.")
		} else {
			fmt.Print(diffText)
			fmt.Printf("\n%d addition(s), %d deletion(s)\n", additions, deletions)
		}
		return nil
	}

	if !yes {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Restore is destructive. Preview with --dry-run first, then rerun with --yes.", ExitCode: 2}
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "restore", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate restore for %s.\n", pageID)
		return nil
	}

	payload := map[string]any{
		"type":  "page",
		"title": restoreTitle,
		"version": map[string]any{
			"number": currentVersion + 1,
		},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          restoreBody,
				"representation": "storage",
			},
		},
	}
	_, err = client.UpdatePage(pageID, payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.restore %s", pageID))
	}

	if mode == "json" {
		result["restored"] = true
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "restore", target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Restored page %s from version %d.\n", pageID, fromVersion)
		return nil
	}
	fmt.Printf("Restored page %s from version %d.\n", pageID, fromVersion)
	return nil
}
