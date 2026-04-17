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

// NewTemplatesCmd creates the "templates" command group.
func NewTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "templates",
		Aliases: []string{"template"},
		Short:   "List, fetch, create, update, or delete Confluence templates",
	}
	cmd.AddCommand(
		newTemplatesListCmd(),
		newTemplatesGetCmd(),
		newTemplatesCreateCmd(),
		newTemplatesUpdateCmd(),
		newTemplatesDeleteCmd(),
	)
	return cmd
}

func newTemplatesListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List content templates",
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			spaceKey, _ := cmd.Flags().GetString("space")
			limit, _ := cmd.Flags().GetInt("limit")
			start, _ := cmd.Flags().GetInt("start")
			result, err := client.ListTemplates(spaceKey, limit, start)
			if err != nil {
				return err
			}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.list", map[string]any{"space": spaceKey}, result, nil, nil, "", "", "", nil))
			}
			fmt.Println("Listed templates.")
			return nil
		},
	}
	cmd.Flags().String("space", "", "Optional space key to scope templates")
	cmd.Flags().Int("limit", 25, "Maximum templates to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newTemplatesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <template-id>",
		Short: "Get a template by id",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			result, err := client.GetTemplate(args[0])
			if err != nil {
				return err
			}
			if mode == "json" || mode == "ndjson" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.get", map[string]any{"template_id": args[0]}, result, nil, nil, "", "", "", nil))
			}
			fmt.Printf("Fetched template %s.\n", args[0])
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newTemplatesCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a content template",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateCreate,
	}
	addTemplateMutationFlags(cmd)
	return cmd
}

func newTemplatesUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <template-id> <name>",
		Short: "Update a content template",
		Args:  cobra.ExactArgs(2),
		RunE:  runTemplateUpdate,
	}
	addTemplateMutationFlags(cmd)
	return cmd
}

func newTemplatesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <template-id>",
		Short: "Delete a content template",
		Args:  cobra.ExactArgs(1),
		RunE:  runTemplateDelete,
	}
	cmd.Flags().Bool("dry-run", false, "Preview the delete without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func addTemplateMutationFlags(cmd *cobra.Command) {
	cmd.Flags().String("description", "", "Template description")
	cmd.Flags().String("file", "", "Template body file")
	cmd.Flags().String("format", "storage", "Template body format: storage or markdown")
	cmd.Flags().String("space", "", "Optional space key for a space template")
	cmd.Flags().Bool("dry-run", false, "Preview without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
}

func runTemplateCreate(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	payload, target, dryRun, idemKey, err := buildTemplatePayload(cmd, "", args[0])
	if err != nil {
		return err
	}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.create", target, map[string]any{"dry_run": true, "payload": payload}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.create", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	result, err := client.CreateTemplate(payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.template.create %s", args[0]))
	}
	return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.create", target, result, nil, nil, "", "", "", nil))
}

func runTemplateUpdate(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	payload, target, dryRun, idemKey, err := buildTemplatePayload(cmd, args[0], args[1])
	if err != nil {
		return err
	}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.update", target, map[string]any{"dry_run": true, "payload": payload}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.update", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	result, err := client.UpdateTemplate(payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.template.update %s", args[0]))
	}
	return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.update", target, result, nil, nil, "", "", "", nil))
}

func runTemplateDelete(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	target := map[string]any{"template_id": args[0]}
	if dryRun {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.delete", target, map[string]any{"dry_run": true, "deleted": false}, nil, nil, "", "", "", nil))
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.delete", target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	if err := client.DeleteTemplate(args[0]); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.template.delete %s", args[0]))
	}
	if mode == "json" || mode == "ndjson" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "templates.delete", target, map[string]any{"deleted": true}, nil, nil, "", "", "", nil))
	}
	fmt.Printf("Deleted template %s.\n", args[0])
	return nil
}

func buildTemplatePayload(cmd *cobra.Command, templateID string, name string) (map[string]any, map[string]any, bool, string, error) {
	description, _ := cmd.Flags().GetString("description")
	filePath, _ := cmd.Flags().GetString("file")
	format, _ := cmd.Flags().GetString("format")
	spaceKey, _ := cmd.Flags().GetString("space")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	if strings.TrimSpace(filePath) == "" {
		return nil, nil, false, "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--file is required for template body content.", ExitCode: 2}
	}
	body, err := readTextFile(filePath)
	if err != nil {
		return nil, nil, false, "", err
	}
	body, err = convertStorageBody(body, format)
	if err != nil {
		return nil, nil, false, "", err
	}
	payload := map[string]any{
		"name":         name,
		"templateType": "page",
		"body": map[string]any{
			"storage": map[string]any{
				"value":          body,
				"representation": "storage",
			},
		},
	}
	if strings.TrimSpace(description) != "" {
		payload["description"] = description
	}
	if strings.TrimSpace(spaceKey) != "" {
		payload["space"] = map[string]any{"key": spaceKey}
	}
	if strings.TrimSpace(templateID) != "" {
		payload["templateId"] = templateID
	}
	target := map[string]any{"name": name}
	if templateID != "" {
		target["template_id"] = templateID
	}
	if spaceKey != "" {
		target["space"] = spaceKey
	}
	return payload, target, dryRun, idemKey, nil
}
