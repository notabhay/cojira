package jira

import (
	"fmt"
	"sort"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewValidateCmd creates the "validate" subcommand.
func NewValidateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "validate <file>",
		Short: "Basic sanity check for a Jira JSON payload",
		Args:  cobra.ExactArgs(1),
		RunE:  runValidate,
	}
	cmd.Flags().String("kind", "", "Optional payload kind hint (create, update, batch)")
	cli.AddOutputFlags(cmd, true)
	return cmd
}

func runValidate(cmd *cobra.Command, args []string) error {
	mode := cli.NormalizeOutputMode(cmd)
	file := args[0]
	kind, _ := cmd.Flags().GetString("kind")

	payload, err := readJSONFile(file)
	if err != nil {
		return err
	}

	keys := make([]string, 0, len(payload))
	for k := range payload {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	_, hasFields := payload["fields"].(map[string]any)
	operations := mapSlice(payload["operations"])
	if kind == "" {
		switch {
		case len(operations) > 0:
			kind = "batch"
		case hasFields:
			if fields, _ := payload["fields"].(map[string]any); safeString(fields, "project", "key") != "" && safeString(fields, "issuetype", "name") != "" {
				kind = "create"
			} else {
				kind = "update"
			}
		default:
			kind = "unknown"
		}
	}

	result := map[string]any{
		"valid":      true,
		"kind":       kind,
		"has_fields": hasFields,
		"keys":       keys,
	}

	switch kind {
	case "create":
		fields, _ := payload["fields"].(map[string]any)
		missing := []string{}
		if safeString(fields, "project", "key") == "" {
			missing = append(missing, "fields.project.key")
		}
		if safeString(fields, "issuetype", "name") == "" && safeString(fields, "issuetype", "id") == "" {
			missing = append(missing, "fields.issuetype")
		}
		if strings.TrimSpace(fmt.Sprintf("%v", fields["summary"])) == "" {
			missing = append(missing, "fields.summary")
		}
		result["missing"] = missing
		result["valid"] = len(missing) == 0
	case "update":
		result["valid"] = hasFields
	case "batch":
		opSummaries := []map[string]any{}
		for _, op := range operations {
			summary := map[string]any{
				"op":      batchString(op, "op"),
				"issue":   batchString(op, "issue"),
				"capture": batchString(op, "capture"),
			}
			switch {
			case batchString(op, "template") != "":
				summary["source"] = "template"
			case batchString(op, "clone") != "":
				summary["source"] = "clone"
			case batchInlineJSON(op) != "":
				summary["source"] = "inline"
			case batchString(op, "file") != "":
				summary["source"] = "file"
			default:
				summary["source"] = "quick"
			}
			opSummaries = append(opSummaries, summary)
		}
		result["operations"] = opSummaries
		result["valid"] = len(operations) > 0
	}

	if mode == "json" {
		return output.PrintJSON(output.BuildEnvelope(
			true, "jira", "validate",
			map[string]any{"file": file, "kind": kind},
			result, nil, nil, "", "", "", nil,
		))
	}

	if mode == "summary" {
		keysStr := "none"
		if len(keys) > 0 {
			keysStr = strings.Join(keys, ", ")
		}
		fmt.Printf("Sanity check passed for Jira payload (%s). Keys: %s\n", kind, keysStr)
		return nil
	}

	fmt.Println("Sanity check passed for Jira payload.")
	fmt.Printf("Kind: %s\n", kind)
	keysStr := "(none)"
	if len(keys) > 0 {
		keysStr = strings.Join(keys, ", ")
	}
	fmt.Printf("Keys: %s\n", keysStr)
	if !hasFields {
		fmt.Println("Warning: No top-level 'fields' object found.")
	}
	return nil
}
