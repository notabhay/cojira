package jira

import (
	"fmt"
	"sort"
	"strings"

	"github.com/cojira/cojira/internal/cli"
	"github.com/cojira/cojira/internal/output"
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
	if kind == "" {
		if hasFields {
			kind = "create/update"
		} else {
			kind = "unknown"
		}
	}

	result := map[string]any{
		"valid":      true,
		"kind":       kind,
		"has_fields": hasFields,
		"keys":       keys,
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
