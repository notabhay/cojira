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

// NewSpacesCmd creates the "spaces" subcommand.
func NewSpacesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "spaces [query]",
		Short: "List visible Confluence spaces",
		Args:  cobra.MaximumNArgs(1),
		RunE:  runSpaces,
	}
	cmd.Flags().Bool("all", false, "Fetch all spaces")
	cmd.Flags().Int("limit", 25, "Maximum spaces to fetch")
	cmd.Flags().Int("start", 0, "Start offset")
	cmd.Flags().Int("page-size", 50, "Page size when using --all")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	cmd.AddCommand(newSpacesGetCmd(), newSpacesCreateCmd(), newSpacesUpdateCmd(), newSpacesDeleteCmd())
	return cmd
}

func runSpaces(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	query := ""
	if len(args) > 0 {
		query = strings.TrimSpace(args[0])
	}
	all, _ := cmd.Flags().GetBool("all")
	limit, _ := cmd.Flags().GetInt("limit")
	start, _ := cmd.Flags().GetInt("start")
	pageSize, _ := cmd.Flags().GetInt("page-size")

	items := make([]map[string]any, 0)
	if all {
		if pageSize <= 0 {
			pageSize = 50
		}
		offset := start
		for {
			data, err := client.ListSpaces(pageSize, offset)
			if err != nil {
				return err
			}
			pageItems := extractResults(data)
			items = append(items, pageItems...)
			if len(pageItems) < pageSize {
				break
			}
			offset += len(pageItems)
		}
	} else {
		data, err := client.ListSpaces(limit, start)
		if err != nil {
			return err
		}
		items = extractResults(data)
	}

	if query != "" {
		needle := strings.ToLower(query)
		filtered := make([]map[string]any, 0, len(items))
		for _, item := range items {
			key := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", item["key"])))
			name := strings.ToLower(strings.TrimSpace(fmt.Sprintf("%v", item["name"])))
			if strings.Contains(key, needle) || strings.Contains(name, needle) {
				filtered = append(filtered, item)
			}
		}
		items = filtered
	}

	target := map[string]any{}
	if query != "" {
		target["query"] = query
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "spaces",
			target,
			map[string]any{"spaces": items, "summary": map[string]any{"count": len(items)}},
			nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		if query != "" {
			fmt.Printf("Found %d Confluence space(s) matching %q.\n", len(items), query)
		} else {
			fmt.Printf("Found %d Confluence space(s).\n", len(items))
		}
		return nil
	}

	if len(items) == 0 {
		fmt.Println("No spaces found.")
		return nil
	}

	fmt.Printf("Spaces (%d):\n\n", len(items))
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{
			normalizeMaybeString(item["key"]),
			output.Truncate(normalizeMaybeString(item["type"]), 18),
			output.Truncate(normalizeMaybeString(item["name"]), 56),
		})
	}
	fmt.Println(output.TableString([]string{"KEY", "TYPE", "NAME"}, rows))
	return nil
}

func newSpacesGetCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "get <key>",
		Short: "Get a Confluence space by key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := cli.NormalizeOutputMode(cmd)
			client, err := clientFromCmd(cmd)
			if err != nil {
				return err
			}
			space, err := client.GetSpace(args[0])
			if err != nil {
				return err
			}
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(true, "confluence", "spaces.get", map[string]any{"key": args[0]}, space, nil, nil, "", "", "", nil))
			}
			if mode == "summary" {
				fmt.Printf("Found Confluence space %s.\n", args[0])
				return nil
			}
			fmt.Printf("Space %s: %s\n", normalizeMaybeString(space["key"]), normalizeMaybeString(space["name"]))
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	return cmd
}

func newSpacesCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <key>",
		Short: "Create a Confluence space",
		Args:  cobra.ExactArgs(1),
		RunE:  runSpacesCreate,
	}
	addSpaceMutationFlags(cmd)
	return cmd
}

func newSpacesUpdateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "update <key>",
		Short: "Update a Confluence space",
		Args:  cobra.ExactArgs(1),
		RunE:  runSpacesUpdate,
	}
	addSpaceMutationFlags(cmd)
	return cmd
}

func newSpacesDeleteCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "delete <key>",
		Short: "Delete a Confluence space",
		Args:  cobra.ExactArgs(1),
		RunE:  runSpacesDelete,
	}
	cmd.Flags().Bool("dry-run", false, "Preview the delete without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cmd.Flags().Bool("yes", false, "Confirm deleting the space")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func addSpaceMutationFlags(cmd *cobra.Command) {
	cmd.Flags().String("name", "", "Space name")
	cmd.Flags().String("description", "", "Space description")
	cmd.Flags().String("file", "", "Read description from a file")
	cmd.Flags().String("type", "global", "Space type")
	cmd.Flags().Bool("dry-run", false, "Preview the change without applying")
	cmd.Flags().Bool("plan", false, "Alias for --dry-run")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
}

