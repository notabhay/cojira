package confluence

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCommentCmd creates the "comment" subcommand.
func NewCommentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "comment <page>",
		Short: "List or add Confluence comments",
		Args:  cobra.ExactArgs(1),
		RunE:  runComment,
	}
	cmd.Flags().String("add", "", "Comment body to add (storage XHTML)")
	cmd.Flags().String("file", "", "Read comment body from a file")
	cmd.Flags().String("format", "storage", "Comment body format: storage or markdown")
	cmd.Flags().Bool("all", false, "Fetch all comments")
	cmd.Flags().Int("limit", 20, "Maximum comments to fetch")
	cmd.Flags().Int("start", 0, "Start offset for comment listing")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cmd.Flags().Bool("dry-run", false, "Preview comment creation without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runComment(cmd *cobra.Command, args []string) error {
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

	addBody, _ := cmd.Flags().GetString("add")
	bodyFile, _ := cmd.Flags().GetString("file")
	format, _ := cmd.Flags().GetString("format")
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if addBody != "" || bodyFile != "" {
		if addBody != "" && bodyFile != "" {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --add or --file, not both.", ExitCode: 2}
		}
		if bodyFile != "" {
			content, err := readTextFile(bodyFile)
			if err != nil {
				return err
			}
			addBody = content
		}
		bodyText := strings.TrimSpace(addBody)
		if bodyText == "" {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Comment body cannot be empty.", ExitCode: 2}
		}
		bodyText, err = convertStorageBody(bodyText, format)
		if err != nil {
			return err
		}
		target := map[string]any{"page": pageArg, "page_id": pageID}
		if dryRun {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "comment", target, map[string]any{"dry_run": true, "body": bodyText}, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Would add a comment to page %s.\n", pageID)
				return nil
			}
			fmt.Printf("Would add a comment to page %s.\n", pageID)
			return nil
		}
		if idemKey != "" && idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "comment", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
			}
			fmt.Printf("Skipped duplicate comment add for %s.\n", pageID)
			return nil
		}
		comment, err := client.AddPageComment(pageID, bodyText)
		if err != nil {
			return err
		}
		if idemKey != "" {
			_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.comment %s", pageID))
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "confluence", "comment", target, map[string]any{"comment": comment}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Added a comment to page %s.\n", pageID)
			return nil
		}
		fmt.Printf("Added comment %v to page %s.\n", comment["id"], pageID)
		return nil
	}

	items := make([]map[string]any, 0)
	total := 0
	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.ListPageComments(pageID, pageSize, offset)
			if err != nil {
				return err
			}
			pageItems := extractResults(data)
			total = intFromAny(data["size"], total)
			items = append(items, pageItems...)
			offset += len(pageItems)
			if len(pageItems) == 0 {
				break
			}
		}
	} else {
		data, err := client.ListPageComments(pageID, limit, start)
		if err != nil {
			return err
		}
		items = extractResults(data)
		total = intFromAny(data["size"], len(items))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "comment", map[string]any{"page": pageArg, "page_id": pageID}, map[string]any{"comments": items, "summary": map[string]any{"count": len(items), "total": total}}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Found %d comment(s) on page %s.\n", len(items), pageID)
		return nil
	}
	if len(items) == 0 {
		fmt.Println("No comments found.")
		return nil
	}
	fmt.Printf("Comments on %s:\n\n", pageID)
	for _, item := range items {
		body := getNestedString(item, "body", "storage", "value")
		author := getNestedString(item, "history", "createdBy", "displayName")
		fmt.Printf("[%v] %s | %v\n%s\n\n", item["id"], author, getNestedString(item, "history", "createdDate"), body)
	}
	return nil
}
