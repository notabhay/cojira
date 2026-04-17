package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewLinkCmd creates the "link" subcommand.
func NewLinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "link [issue] [target]",
		Short: "Create, list, delete, or inspect Jira issue links",
		Args:  cobra.RangeArgs(0, 2),
		RunE:  runLink,
	}
	cmd.Flags().Bool("list", false, "List links on an issue")
	cmd.Flags().Bool("types", false, "List available issue link types")
	cmd.Flags().String("delete", "", "Link ID to delete from an issue")
	cmd.Flags().String("type", "Relates", "Link type name (for example: Relates, Blocks, Duplicates)")
	cmd.Flags().String("comment", "", "Optional comment to include with the link")
	cmd.Flags().String("comment-file", "", "Read the optional comment from a file")
	cmd.Flags().Bool("dry-run", false, "Preview the link without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runLink(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	listFlag, _ := cmd.Flags().GetBool("list")
	typesFlag, _ := cmd.Flags().GetBool("types")
	deleteID, _ := cmd.Flags().GetString("delete")
	linkType, _ := cmd.Flags().GetString("type")
	commentText, _ := cmd.Flags().GetString("comment")
	commentFile, _ := cmd.Flags().GetString("comment-file")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	actions := 0
	if listFlag {
		actions++
	}
	if typesFlag {
		actions++
	}
	if strings.TrimSpace(deleteID) != "" {
		actions++
	}
	if len(args) == 2 {
		actions++
	}
	if actions == 0 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide an issue to list, --types, --delete <id>, or <source> <target> to create a link.", ExitCode: 2}
	}
	if actions > 1 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Choose only one of list, types, delete, or create link actions.", ExitCode: 2}
	}

	if typesFlag {
		return runLinkTypes(client, mode)
	}

	if listFlag {
		if len(args) != 1 {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Listing links requires exactly one issue argument.", ExitCode: 2}
		}
		return runLinkList(client, ResolveIssueIdentifier(args[0]), mode)
	}

	if deleteID != "" {
		if len(args) != 1 {
			return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Deleting a link requires exactly one issue argument.", ExitCode: 2}
		}
		return runLinkDelete(cmd, client, ResolveIssueIdentifier(args[0]), deleteID, dryRun, idemKey, mode)
	}

	if len(args) != 2 {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Creating a link requires source and target issue arguments.", ExitCode: 2}
	}

	source := ResolveIssueIdentifier(args[0])
	targetIssue := ResolveIssueIdentifier(args[1])

	if commentText != "" && commentFile != "" {
		return fmt.Errorf("use either --comment or --comment-file, not both")
	}
	if commentFile != "" {
		content, err := readTextFile(commentFile)
		if err != nil {
			return err
		}
		commentText = strings.TrimSpace(content)
	}

	payload := map[string]any{
		"type":         map[string]any{"name": linkType},
		"outwardIssue": map[string]any{"key": source},
		"inwardIssue":  map[string]any{"key": targetIssue},
	}
	if commentText != "" {
		payload["comment"] = map[string]any{"body": commentText}
	}

	target := map[string]any{"source": source, "target": targetIssue}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "link",
				target,
				map[string]any{"dry_run": true, "payload": payload},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Would link %s to %s using %q.\n", source, targetIssue, linkType)
			return nil
		}
		fmt.Printf("Would link %s to %s using %q.\n", source, targetIssue, linkType)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "link",
				target,
				map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
				nil, nil, "", "", "", nil,
			))
		}
		fmt.Printf("Skipped duplicate link request for %s and %s.\n", source, targetIssue)
		return nil
	}

	if err := client.CreateIssueLink(payload); err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.link %s %s", source, targetIssue))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "link",
			target,
			map[string]any{"linked": true, "type": linkType},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Linked %s to %s using %q.\n", source, targetIssue, linkType)
		return nil
	}
	fmt.Printf("Linked %s to %s using %q.\n", source, targetIssue, linkType)
	return nil
}

