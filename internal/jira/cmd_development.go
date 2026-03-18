package jira

import (
	"fmt"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewDevelopmentCmd creates the experimental Jira development-data command group.
func NewDevelopmentCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "development",
		Short: "EXPERIMENTAL: Inspect Jira Development-tab data",
		Long:  "EXPERIMENTAL: Read Jira Development-tab data such as pull requests, commits, branches, builds, reviews, and deployments.",
	}
	cmd.AddCommand(
		newDevelopmentSummaryCmd(),
		newDevelopmentDetailCmd(),
		newDevelopmentTypedCmd("pull-requests", "pullrequest", "EXPERIMENTAL: Show linked pull request details"),
		newDevelopmentTypedCmd("commits", "repository", "EXPERIMENTAL: Show linked repository/commit details"),
		newDevelopmentTypedCmd("branches", "branch", "EXPERIMENTAL: Show linked branch details"),
		newDevelopmentTypedCmd("builds", "build", "EXPERIMENTAL: Show linked build details"),
		newDevelopmentTypedCmd("reviews", "review", "EXPERIMENTAL: Show linked review details"),
		newDevelopmentTypedCmd("deployments", "deployment-environment", "EXPERIMENTAL: Show linked deployment details"),
	)
	return cmd
}

func newDevelopmentSummaryCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "summary <issue>",
		Short: "EXPERIMENTAL: Show Development-tab summary counts",
		Args:  cobra.ExactArgs(1),
		RunE:  runDevelopmentSummary,
	}
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newDevelopmentDetailCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "detail <issue>",
		Short: "EXPERIMENTAL: Show raw Development-tab detail data",
		Args:  cobra.ExactArgs(1),
		RunE:  runDevelopmentDetail,
	}
	cmd.Flags().String("application-type", "", "Provider instance type to inspect (e.g. stash)")
	cmd.Flags().String("data-type", "", "Development data type (pullrequest, repository, branch, build, review, deployment-environment)")
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func newDevelopmentTypedCmd(name string, dataType string, short string) *cobra.Command {
	cmd := &cobra.Command{
		Use:   name + " <issue>",
		Short: short,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runTypedDevelopmentDetail(cmd, args[0], dataType)
		},
	}
	cmd.Flags().String("application-type", "", "Provider instance type to inspect (e.g. stash)")
	cmd.Flags().StringP("output", "o", "", "Output file (default: stdout)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func requireExperimentalJira(cmd *cobra.Command) error {
	experimental, _ := cmd.Flags().GetBool("experimental")
	if experimental {
		return nil
	}
	return &cerrors.CojiraError{
		Code:     cerrors.Unsupported,
		Message:  "This command is experimental. Re-run with `cojira jira --experimental ...`.",
		ExitCode: 2,
	}
}

func runDevelopmentSummary(cmd *cobra.Command, args []string) error {
	if err := requireExperimentalJira(cmd); err != nil {
		return err
	}
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	outputFile, _ := cmd.Flags().GetString("output")

	issueRef, apiBase, summary, err := fetchDevelopmentSummary(client, args[0])
	if err != nil {
		return developmentCommandError(mode, "summary", args[0], err)
	}
	result := map[string]any{
		"schema":   "jira.development.summary/v1",
		"issue":    issueRef,
		"api_base": apiBase,
		"summary":  summary,
		"counts":   developmentCounts(summary),
	}
	return emitDevelopmentResult(cmd, mode, "summary", args[0], outputFile, result)
}

func runDevelopmentDetail(cmd *cobra.Command, args []string) error {
	if err := requireExperimentalJira(cmd); err != nil {
		return err
	}
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	outputFile, _ := cmd.Flags().GetString("output")
	applicationType, _ := cmd.Flags().GetString("application-type")
	dataType, _ := cmd.Flags().GetString("data-type")

	issueRef, apiBase, summary, err := fetchDevelopmentSummary(client, args[0])
	if err != nil {
		return developmentCommandError(mode, "detail", args[0], err)
	}

	var selections []developmentSelection
	if strings.TrimSpace(dataType) != "" {
		selections = developmentSelectionsFor(summary, strings.TrimSpace(dataType), applicationType)
		if len(selections) == 0 && strings.TrimSpace(applicationType) != "" {
			selections = []developmentSelection{{ApplicationType: strings.TrimSpace(applicationType), DataType: strings.TrimSpace(dataType)}}
		}
	} else {
		for _, candidate := range []string{"pullrequest", "repository", "branch", "build", "review", "deployment-environment"} {
			selections = append(selections, developmentSelectionsFor(summary, candidate, applicationType)...)
		}
	}

	details, failures := collectDevelopmentDetails(client, issueRef, apiBase, selections)
	result := map[string]any{
		"schema":          "jira.development.detail/v1",
		"issue":           issueRef,
		"api_base":        apiBase,
		"summary":         summary,
		"counts":          developmentCounts(summary),
		"details":         details,
		"failed_requests": failures,
	}
	return emitDevelopmentResult(cmd, mode, "detail", args[0], outputFile, result)
}

