package board

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewBoardDetailViewCmd returns the "board-detail-view" parent command
// with its 4 subcommands: get, search-fields, export, apply.
func NewBoardDetailViewCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board-detail-view",
		Short: "(EXPERIMENTAL) Manage Jira board Issue Detail View fields (GreenHopper)",
		Long: "EXPERIMENTAL: Configure Jira Software board Issue Detail View fields using internal GreenHopper REST APIs.\n" +
			"Requires: `cojira jira --experimental ...`",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().Bool("plan", false, "Preview without applying (for `cojira plan`)")

	cmd.AddCommand(
		newDetailViewGetCmd(clientFn),
		newDetailViewSearchFieldsCmd(clientFn),
		newDetailViewExportCmd(clientFn),
		newDetailViewApplyCmd(clientFn),
	)

	return cmd
}

// ── GreenHopper API helpers ─────────────────────────────────────────

func ghDetailViewFieldConfigURL(client *jira.Client, boardID string) string {
	return greenhopperURL(client.BaseURL(), fmt.Sprintf("detailviewfield/%s/configured", boardID))
}

func ghDetailViewFieldURL(client *jira.Client, boardID string, fieldID *int) string {
	if fieldID == nil {
		return greenhopperURL(client.BaseURL(), fmt.Sprintf("detailviewfield/%s/field", boardID))
	}
	return greenhopperURL(client.BaseURL(), fmt.Sprintf("detailviewfield/%s/field/%d", boardID, *fieldID))
}

func ghGetDetailViewFieldConfig(client *jira.Client, boardID string) (map[string]any, error) {
	if client == nil {
		return nil, &cerrors.CojiraError{
			Code:        cerrors.Error,
			Message:     "GreenHopper detail view request requires a Jira client.",
			UserMessage: "This board command hit an internal setup error. Please update cojira and try again.",
			ExitCode:    1,
		}
	}
	u := ghDetailViewFieldConfigURL(client, boardID)
	resp, err := client.RequestURL("GET", u, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := requireResponseBody(resp, "GreenHopper detail view config"); err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "Unexpected response from GreenHopper detail view field config.",
			ExitCode: 1,
		}
	}
	return data, nil
}

// ── get ─────────────────────────────────────────────────────────────

func newDetailViewGetCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "get <board>",
		Short:         "Get Issue Detail View field config for a board",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireExperimental(cmd); err != nil {
				return err
			}
			mode := cli.NormalizeOutputMode(cmd)
			boardID, err := resolveBoardID(args[0])
			if err != nil {
				return err
			}
			client, err := resolveBoardClient(cmd, clientFn)
			if err != nil {
				return err
			}

			rawCfg, err := ghGetDetailViewFieldConfig(client, boardID)
			if err != nil {
				return err
			}
			cfg, err := ExtractDetailViewFieldConfig(rawCfg)
			if err != nil {
				return err
			}

			current := cfg.CurrentFields
			available := cfg.AvailableFields
			canEdit := cfg.CanEdit

			if mode == "json" {
				currentOut := make([]map[string]any, len(current))
				for i, f := range current {
					currentOut[i] = map[string]any{
						"fieldId":  f.FieldID,
						"name":     f.Name,
						"category": f.Category,
					}
				}
				result := map[string]any{
					"canEdit":       canEdit,
					"currentFields": currentOut,
					"summary":       map[string]any{"count": len(current)},
				}

				includeAvailable, _ := cmd.Flags().GetBool("include-available")
				if includeAvailable {
					availableOut := make([]map[string]any, len(available))
					for i, f := range available {
						availableOut[i] = map[string]any{
							"fieldId":  f.FieldID,
							"name":     f.Name,
							"category": f.Category,
							"isValid":  f.IsValid,
						}
					}
					result["availableFields"] = availableOut
				}

				emitEnvelope(true, "board-detail-view get", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}

			if mode == "summary" {
				fmt.Printf("Board %s Issue Detail View: fields=%d canEdit=%t.\n", boardID, len(current), canEdit)
				return nil
			}

			// Human mode.
			fmt.Printf("Board %s Issue Detail View (canEdit=%t):\n", boardID, canEdit)
			if len(current) == 0 {
				fmt.Println("  (no fields)")
			}
			for _, f := range current {
				name := f.Name
				if name == "" {
					name = "-"
				}
				fmt.Printf("  - %s | %s\n", f.FieldID, name)
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().Bool("include-available", false, "Include available fields in JSON output")
	return cmd
}

// ── search-fields ───────────────────────────────────────────────────

func newDetailViewSearchFieldsCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "search-fields <board>",
		Short:         "Search available detail view fields by name or ID",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireExperimental(cmd); err != nil {
				return err
			}
			mode := cli.NormalizeOutputMode(cmd)
			boardID, err := resolveBoardID(args[0])
			if err != nil {
				return err
			}
			query, _ := cmd.Flags().GetString("query")
			needle := strings.TrimSpace(strings.ToLower(query))
			if needle == "" {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "--query is required.",
					ExitCode: 2,
				}
			}
			client, err := resolveBoardClient(cmd, clientFn)
			if err != nil {
				return err
			}

			rawCfg, err := ghGetDetailViewFieldConfig(client, boardID)
			if err != nil {
				return err
			}
			cfg, err := ExtractDetailViewFieldConfig(rawCfg)
			if err != nil {
				return err
			}

			current := cfg.CurrentFields
			available := cfg.AvailableFields
			currentIDs := make(map[string]bool)
			for _, f := range current {
				if f.FieldID != "" {
					currentIDs[f.FieldID] = true
				}
			}

			// Merge current + available, deduplicate by fieldId.
			allFields := make([]DetailViewField, 0, len(current)+len(available))
			seen := make(map[string]bool)
			for _, f := range current {
				if !seen[f.FieldID] {
					allFields = append(allFields, f)
					seen[f.FieldID] = true
				}
			}
			for _, f := range available {
				if !seen[f.FieldID] {
					allFields = append(allFields, f)
					seen[f.FieldID] = true
				}
			}

			// Search.
			var matches []DetailViewField
			for _, f := range allFields {
				if strings.Contains(strings.ToLower(f.FieldID), needle) ||
					strings.Contains(strings.ToLower(f.Name), needle) ||
					strings.Contains(strings.ToLower(f.Category), needle) {
					matches = append(matches, f)
				}
			}

			maxResults := 20
			rows := matches
			if len(rows) > maxResults {
				rows = rows[:maxResults]
			}

			summaryMap := map[string]any{
				"total":    len(matches),
				"returned": len(rows),
			}

			if mode == "json" {
				rowsOut := make([]map[string]any, len(rows))
				for i, f := range rows {
					rowsOut[i] = map[string]any{
						"fieldId":  f.FieldID,
						"name":     f.Name,
						"category": f.Category,
						"isActive": currentIDs[f.FieldID],
						"isValid":  f.IsValid,
					}
				}
				result := map[string]any{"fields": rowsOut, "summary": summaryMap}
				emitEnvelope(true, "board-detail-view search-fields", map[string]any{"board": boardID, "query": needle}, result, nil, nil)
				return nil
			}

			if mode == "summary" {
				fmt.Printf("Found %d field(s) matching %q.\n", len(matches), needle)
				return nil
			}

			if len(matches) == 0 {
				fmt.Printf("No fields matching %q.\n", needle)
				return nil
			}

			fmt.Printf("Fields matching %q:\n", needle)
			for _, f := range rows {
				active := ""
				if currentIDs[f.FieldID] {
					active = " [active]"
				}
				name := f.Name
				if name == "" {
					name = "-"
				}
				fmt.Printf("  - %s | %s | %s%s\n", f.FieldID, name, f.Category, active)
			}
			if len(matches) > len(rows) {
				fmt.Printf("  ... and %d more\n", len(matches)-len(rows))
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().String("query", "", "Search term (substring match on fieldId, name, or category)")
	return cmd
}

// ── export ──────────────────────────────────────────────────────────

func newDetailViewExportCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "export <board>",
		Short:         "Export Issue Detail View field config to JSON file",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireExperimental(cmd); err != nil {
				return err
			}
			mode := cli.NormalizeOutputMode(cmd)
			boardID, err := resolveBoardID(args[0])
			if err != nil {
				return err
			}
			client, err := resolveBoardClient(cmd, clientFn)
			if err != nil {
				return err
			}

			rawCfg, err := ghGetDetailViewFieldConfig(client, boardID)
			if err != nil {
				return err
			}
			cfg, err := ExtractDetailViewFieldConfig(rawCfg)
			if err != nil {
				return err
			}

			current := cfg.CurrentFields
			fieldsOut := make([]map[string]any, len(current))
			for i, f := range current {
				fieldsOut[i] = f.ToExportJSON()
			}
			exportData := map[string]any{"fields": fieldsOut}

			outPath, _ := cmd.Flags().GetString("output")
			b, _ := json.MarshalIndent(exportData, "", "  ")
			if err := os.WriteFile(outPath, b, 0644); err != nil {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  fmt.Sprintf("Could not write to %s: %v", outPath, err),
					ExitCode: 1,
				}
			}

			if mode == "json" {
				result := map[string]any{
					"saved_to": outPath,
					"summary":  map[string]any{"count": len(current)},
				}
				emitEnvelope(true, "board-detail-view export", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}

			if mode == "summary" {
				fmt.Printf("Exported %d field(s) for board %s to %s.\n", len(current), boardID, outPath)
				return nil
			}

			fmt.Printf("Saved Issue Detail View field config to: %s\n", outPath)
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().StringP("output", "o", "detail-view.json", "Output file path")
	return cmd
}

// ── apply ───────────────────────────────────────────────────────────

func newDetailViewApplyCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "apply <board>",
		Short:         "Apply Issue Detail View fields from JSON file",
		Args:          cobra.ExactArgs(1),
		SilenceUsage:  true,
		SilenceErrors: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := requireExperimental(cmd); err != nil {
				return err
			}
			cli.ApplyPlanFlag(cmd)
			mode := cli.NormalizeOutputMode(cmd)
			boardID, err := resolveBoardID(args[0])
			if err != nil {
				return err
			}
			filePath, _ := cmd.Flags().GetString("file")
			deleteMissing, _ := cmd.Flags().GetBool("delete-missing")
			client, err := resolveBoardClient(cmd, clientFn)
			if err != nil {
				return err
			}

			desiredFieldIDs, err := LoadDesiredDetailViewFieldsFile(filePath)
			if err != nil {
				return err
			}

			currentCfgRaw, err := ghGetDetailViewFieldConfig(client, boardID)
			if err != nil {
				return err
			}
			currentCfg, err := ExtractDetailViewFieldConfig(currentCfgRaw)
			if err != nil {
				return err
			}

			if !currentCfg.CanEdit {
				return &cerrors.CojiraError{
					Code:     cerrors.HTTP403,
					Message:  "Board Issue Detail View is not editable with this token/user.",
					Hint:     "You need board administration permission. " + cerrors.HintPermission(),
					ExitCode: 1,
				}
			}

			currentFields := currentCfg.CurrentFields
			availableFields := currentCfg.AvailableFields
			currentByFieldID := make(map[string]DetailViewField)
			for _, f := range currentFields {
				if f.FieldID != "" {
					currentByFieldID[f.FieldID] = f
				}
			}
			desiredSet := make(map[string]bool)
			for _, fid := range desiredFieldIDs {
				desiredSet[fid] = true
			}

			// Pre-validate: every desired field must be either current or available.
			knownFieldIDs := make(map[string]bool)
			for _, f := range currentFields {
				if f.FieldID != "" {
					knownFieldIDs[f.FieldID] = true
				}
			}
			for _, f := range availableFields {
				if f.FieldID != "" {
					knownFieldIDs[f.FieldID] = true
				}
			}
			var unknown []string
			for _, fid := range desiredFieldIDs {
				if !knownFieldIDs[fid] {
					unknown = append(unknown, fid)
				}
			}
			if len(unknown) > 0 {
				return &cerrors.CojiraError{
					Code:     cerrors.InvalidJSON,
					Message:  fmt.Sprintf("Field(s) not available on this board: %s", strings.Join(unknown, ", ")),
					Hint:     "Use `board-detail-view search-fields` to find valid field IDs (or `get --include-available` to list them all).",
					ExitCode: 1,
				}
			}

			// Build ops.
			var adds []string
			for _, fid := range desiredFieldIDs {
				if _, exists := currentByFieldID[fid]; !exists {
					adds = append(adds, fid)
				}
			}

			var deletes []DetailViewField
			if deleteMissing {
				for _, f := range currentFields {
					if f.FieldID != "" && !desiredSet[f.FieldID] {
						deletes = append(deletes, f)
					}
				}
			}

			var ops []map[string]any
			for _, fid := range adds {
				ops = append(ops, map[string]any{"action": "add", "fieldId": fid})
			}
			for _, f := range deletes {
				op := map[string]any{"action": "delete", "fieldId": f.FieldID}
				if f.ID != nil {
					op["id"] = *f.ID
				}
				if f.Name != "" {
					op["name"] = f.Name
				}
				ops = append(ops, op)
			}

			// Compute final order.
			var extrasFieldIDs []string
			for _, f := range currentFields {
				if f.FieldID != "" && !desiredSet[f.FieldID] {
					extrasFieldIDs = append(extrasFieldIDs, f.FieldID)
				}
			}
			finalFieldIDs := make([]string, len(desiredFieldIDs))
			copy(finalFieldIDs, desiredFieldIDs)
			if !deleteMissing {
				finalFieldIDs = append(finalFieldIDs, extrasFieldIDs...)
			}

			currentOrder := make([]string, 0)
			for _, f := range currentFields {
				if f.FieldID != "" {
					currentOrder = append(currentOrder, f.FieldID)
				}
			}
			if !stringSliceEqual(finalFieldIDs, currentOrder) {
				ops = append(ops, map[string]any{"action": "reorder", "finalOrder": finalFieldIDs})
			}

			dryRun := isDryRun(cmd)
			summaryMap := map[string]any{
				"add":     len(adds),
				"delete":  len(deletes),
				"fields":  len(finalFieldIDs),
				"dry_run": dryRun,
			}

			if dryRun {
				if mode == "json" {
					result := map[string]any{"dry_run": true, "ops": ops, "summary": summaryMap}
					emitEnvelope(true, "board-detail-view apply", map[string]any{"board": boardID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Would apply Issue Detail View fields to board %s.\n", boardID)
					return nil
				}
				printDetailViewOpsHuman(ops, finalFieldIDs)
				if !isQuiet(cmd) {
					r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would apply Issue Detail View fields to board %s", boardID)}
					fmt.Println(r.Format())
				}
				return nil
			}

			// Execute the apply.
			if err := executeDetailViewApply(client, cmd, boardID, desiredFieldIDs, desiredSet, currentFields, currentByFieldID, deleteMissing); err != nil {
				// Best-effort verification.
				if verifyErr := verifyDetailViewApply(client, boardID, desiredFieldIDs, desiredSet, deleteMissing, mode, cmd, ops, summaryMap); verifyErr == nil {
					return nil
				}
				return err
			}

			if mode == "json" {
				result := map[string]any{"dry_run": false, "ops": ops, "summary": summaryMap}
				emitEnvelope(true, "board-detail-view apply", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}
			if mode == "summary" {
				fmt.Printf("Applied Issue Detail View fields to board %s.\n", boardID)
				return nil
			}
			if !isQuiet(cmd) {
				r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Applied Issue Detail View fields to board %s", boardID)}
				fmt.Println(r.Format())
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().String("file", "", "Desired Issue Detail View field config JSON file")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().Bool("delete-missing", false, "Delete existing fields not in file")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("diff", false, "Show diff of changes")
	cmd.Flags().Bool("preview", false, "Show preview of changes")
	cmd.Flags().Float64("sleep", 0.0, "Delay between API calls in seconds")
	return cmd
}

func executeDetailViewApply(client *jira.Client, cmd *cobra.Command, boardID string, desiredFieldIDs []string, desiredSet map[string]bool, currentFields []DetailViewField, currentByFieldID map[string]DetailViewField, deleteMissing bool) error {
	// Deletes first.
	if deleteMissing {
		for _, f := range currentFields {
			if f.FieldID == "" || desiredSet[f.FieldID] {
				continue
			}
			if f.ID == nil {
				continue
			}
			u := ghDetailViewFieldURL(client, boardID, f.ID)
			resp, err := client.RequestURL("DELETE", u, nil, nil)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()
			sleepIfNeeded(cmd)
		}
	}

	// Add missing fields.
	adds := make([]string, 0)
	for _, fid := range desiredFieldIDs {
		if _, exists := currentByFieldID[fid]; !exists {
			adds = append(adds, fid)
		}
	}
	for _, fid := range adds {
		u := ghDetailViewFieldURL(client, boardID, nil)
		body, _ := json.Marshal(map[string]any{"fieldId": fid})
		resp, err := client.RequestURL("POST", u, body, nil)
		if err != nil {
			return err
		}
		var data map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&data)
		_ = resp.Body.Close()
		if data == nil || data["id"] == nil {
			return &cerrors.CojiraError{
				Code:     cerrors.CreateFailed,
				Message:  "GreenHopper add detail view field did not return an id.",
				ExitCode: 1,
			}
		}
		created, err := DetailViewFieldFromAPI(data, 0, false)
		if err != nil {
			return err
		}
		currentByFieldID[created.FieldID] = created
		sleepIfNeeded(cmd)
	}

	// Re-fetch after mutations.
	var currentForMoves []DetailViewField
	var currentByFieldIDForMoves map[string]DetailViewField
	if deleteMissing || len(adds) > 0 {
		cfgAfterRaw, err := ghGetDetailViewFieldConfig(client, boardID)
		if err != nil {
			return err
		}
		cfgAfter, err := ExtractDetailViewFieldConfig(cfgAfterRaw)
		if err != nil {
			return err
		}
		currentForMoves = cfgAfter.CurrentFields
		currentByFieldIDForMoves = make(map[string]DetailViewField)
		for _, f := range currentForMoves {
			if f.FieldID != "" {
				currentByFieldIDForMoves[f.FieldID] = f
			}
		}
	} else {
		currentForMoves = currentFields
		currentByFieldIDForMoves = currentByFieldID
	}

	// Build final id order.
	var finalIDs []int
	for _, fid := range desiredFieldIDs {
		f, ok := currentByFieldIDForMoves[fid]
		if !ok || f.ID == nil {
			return &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Could not resolve id for fieldId=%q after adds.", fid),
				ExitCode: 1,
			}
		}
		finalIDs = append(finalIDs, *f.ID)
	}

	if !deleteMissing {
		for _, f := range currentForMoves {
			if f.FieldID == "" || desiredSet[f.FieldID] {
				continue
			}
			if f.ID == nil {
				continue
			}
			finalIDs = append(finalIDs, *f.ID)
		}
	}

	var currentIDs []int
	for _, f := range currentForMoves {
		if f.ID != nil {
			currentIDs = append(currentIDs, *f.ID)
		}
	}

	moveOps := ComputeMoveOps(currentIDs, finalIDs)
	for _, op := range moveOps {
		moveURL := greenhopperURL(client.BaseURL(), fmt.Sprintf("detailviewfield/%s/field/%d/move", boardID, op.ID))
		if op.Position == "First" {
			if err := ghRequestNoBody(client, "POST", moveURL, map[string]any{"position": "First"}); err != nil {
				return err
			}
		} else if op.AfterID != nil {
			afterURL := ghDetailViewFieldURL(client, boardID, op.AfterID)
			if err := ghRequestNoBody(client, "POST", moveURL, map[string]any{"after": afterURL}); err != nil {
				return err
			}
		}
		sleepIfNeeded(cmd)
	}

	return nil
}

func verifyDetailViewApply(client *jira.Client, boardID string, desiredFieldIDs []string, desiredSet map[string]bool, deleteMissing bool, mode string, cmd *cobra.Command, ops []map[string]any, summary map[string]any) error {
	cfgRaw, err := ghGetDetailViewFieldConfig(client, boardID)
	if err != nil {
		return err
	}
	cfg, err := ExtractDetailViewFieldConfig(cfgRaw)
	if err != nil {
		return err
	}

	verifyCurrent := cfg.CurrentFields
	verifyByFieldID := make(map[string]DetailViewField)
	for _, f := range verifyCurrent {
		if f.FieldID != "" {
			verifyByFieldID[f.FieldID] = f
		}
	}

	// Check: all desired fields present.
	for _, fid := range desiredFieldIDs {
		if _, ok := verifyByFieldID[fid]; !ok {
			return fmt.Errorf("verification failed: missing field %s", fid)
		}
	}

	// Check: extras order.
	var verifyExtras []string
	for _, f := range verifyCurrent {
		if f.FieldID != "" && !desiredSet[f.FieldID] {
			verifyExtras = append(verifyExtras, f.FieldID)
		}
	}
	verifyFinal := make([]string, len(desiredFieldIDs))
	copy(verifyFinal, desiredFieldIDs)
	if !deleteMissing {
		verifyFinal = append(verifyFinal, verifyExtras...)
	}
	verifyOrder := make([]string, 0)
	for _, f := range verifyCurrent {
		if f.FieldID != "" {
			verifyOrder = append(verifyOrder, f.FieldID)
		}
	}
	if !stringSliceEqual(verifyFinal, verifyOrder) {
		return fmt.Errorf("verification failed: order mismatch")
	}

	// Check: no stale deletes.
	if deleteMissing {
		for _, fid := range verifyExtras {
			if fid != "" {
				return fmt.Errorf("verification failed: stale field %s", fid)
			}
		}
	}

	// Board matches desired state.
	warn := "Applied Issue Detail View fields, but got a transient error while updating. The board now matches the desired configuration."
	if mode == "json" {
		result := map[string]any{
			"dry_run":              false,
			"ops":                  ops,
			"summary":              summary,
			"verified_after_error": true,
		}
		emitEnvelope(true, "board-detail-view apply", map[string]any{"board": boardID}, result, []any{warn}, nil)
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Applied Issue Detail View fields to board %s (verified).\n", boardID)
		return nil
	}
	if !isQuiet(cmd) {
		r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Applied Issue Detail View fields to board %s (verified)", boardID)}
		fmt.Println(r.Format())
	}
	return nil
}

func printDetailViewOpsHuman(ops []map[string]any, finalOrder []string) {
	if len(ops) == 0 {
		fmt.Println("No changes.")
		return
	}
	fmt.Println("Planned operations:")
	for _, op := range ops {
		action, _ := op["action"].(string)
		switch action {
		case "add":
			fieldID, _ := op["fieldId"].(string)
			name, _ := op["name"].(string)
			if name != "" {
				fmt.Printf("  - add: %s (%s)\n", fieldID, name)
			} else {
				fmt.Printf("  - add: %s\n", fieldID)
			}
		case "delete":
			fieldID, _ := op["fieldId"].(string)
			name, _ := op["name"].(string)
			if name != "" {
				fmt.Printf("  - delete: %s (%s)\n", fieldID, name)
			} else {
				fmt.Printf("  - delete: %s\n", fieldID)
			}
		case "reorder":
			if len(finalOrder) > 0 {
				fmt.Printf("  - reorder: %s\n", strings.Join(finalOrder, " → "))
			} else {
				fmt.Println("  - reorder: will reorder fields to match file order")
			}
		default:
			fmt.Printf("  - %s: %v\n", action, op)
		}
	}
}

func stringSliceEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