func runLinkList(client *Client, issueID, mode string) error {
	issue, err := client.GetIssue(issueID, "issuelinks", "")
	if err != nil {
		return err
	}

	fields, _ := issue["fields"].(map[string]any)
	raw, _ := fields["issuelinks"].([]any)
	links := coerceJSONArray(raw)
	items := summarizeIssueLinks(links)

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "link", map[string]any{"issue": issueID}, map[string]any{"links": items, "summary": map[string]any{"count": len(items)}}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Found %d link(s) on %s.\n", len(items), issueID)
		return nil
	}
	if len(items) == 0 {
		fmt.Println("No links found.")
		return nil
	}
	fmt.Printf("Links for %s:\n\n", issueID)
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			normalizeMaybeString(item["id"]),
			strings.ToUpper(normalizeMaybeString(item["direction"])),
			output.Truncate(normalizeMaybeString(item["type"]), 18),
			normalizeMaybeString(item["issue"]),
			output.StatusBadge(normalizeMaybeString(item["status"])),
			output.Truncate(normalizeMaybeString(item["summary"]), 52),
		})
	}
	fmt.Println(output.TableString([]string{"ID", "DIR", "TYPE", "ISSUE", "STATUS", "SUMMARY"}, rows))
	return nil
}

func runLinkTypes(client *Client, mode string) error {
	items, err := client.ListIssueLinkTypes()
	if err != nil {
		return err
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "link", map[string]any{}, map[string]any{"types": items, "summary": map[string]any{"count": len(items)}}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Found %d link type(s).\n", len(items))
		return nil
	}
	if len(items) == 0 {
		fmt.Println("No link types found.")
		return nil
	}
	fmt.Println("Link types:")
	fmt.Println()
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			output.Truncate(normalizeMaybeString(item["name"]), 24),
			output.Truncate(normalizeMaybeString(item["outward"]), 32),
			output.Truncate(normalizeMaybeString(item["inward"]), 32),
		})
	}
	fmt.Println(output.TableString([]string{"NAME", "OUTWARD", "INWARD"}, rows))
	return nil
}

func runLinkDelete(cmd *cobra.Command, client *Client, issueID, deleteID string, dryRun bool, idemKey, mode string) error {
	issue, err := client.GetIssue(issueID, "issuelinks", "")
	if err != nil {
		return err
	}
	fields, _ := issue["fields"].(map[string]any)
	raw, _ := fields["issuelinks"].([]any)
	links := summarizeIssueLinks(coerceJSONArray(raw))

	var selected map[string]any
	for _, item := range links {
		if normalizeMaybeString(item["id"]) == deleteID {
			selected = item
			break
		}
	}
	if selected == nil {
		return &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: fmt.Sprintf("Link %s was not found on %s.", deleteID, issueID), ExitCode: 1}
	}

	target := map[string]any{"issue": issueID, "link_id": deleteID}

	if dryRun {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "link", target, map[string]any{"dry_run": true, "deleted": false, "link": selected}, nil, nil, "", "", "", nil))
		}
		if mode == "summary" {
			fmt.Printf("Would delete link %s on %s.\n", deleteID, issueID)
			return nil
		}
		fmt.Printf("Would delete link %s on %s.\n", deleteID, issueID)
		return nil
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "link", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Skipped duplicate link delete for %s.\n", issueID)
		return nil
	}

	if err := client.DeleteIssueLink(deleteID); err != nil {
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.link delete %s %s", issueID, deleteID))
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "link", target, map[string]any{"deleted": true, "link": selected}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Deleted link %s on %s.\n", deleteID, issueID)
		return nil
	}
	fmt.Printf("Deleted link %s on %s.\n", deleteID, issueID)
	return nil
}

func summarizeIssueLinks(links []map[string]any) []map[string]any {
	items := make([]map[string]any, 0, len(links))
	for _, link := range links {
		item := map[string]any{
			"id":   link["id"],
			"type": safeString(link, "type", "name"),
		}
		switch {
		case link["outwardIssue"] != nil:
			outward, _ := link["outwardIssue"].(map[string]any)
			item["direction"] = "outward"
			item["issue"] = outward["key"]
			item["summary"] = safeString(outward, "fields", "summary")
			item["status"] = safeString(outward, "fields", "status", "name")
		case link["inwardIssue"] != nil:
			inward, _ := link["inwardIssue"].(map[string]any)
			item["direction"] = "inward"
			item["issue"] = inward["key"]
			item["summary"] = safeString(inward, "fields", "summary")
			item["status"] = safeString(inward, "fields", "status", "name")
		default:
			item["direction"] = "unknown"
		}
		items = append(items, item)
	}
	return items
}