func runTypedDevelopmentDetail(cmd *cobra.Command, issue string, dataType string) error {
	if err := requireExperimentalJira(cmd); err != nil {
		return err
	}
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	outputFile, _ := cmd.Flags().GetString("output")
	applicationType, _ := cmd.Flags().GetString("application-type")

	issueRef, apiBase, summary, err := fetchDevelopmentSummary(client, issue)
	if err != nil {
		return developmentCommandError(mode, dataType, issue, err)
	}
	selections := developmentSelectionsFor(summary, dataType, applicationType)
	details, failures := collectDevelopmentDetails(client, issueRef, apiBase, selections)
	result := map[string]any{
		"schema":          "jira.development." + strings.ReplaceAll(dataType, "-", "_") + "/v1",
		"issue":           issueRef,
		"api_base":        apiBase,
		"summary":         summary,
		"counts":          developmentCounts(summary),
		"data_type":       dataType,
		"details":         details,
		"failed_requests": failures,
	}
	return emitDevelopmentResult(cmd, mode, dataType, issue, outputFile, result)
}

func collectDevelopmentDetails(client *Client, issueRef developmentIssueRef, apiBase string, selections []developmentSelection) ([]map[string]any, []map[string]any) {
	details := []map[string]any{}
	failures := []map[string]any{}
	for _, selection := range selections {
		payload, err := fetchDevelopmentDetail(client, apiBase, issueRef.IssueID, selection)
		if err != nil {
			failures = append(failures, map[string]any{
				"application_type": selection.ApplicationType,
				"data_type":        selection.DataType,
				"error":            err.Error(),
			})
			continue
		}
		details = append(details, map[string]any{
			"application_type": selection.ApplicationType,
			"data_type":        selection.DataType,
			"detail":           payload,
		})
	}
	return details, failures
}

func emitDevelopmentResult(cmd *cobra.Command, mode string, command string, issue string, outputFile string, result map[string]any) error {
	if outputFile != "" {
		content, err := output.JSONDumps(result)
		if err != nil {
			return err
		}
		if err := writeFile(outputFile, content); err != nil {
			return err
		}
		if mode == "json" {
			return output.PrintJSON(output.BuildEnvelope(
				true, "jira", "development "+command,
				map[string]any{"issue": issue},
				map[string]any{"schema": result["schema"], "saved_to": outputFile},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			fmt.Printf("Saved Jira development %s for %s to %s.\n", command, ResolveIssueIdentifier(issue), outputFile)
			return nil
		}
		fmt.Printf("Saved Jira development %s to: %s\n", command, outputFile)
		return nil
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "development "+command,
			map[string]any{"issue": issue},
			result,
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		counts, _ := result["counts"].(map[string]any)
		parts := []string{}
		for _, key := range []string{"pullrequest", "repository", "branch", "build", "review", "deployment-environment"} {
			if entry, ok := counts[key].(map[string]any); ok {
				parts = append(parts, fmt.Sprintf("%s=%d", key, intFromAny(entry["count"], 0)))
			}
		}
		fmt.Printf("%s development: %s\n", ResolveIssueIdentifier(issue), strings.Join(parts, ", "))
		return nil
	}
	fmt.Printf("Jira development %s for %s\n", command, ResolveIssueIdentifier(issue))
	if counts, ok := result["counts"].(map[string]any); ok {
		for _, key := range []string{"pullrequest", "repository", "branch", "build", "review", "deployment-environment"} {
			if entry, ok := counts[key].(map[string]any); ok {
				fmt.Printf("  %-22s %d\n", key, intFromAny(entry["count"], 0))
			}
		}
	}
	if failures, ok := result["failed_requests"].([]map[string]any); ok && len(failures) > 0 {
		fmt.Printf("Failed detail requests: %d\n", len(failures))
	}
	return nil
}

func developmentCommandError(mode string, command string, issue string, err error) error {
	if mode != "json" {
		return err
	}
	errObj, _ := output.ErrorObj(errorCode(err, cerrors.FetchFailed), err.Error(), "", "", nil)
	return output.PrintJSON(output.BuildEnvelope(
		false, "jira", "development "+command,
		map[string]any{"issue": issue},
		nil, nil, []any{errObj}, "", "", "", nil,
	))
}
