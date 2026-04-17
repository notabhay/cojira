package jira

import (
	"bytes"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

var branchIssueRe = regexp.MustCompile(`[A-Za-z][A-Za-z0-9_]+-\d+`)

// NewCurrentCmd creates the "current" command.
func NewCurrentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "current",
		Short: "Detect the current Jira issue from the active git branch",
		Args:  cobra.NoArgs,
		RunE:  runCurrent,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

// NewBranchCmd creates the "branch" command.
func NewBranchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "branch <issue>",
		Short: "Create or print a git branch name for a Jira issue",
		Args:  cobra.ExactArgs(1),
		RunE:  runBranch,
	}
	cmd.Flags().String("from", "", "Starting ref for the new branch")
	cmd.Flags().Bool("print-only", false, "Print the resolved branch name without creating it")
	cmd.Flags().Bool("plan", false, "Alias for --print-only")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

// NewCommitTemplateCmd creates the "commit-template" command.
func NewCommitTemplateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "commit-template [issue]",
		Short: "Print a git commit title template for a Jira issue",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runCommitTemplate,
	}
	cmd.Flags().Bool("summary", true, "Include the issue summary when available")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

// NewPRTitleCmd creates the "pr-title" command.
func NewPRTitleCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "pr-title [issue]",
		Short: "Print a pull request title for a Jira issue",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runPRTitle,
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

// NewFinishBranchCmd creates the "finish-branch" command.
func NewFinishBranchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "finish-branch",
		Short: "Switch back to a base branch and optionally delete the current issue branch",
		Args:  cobra.NoArgs,
		RunE:  runFinishBranch,
	}
	cmd.Flags().String("base", "main", "Base branch to switch to before finishing")
	cmd.Flags().Bool("delete", false, "Delete the finished branch after switching away from it")
	cmd.Flags().String("transition-to", "", "Optional Jira transition to apply to the inferred issue")
	cmd.Flags().Bool("plan", false, "Preview the actions without applying")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runCurrent(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	branch, err := gitCurrentBranch()
	if err != nil {
		return err
	}
	issue := detectIssueFromBranch(branch)
	result := map[string]any{"branch": branch, "issue": issue}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "current", nil, result, nil, nil, "", "", "", nil))
	}
	if issue == "" {
		if mode == "summary" {
			fmt.Println("No Jira issue detected in the current branch.")
			return nil
		}
		fmt.Printf("Current branch: %s\nNo Jira issue detected.\n", branch)
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Current branch %s maps to %s.\n", branch, issue)
		return nil
	}
	fmt.Printf("Current branch: %s\nIssue: %s\n", branch, issue)
	return nil
}

func runBranch(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	issueID := ResolveIssueIdentifier(args[0])
	printOnly, _ := cmd.Flags().GetBool("print-only")
	plan, _ := cmd.Flags().GetBool("plan")
	fromRef, _ := cmd.Flags().GetString("from")
	if plan {
		printOnly = true
	}

	issue, err := client.GetIssue(issueID, "summary", "")
	if err != nil {
		return err
	}
	recordSearchRecents(client, []map[string]any{issue}, "branch")
	fields, _ := issue["fields"].(map[string]any)
	summary := normalizeMaybeString(fields["summary"])
	branchName := renderBranchTemplate(gitBranchTemplateFromConfig(), issueID, summary)

	if printOnly {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "branch", map[string]any{"issue": issueID}, map[string]any{"branch": branchName, "from": fromRef, "dry_run": true}, nil, nil, "", "", "", nil))
		}
		fmt.Println(branchName)
		return nil
	}
	if err := gitCreateBranch(branchName, fromRef); err != nil {
		return err
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "branch", map[string]any{"issue": issueID}, map[string]any{"branch": branchName, "from": fromRef}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Created branch %s.\n", branchName)
		return nil
	}
	fmt.Printf("Created branch %s.\n", branchName)
	return nil
}

func runCommitTemplate(cmd *cobra.Command, args []string) error {
	return runIssueTitleTemplate(cmd, args, "commit-template")
}

func runPRTitle(cmd *cobra.Command, args []string) error {
	return runIssueTitleTemplate(cmd, args, "pr-title")
}

func runIssueTitleTemplate(cmd *cobra.Command, args []string, command string) error {
	mode := cli.NormalizeOutputMode(cmd)
	issueID, branch, err := resolveCurrentOrExplicitIssue(args)
	if err != nil {
		return err
	}
	includeSummary, _ := cmd.Flags().GetBool("summary")
	title := issueID
	if includeSummary {
		client, err := clientFromCmd(cmd)
		if err == nil {
			issue, fetchErr := client.GetIssue(issueID, "summary", "")
			if fetchErr == nil {
				recordSearchRecents(client, []map[string]any{issue}, command)
				fields, _ := issue["fields"].(map[string]any)
				if summary := normalizeMaybeString(fields["summary"]); summary != "" {
					title = fmt.Sprintf("%s: %s", issueID, summary)
				}
			}
		}
	}
	result := map[string]any{"issue": issueID, "title": title, "branch": branch}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", command, nil, result, nil, nil, "", "", "", nil))
	}
	fmt.Println(title)
	return nil
}

