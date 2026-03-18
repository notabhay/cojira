package jira

import (
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/spf13/cobra"
)

// NewCloneCmd creates the "clone" subcommand.
func NewCloneCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clone <issue>",
		Short: "Clone an existing Jira issue into a new one",
		Args:  cobra.ExactArgs(1),
		RunE:  runClone,
	}
	cmd.Flags().String("clone-mode", "portable", "Clone mode: portable or full")
	cmd.Flags().StringArray("include-field", nil, "Include additional field(s) from the source issue")
	cmd.Flags().StringArray("exclude-field", nil, "Exclude field(s) from the cloned payload")
	cmd.Flags().String("project", "", "Project key override")
	cmd.Flags().String("type", "", "Issue type name override")
	cmd.Flags().String("issue-type", "", "Alias for --type")
	cmd.Flags().String("summary", "", "Issue summary override")
	cmd.Flags().String("description", "", "Issue description override")
	cmd.Flags().String("description-file", "", "Read description override from a text file")
	cmd.Flags().String("priority", "", "Priority override")
	cmd.Flags().String("parent", "", "Parent issue key override for sub-tasks")
	cmd.Flags().String("assignee", "", "Assignee override: accountId, accountId:xxx, name:xxx, or null")
	cmd.Flags().StringArray("component", nil, "Component override by name (repeatable)")
	cmd.Flags().StringArray("label", nil, "Label override (repeatable)")
	cmd.Flags().StringArray("set", nil, "Shorthand field override (repeatable): field=value, field:=<json>, labels+=x, labels-=x")
	cmd.Flags().Bool("no-notify", false, "Disable notifications")
	cmd.Flags().String("emit", "", "Emit a scalar result instead of the normal output: key, id, url, receipt")
	cmd.Flags().Bool("dry-run", false, "Preview without creating")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runClone(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	cli.ApplyPlanFlag(cmd)
	if mode == "key" && !cli.SupportsKeyOutput(cmd) {
		return cli.KeyModeUnsupportedError(cmd)
	}
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	typeFlag, _ := cmd.Flags().GetString("type")
	typeAliasFlag, _ := cmd.Flags().GetString("issue-type")
	typeName := strings.TrimSpace(typeFlag)
	if typeName == "" {
		typeName = strings.TrimSpace(typeAliasFlag)
	}
	cloneMode, _ := cmd.Flags().GetString("clone-mode")
	includeFields, _ := cmd.Flags().GetStringArray("include-field")
	excludeFields, _ := cmd.Flags().GetStringArray("exclude-field")
	projectFlag, _ := cmd.Flags().GetString("project")
	summaryFlag, _ := cmd.Flags().GetString("summary")
	descriptionFlag, _ := cmd.Flags().GetString("description")
	descriptionFile, _ := cmd.Flags().GetString("description-file")
	priorityFlag, _ := cmd.Flags().GetString("priority")
	parentFlag, _ := cmd.Flags().GetString("parent")
	assigneeFlag, _ := cmd.Flags().GetString("assignee")
	componentsFlag, _ := cmd.Flags().GetStringArray("component")
	labelsFlag, _ := cmd.Flags().GetStringArray("label")
	setExprs, _ := cmd.Flags().GetStringArray("set")
	noNotify, _ := cmd.Flags().GetBool("no-notify")
	emitFlag, _ := cmd.Flags().GetString("emit")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	resolution, err := resolveCreatePayload(client, createInput{
		CloneIssue:      args[0],
		CloneMode:       cloneMode,
		IncludeFields:   includeFields,
		ExcludeFields:   excludeFields,
		Project:         projectFlag,
		IssueType:       typeName,
		Summary:         summaryFlag,
		Description:     descriptionFlag,
		DescriptionFile: descriptionFile,
		Priority:        priorityFlag,
		Parent:          parentFlag,
		Assignee:        assigneeFlag,
		Components:      componentsFlag,
		Labels:          labelsFlag,
		SetExprs:        setExprs,
	})
	if err != nil {
		return err
	}

	return executeResolvedCreate(cmd, mode, client, resolution, noNotify, dryRun, idemKey, emitFlag)
}
