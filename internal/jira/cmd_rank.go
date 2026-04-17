package jira

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewRankCmd creates the "rank" subcommand.
func NewRankCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "rank <issue> [issue...]",
		Short: "Rank one or more issues before or after another issue",
		Args:  cobra.MinimumNArgs(1),
		RunE:  runRank,
	}
	cmd.Flags().String("before", "", "Rank these issues before another issue")
	cmd.Flags().String("after", "", "Rank these issues after another issue")
	cmd.Flags().String("rank-field", "", "Optional Rank custom field id (for example: customfield_12345)")
	cmd.Flags().Bool("dry-run", false, "Preview the ranking without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runRank(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)

	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	before, _ := cmd.Flags().GetString("before")
	after, _ := cmd.Flags().GetString("after")
	rankFieldFlag, _ := cmd.Flags().GetString("rank-field")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if strings.TrimSpace(before) == "" && strings.TrimSpace(after) == "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Provide --before or --after.", ExitCode: 2}
	}
	if strings.TrimSpace(before) != "" && strings.TrimSpace(after) != "" {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use only one of --before or --after.", ExitCode: 2}
	}

	issues := make([]string, 0, len(args))
	for _, arg := range args {
		issues = append(issues, ResolveIssueIdentifier(arg))
	}
	if strings.TrimSpace(before) != "" {
		before = ResolveIssueIdentifier(before)
	}
	if strings.TrimSpace(after) != "" {
		after = ResolveIssueIdentifier(after)
	}

	rankFieldID, err := resolveRankCustomFieldID(client, issues[0], rankFieldFlag)
	if err != nil {
		return err
	}

	target := map[string]any{"issues": issues}
	result := map[string]any{"issues": issues}
	if before != "" {
		target["before"] = before
		result["rank_before_issue"] = before
	}
	if after != "" {
		target["after"] = after
		result["rank_after_issue"] = after
	}
	if rankFieldID > 0 {
		target["rank_field_id"] = rankFieldID
		result["rank_custom_field_id"] = rankFieldID
	}

	if dryRun {
		result["dry_run"] = true
		return printRankResult(mode, target, result)
	}

	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return printRankResult(mode, target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"})
	}

	if err := client.RankIssues(issues, before, after, rankFieldID); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("jira.rank %s", strings.Join(issues, ",")))
	}
	result["updated"] = true
	return printRankResult(mode, target, result)
}

func printRankResult(mode string, target, result map[string]any) error {
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "rank", target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		if result["dry_run"] == true {
			fmt.Printf("Would rank %d issue(s).\n", len(coerceIssueList(result["issues"])))
			return nil
		}
		if result["skipped"] == true {
			fmt.Println("Skipped duplicate rank request.")
			return nil
		}
		fmt.Printf("Ranked %d issue(s).\n", len(coerceIssueList(result["issues"])))
		return nil
	}
	if result["dry_run"] == true {
		fmt.Printf("Would rank issues: %s\n", strings.Join(coerceIssueList(result["issues"]), ", "))
		return nil
	}
	if result["skipped"] == true {
		fmt.Println("Skipped duplicate rank request.")
		return nil
	}
	fmt.Printf("Ranked issues: %s\n", strings.Join(coerceIssueList(result["issues"]), ", "))
	return nil
}

func resolveRankCustomFieldID(client *Client, issueID, explicit string) (int, error) {
	if normalized, ok := parseRankCustomFieldID(explicit); ok {
		return normalized, nil
	}
	if client == nil || issueID == "" {
		return 0, nil
	}
	resolved, err := newIssueFieldResolver(client, issueID).Resolve("Rank")
	if err != nil {
		return 0, nil
	}
	if normalized, ok := parseRankCustomFieldID(resolved); ok {
		return normalized, nil
	}
	return 0, nil
}

func parseRankCustomFieldID(value string) (int, bool) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return 0, false
	}
	if strings.HasPrefix(strings.ToLower(trimmed), "customfield_") {
		trimmed = strings.TrimPrefix(strings.ToLower(trimmed), "customfield_")
	}
	id, err := strconv.Atoi(trimmed)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

func coerceIssueList(raw any) []string {
	switch typed := raw.(type) {
	case []string:
		return typed
	case []any:
		values := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := normalizeMaybeString(item); text != "" {
				values = append(values, text)
			}
		}
		return values
	default:
		return nil
	}
}
