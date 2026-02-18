package confluence

import (
	"fmt"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/idempotency"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewCreateCmd creates the "create" subcommand.
func NewCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <title>",
		Short: "Create a new page",
		Args:  cobra.ExactArgs(1),
		RunE:  runCreate,
	}
	cmd.Flags().StringP("space", "s", "", "Space key (or set confluence.default_space)")
	cmd.Flags().StringP("parent", "p", "", "Parent page identifier")
	cmd.Flags().StringP("file", "f", "", "Content file (storage-format XHTML)")
	cmd.Flags().Bool("plan", false, "Preview create without applying")
	cli.AddOutputFlags(cmd, true)
	cli.AddHTTPRetryFlags(cmd)
	cli.AddIdempotencyFlags(cmd)
	return cmd
}

func runCreate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	client, err := clientFromCmd(cmd)
	if err != nil {
		return err
	}

	cfgData := loadProjectConfigData()
	title := strings.TrimSpace(args[0])
	space, _ := cmd.Flags().GetString("space")
	if space == "" {
		space = defaultSpace(cfgData)
	}
	parentArg, _ := cmd.Flags().GetString("parent")
	filePath, _ := cmd.Flags().GetString("file")
	planMode, _ := cmd.Flags().GetBool("plan")
	idemKey, _ := cmd.Flags().GetString("idempotency-key")

	if title == "" {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.InvalidTitle, "Title is required.", "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "create",
				map[string]any{"title": title, "space": space},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintln(os.Stderr, "Error: Title is required.")
		return &cerrors.CojiraError{Code: cerrors.InvalidTitle, Message: "Title is required.", ExitCode: 1}
	}

	if space == "" {
		exitCode := 2
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.OpFailed, "Space key is required (or set confluence.default_space).", "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "create",
				map[string]any{"title": title},
				nil, nil, []any{errObj}, "", "", "", &exitCode,
			))
		}
		fmt.Fprintln(os.Stderr, "Error: Space key is required (or set confluence.default_space).")
		return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Space key is required.", ExitCode: 2}
	}

	// Read body from file or use empty.
	var body string
	if filePath != "" {
		body, err = readTextFile(filePath)
		if err != nil {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.FileNotFound, fmt.Sprintf("File not found: %s", filePath), "", "", nil)
				return output.PrintJSON(output.BuildEnvelope(
					false, "confluence", "create",
					map[string]any{"title": title, "file": filePath},
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Error: File not found: %s\n", filePath)
			return err
		}
	}

	// Resolve parent if provided.
	var parentID string
	if parentArg != "" {
		parentID, err = ResolvePageID(client, parentArg, "")
		if err != nil {
			if mode == "json" {
				errObj, _ := output.ErrorObj(cerrors.IdentUnresolved, err.Error(), cerrors.HintIdentifier(ConfluenceIdentifierFormats), "", nil)
				return output.PrintJSON(output.BuildEnvelope(
					false, "confluence", "create",
					map[string]any{"title": title, "space": space, "parent": parentArg},
					nil, nil, []any{errObj}, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Error resolving parent: %v\n", err)
			return err
		}
	}

	if planMode {
		if mode == "json" {
			var parentIDVal any
			if parentID != "" {
				parentIDVal = parentID
			}
			return output.PrintJSON(output.BuildEnvelope(
				true, "confluence", "create",
				map[string]any{"title": title, "space": space, "parent": parentArg, "parent_id": parentIDVal},
				map[string]any{
					"plan":      true,
					"title":     title,
					"space":     space,
					"parent_id": parentIDVal,
					"idempotency": map[string]any{
						"key": output.IdempotencyKey("confluence.create", space, title, body),
					},
				},
				nil, nil, "", "", "", nil,
			))
		}
		if mode == "summary" {
			parentDesc := ""
			if parentID != "" {
				parentDesc = fmt.Sprintf(" under %s", parentID)
			}
			fmt.Printf("Would create page '%s' in %s%s.\n", title, space, parentDesc)
			return nil
		}
		quiet, _ := cmd.Flags().GetBool("quiet")
		if !quiet {
			parentDesc := ""
			if parentID != "" {
				parentDesc = fmt.Sprintf(" under %s", parentID)
			}
			r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would create page '%s' in %s%s", title, space, parentDesc)}
			fmt.Println(r.Format())
		}
		return nil
	}

	// Idempotency check.
	if idemKey != "" {
		if idempotency.IsDuplicate(idemKey) {
			if mode == "json" {
				return output.PrintJSON(output.BuildEnvelope(
					true, "confluence", "create",
					map[string]any{"title": title, "space": space},
					map[string]any{"skipped": true, "reason": "idempotency_key_already_used"},
					nil, nil, "", "", "", nil,
				))
			}
			fmt.Fprintf(os.Stderr, "Skipped (idempotency key already used): %s\n", idemKey)
			return nil
		}
	}

	// Build create payload.
	payload := map[string]any{
		"type":  "page",
		"title": title,
		"space": map[string]any{"key": space},
		"body": map[string]any{
			"storage": map[string]any{
				"value":          body,
				"representation": "storage",
			},
		},
	}
	if parentID != "" {
		payload["ancestors"] = []map[string]any{{"id": parentID}}
	}

	result, err := client.CreatePage(payload)
	if err != nil {
		if mode == "json" {
			errObj, _ := output.ErrorObj(cerrors.CreateFailed, err.Error(), "", "", nil)
			return output.PrintJSON(output.BuildEnvelope(
				false, "confluence", "create",
				map[string]any{"title": title, "space": space, "parent": parentArg},
				nil, nil, []any{errObj}, "", "", "", nil,
			))
		}
		fmt.Fprintf(os.Stderr, "Error creating page: %v\n", err)
		return err
	}

	if idemKey != "" {
		_ = idempotency.Record(idemKey, fmt.Sprintf("confluence.create %s", space))
	}

	newPageID := fmt.Sprintf("%v", result["id"])
	receipt := output.Receipt{OK: true, Message: fmt.Sprintf("Created page %s: %s", newPageID, title)}

	if mode == "json" {
		var parentIDVal any
		if parentID != "" {
			parentIDVal = parentID
		}
		return output.PrintJSON(output.BuildEnvelope(
			true, "confluence", "create",
			map[string]any{"title": title, "space": space, "parent": parentArg, "parent_id": parentIDVal},
			map[string]any{
				"id":        newPageID,
				"title":     title,
				"space":     space,
				"parent_id": parentIDVal,
				"url":       fmt.Sprintf("%s/pages/viewpage.action?pageId=%s", client.BaseURL(), newPageID),
				"receipt":   receipt.Format(),
				"idempotency": map[string]any{
					"key": output.IdempotencyKey("confluence.create", space, title, body),
				},
			},
			nil, nil, "", "", "", nil,
		))
	}
	if mode == "summary" {
		fmt.Printf("Created page %s: %s.\n", newPageID, title)
		return nil
	}
	quiet, _ := cmd.Flags().GetBool("quiet")
	if !quiet {
		fmt.Println(receipt.Format())
	}
	return nil
}