func runFinishBranch(cmd *cobra.Command, _ []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	branch, err := gitCurrentBranch()
	if err != nil {
		return err
	}
	issue := detectIssueFromBranch(branch)
	base, _ := cmd.Flags().GetString("base")
	deleteBranch, _ := cmd.Flags().GetBool("delete")
	transitionTo, _ := cmd.Flags().GetString("transition-to")
	plan, _ := cmd.Flags().GetBool("plan")

	result := map[string]any{
		"branch":        branch,
		"issue":         issue,
		"base":          base,
		"delete":        deleteBranch,
		"transition_to": transitionTo,
		"dry_run":       plan,
	}
	if plan {
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(true, "jira", "finish-branch", nil, result, nil, nil, "", "", "", nil))
		}
		fmt.Printf("Would switch from %s to %s.\n", branch, base)
		if deleteBranch {
			fmt.Printf("Would delete branch %s.\n", branch)
		}
		if issue != "" && transitionTo != "" {
			fmt.Printf("Would transition %s to %s.\n", issue, transitionTo)
		}
		return nil
	}

	if err := gitSwitchBranch(base); err != nil {
		return err
	}
	if deleteBranch {
		if err := gitDeleteBranch(branch); err != nil {
			return err
		}
	}
	if issue != "" && strings.TrimSpace(transitionTo) != "" {
		client, err := clientFromCmd(cmd)
		if err != nil {
			return err
		}
		if err := transitionIssueByName(client, issue, transitionTo); err != nil {
			return err
		}
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "jira", "finish-branch", nil, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Finished branch %s.\n", branch)
		return nil
	}
	fmt.Printf("Finished branch %s.\n", branch)
	return nil
}

func resolveCurrentOrExplicitIssue(args []string) (string, string, error) {
	if len(args) > 0 && strings.TrimSpace(args[0]) != "" {
		return ResolveIssueIdentifier(args[0]), "", nil
	}
	branch, err := gitCurrentBranch()
	if err != nil {
		return "", "", err
	}
	issue := detectIssueFromBranch(branch)
	if issue == "" {
		return "", branch, &cerrors.CojiraError{Code: cerrors.IdentUnresolved, Message: "No Jira issue found in the current branch.", ExitCode: 1}
	}
	return issue, branch, nil
}

func gitCurrentBranch() (string, error) {
	out, err := gitOutput("rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Failed to detect current git branch: %v", err), ExitCode: 1}
	}
	return strings.TrimSpace(out), nil
}

func gitOutput(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("%s", msg)
	}
	return strings.TrimSpace(stdout.String()), nil
}

func gitCreateBranch(branchName, fromRef string) error {
	args := []string{"switch", "-c", branchName}
	if strings.TrimSpace(fromRef) != "" {
		args = append(args, fromRef)
	}
	if _, err := gitOutput(args...); err != nil {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Failed to create branch %s: %v", branchName, err), ExitCode: 1}
	}
	return nil
}

func gitSwitchBranch(branch string) error {
	if _, err := gitOutput("switch", branch); err != nil {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Failed to switch to branch %s: %v", branch, err), ExitCode: 1}
	}
	return nil
}

func gitDeleteBranch(branch string) error {
	if _, err := gitOutput("branch", "-d", branch); err != nil {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: fmt.Sprintf("Failed to delete branch %s: %v", branch, err), ExitCode: 1}
	}
	return nil
}

func detectIssueFromBranch(branch string) string {
	return branchIssueRe.FindString(branch)
}

func renderBranchTemplate(template, issue, summary string) string {
	slug := slugify(summary)
	replacer := strings.NewReplacer(
		"{issue}", issue,
		"{key}", issue,
		"{slug}", slug,
	)
	name := replacer.Replace(template)
	name = strings.TrimSpace(strings.Trim(name, "/"))
	name = strings.ReplaceAll(name, "//", "/")
	if name == "" {
		return issue
	}
	return name
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	value = strings.ReplaceAll(value, "_", "-")
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			lastDash = false
			continue
		}
		if !lastDash {
			b.WriteByte('-')
			lastDash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

func transitionIssueByName(client *Client, issue, status string) error {
	data, err := client.ListTransitions(issue)
	if err != nil {
		return err
	}
	transitions, _ := data["transitions"].([]any)
	matches := filterTransitionsByStatus(transitions, status)
	if len(matches) == 0 {
		return &cerrors.CojiraError{Code: cerrors.TransitionNotFound, Message: fmt.Sprintf("No transition to %s found for %s.", status, issue), ExitCode: 1}
	}
	selected, _ := matches[0].(map[string]any)
	payload := map[string]any{"transition": map[string]any{"id": selected["id"]}}
	return client.TransitionIssue(issue, payload, true)
}