func runSpacesCreate(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	payload, target, dryRun, idemKey, err := buildSpacePayload(cmd, args[0], true)
	if err != nil {
		return err
	}
	if dryRun {
		return printSpaceMutationPreview(mode, "spaces.create", target, payload)
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return printSpaceMutationSkip(mode, "spaces.create", target)
	}
	result, err := client.CreateSpace(payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.space.create %s", args[0]))
	}
	return printSpaceMutationResult(mode, "spaces.create", target, result, "Created")
}

func runSpacesUpdate(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	payload, target, dryRun, idemKey, err := buildSpacePayload(cmd, args[0], false)
	if err != nil {
		return err
	}
	if dryRun {
		return printSpaceMutationPreview(mode, "spaces.update", target, payload)
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return printSpaceMutationSkip(mode, "spaces.update", target)
	}
	result, err := client.UpdateSpace(args[0], payload)
	if err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.space.update %s", args[0]))
	}
	return printSpaceMutationResult(mode, "spaces.update", target, result, "Updated")
}

func runSpacesDelete(cmd *cobra.Command, args []string) error {
	cli.ApplyPlanFlag(cmd)
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	yes, _ := cmd.Flags().GetBool("yes")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	target := map[string]any{"key": args[0]}
	if dryRun {
		return printSpaceMutationPreview(mode, "spaces.delete", target, map[string]any{"key": args[0]})
	}
	if !yes {
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Refusing to delete a space without --yes.", ExitCode: 2}
	}
	if idemKey != "" && idempotency.IsDuplicate(idemKey) {
		return printSpaceMutationSkip(mode, "spaces.delete", target)
	}
	if err := client.DeleteSpace(args[0]); err != nil {
		return err
	}
	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.space.delete %s", args[0]))
	}
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", "spaces.delete", target, map[string]any{"deleted": true}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Deleted Confluence space %s.\n", args[0])
		return nil
	}
	fmt.Printf("Deleted Confluence space %s.\n", args[0])
	return nil
}

func buildSpacePayload(cmd *cobra.Command, key string, requireName bool) (map[string]any, map[string]any, bool, string, error) {
	name, _ := cmd.Flags().GetString("name")
	description, _ := cmd.Flags().GetString("description")
	filePath, _ := cmd.Flags().GetString("file")
	spaceType, _ := cmd.Flags().GetString("type")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")
	if description != "" && filePath != "" {
		return nil, nil, false, "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Use either --description or --file, not both.", ExitCode: 2}
	}
	if filePath != "" {
		content, err := readTextFile(filePath)
		if err != nil {
			return nil, nil, false, "", err
		}
		description = content
	}
	if requireName && strings.TrimSpace(name) == "" {
		return nil, nil, false, "", &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "--name is required when creating a space.", ExitCode: 2}
	}
	if strings.TrimSpace(spaceType) == "" {
		spaceType = "global"
	}
	payload := map[string]any{
		"key":  key,
		"type": spaceType,
	}
	if strings.TrimSpace(name) != "" {
		payload["name"] = name
	}
	if strings.TrimSpace(description) != "" {
		payload["description"] = map[string]any{
			"plain": map[string]any{
				"value":          description,
				"representation": "plain",
			},
		}
	}
	target := map[string]any{"key": key}
	return payload, target, dryRun, idemKey, nil
}

func printSpaceMutationPreview(mode, command string, target map[string]any, payload map[string]any) error {
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", command, target, map[string]any{"dry_run": true, "payload": payload}, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("Would apply %s for space %s.\n", command, normalizeMaybeString(target["key"]))
		return nil
	}
	fmt.Printf("Would apply %s for space %s.\n", command, normalizeMaybeString(target["key"]))
	return nil
}

func printSpaceMutationSkip(mode, command string, target map[string]any) error {
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", command, target, map[string]any{"skipped": true, "reason": "idempotency_key_already_used"}, nil, nil, "", "", "", nil))
	}
	fmt.Printf("Skipped duplicate %s for %s.\n", command, normalizeMaybeString(target["key"]))
	return nil
}

func printSpaceMutationResult(mode, command string, target map[string]any, result map[string]any, verb string) error {
	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(true, "confluence", command, target, result, nil, nil, "", "", "", nil))
	}
	if mode == "summary" {
		fmt.Printf("%s Confluence space %s.\n", verb, normalizeMaybeString(target["key"]))
		return nil
	}
	fmt.Printf("%s Confluence space %s.\n", verb, normalizeMaybeString(target["key"]))
	return nil
}

func extractResults(data map[string]any) []map[string]any {
	raw, _ := data["results"].([]any)
	items := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		if m, ok := item.(map[string]any); ok {
			items = append(items, m)
		}
	}
	return items
}
