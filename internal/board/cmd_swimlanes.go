package board

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/notabhay/cojira/internal/cli"
	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/notabhay/cojira/internal/jira"
	"github.com/notabhay/cojira/internal/output"
	"github.com/spf13/cobra"
)

// NewBoardSwimlanesCmd returns the "board-swimlanes" parent command
// with its 10 subcommands.
func NewBoardSwimlanesCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "board-swimlanes",
		Short: "(EXPERIMENTAL) Manage Jira board swimlanes (GreenHopper)",
		Long: "EXPERIMENTAL: Configure Jira Software board swimlanes using internal GreenHopper REST APIs.\n" +
			"These endpoints are not part of Jira's stable public API and may break after Jira upgrades.\n" +
			"Requires: `cojira jira --experimental ...`",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	cmd.Flags().Bool("plan", false, "Preview without applying (for `cojira plan`)")

	cmd.AddCommand(
		newSwimlanesGetCmd(clientFn),
		newSwimlanesExportCmd(clientFn),
		newSwimlanesApplyCmd(clientFn),
		newSwimlanesSetStrategyCmd(clientFn),
		newSwimlanesAddCmd(clientFn),
		newSwimlanesUpdateCmd(clientFn),
		newSwimlanesDeleteCmd(clientFn),
		newSwimlanesMoveCmd(clientFn),
		newSwimlanesValidateCmd(clientFn),
		newSwimlanesSimulateCmd(clientFn),
	)

	return cmd
}

// ── helpers ─────────────────────────────────────────────────────────

func requireExperimental(cmd *cobra.Command) error {
	experimental, _ := cmd.Flags().GetBool("experimental")
	if !experimental {
		// Walk up to the parent to check for the flag.
		for p := cmd.Parent(); p != nil; p = p.Parent() {
			if f := p.Flags().Lookup("experimental"); f != nil {
				if f.Value.String() == "true" {
					return nil
				}
			}
		}
		if !experimental {
			return &cerrors.CojiraError{
				Code:        cerrors.Unsupported,
				Message:     "This command is experimental and requires --experimental.",
				UserMessage: "This command is experimental. Re-run with `cojira jira --experimental ...`.",
				ExitCode:    2,
			}
		}
	}
	return nil
}

func resolveBoardID(raw string) (string, error) {
	boardID := ResolveBoardIdentifier(raw)
	matched, _ := regexp.MatchString(`^\d+$`, boardID)
	if boardID == "" || !matched {
		return "", &cerrors.CojiraError{
			Code:     cerrors.IdentUnresolved,
			Message:  fmt.Sprintf("Could not resolve board id from: %q", raw),
			Hint:     "Provide a numeric board id or a RapidView URL containing rapidView=<id>.",
			ExitCode: 2,
		}
	}
	return boardID, nil
}

func parseSwimlaneID(raw string) (int, error) {
	s := strings.TrimSpace(raw)
	n, err := strconv.Atoi(s)
	if err != nil || n < 0 {
		return 0, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Invalid swimlane id: %q", raw),
			ExitCode: 2,
		}
	}
	return n, nil
}

func greenhopperURL(baseURL, path string) string {
	base := strings.TrimRight(baseURL, "/")
	rel := strings.TrimLeft(path, "/")
	return fmt.Sprintf("%s%s/%s", base, jira.GreenhopperBase, rel)
}

func ghGetEditmodel(client *jira.Client, boardID string) (map[string]any, error) {
	u := greenhopperURL(client.BaseURL(), "rapidviewconfig/editmodel")
	params := url.Values{"rapidViewId": {boardID}}
	resp, err := client.RequestURL("GET", u, nil, params)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "Unexpected response from GreenHopper editmodel.",
			ExitCode: 1,
		}
	}
	return data, nil
}

func ghSwimlaneURL(client *jira.Client, boardID string, swimlaneID *int) string {
	if swimlaneID == nil {
		return greenhopperURL(client.BaseURL(), fmt.Sprintf("swimlanes/%s", boardID))
	}
	return greenhopperURL(client.BaseURL(), fmt.Sprintf("swimlanes/%s/%d", boardID, *swimlaneID))
}

func ghRequestJSON(client *jira.Client, method, u string, payload any) (map[string]any, error) {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return nil, err
		}
	}
	resp, err := client.RequestURL(method, u, body, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	return data, nil
}

func ghRequestNoBody(client *jira.Client, method, u string, payload any) error {
	var body []byte
	if payload != nil {
		var err error
		body, err = json.Marshal(payload)
		if err != nil {
			return err
		}
	}
	resp, err := client.RequestURL(method, u, body, nil)
	if err != nil {
		return err
	}
	_ = resp.Body.Close()
	return nil
}

func isDryRun(cmd *cobra.Command) bool {
	dr, _ := cmd.Flags().GetBool("dry-run")
	diff, _ := cmd.Flags().GetBool("diff")
	preview, _ := cmd.Flags().GetBool("preview")
	return dr || diff || preview
}

func isQuiet(cmd *cobra.Command) bool {
	q, _ := cmd.Flags().GetBool("quiet")
	return q
}

func sleepIfNeeded(cmd *cobra.Command) {
	s, _ := cmd.Flags().GetFloat64("sleep")
	if s > 0 {
		time.Sleep(time.Duration(s * float64(time.Second)))
	}
}

func printOpsHuman(ops []map[string]any) {
	if len(ops) == 0 {
		fmt.Println("No changes.")
		return
	}
	fmt.Println("Planned operations:")
	for _, op := range ops {
		action, _ := op["action"].(string)
		if action == "reorder" {
			fmt.Println("  - reorder: will reorder swimlanes to match file order")
			continue
		}
		fmt.Printf("  - %s: %v\n", action, op)
	}
}

func emitEnvelope(ok bool, command string, target map[string]any, result any, warnings []any, exitCode *int) {
	env := output.BuildEnvelope(ok, "jira", command, target, result, warnings, nil, "", "", "", exitCode)
	_ = output.PrintJSON(env)
}

// ── get ─────────────────────────────────────────────────────────────

func newSwimlanesGetCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "get <board>",
		Short:         "Get swimlane config for a board",
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
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			editModel, err := ghGetEditmodel(client, boardID)
			if err != nil {
				return err
			}
			cfg, err := ExtractSwimlanesConfig(editModel)
			if err != nil {
				return err
			}

			lanes := cfg.Swimlanes
			strategy := cfg.Strategy
			canEdit := cfg.CanEdit

			if mode == "json" {
				lanesJSON := make([]map[string]any, len(lanes))
				for i, lane := range lanes {
					lanesJSON[i] = lane.ToJSON()
				}
				result := map[string]any{
					"swimlaneStrategy": strategy,
					"canEdit":          canEdit,
					"swimlanes":        lanesJSON,
					"summary":          map[string]any{"count": len(lanes)},
				}
				emitEnvelope(true, "board-swimlanes get", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}

			if mode == "summary" {
				fmt.Printf("Board %s swimlanes: strategy=%s lanes=%d canEdit=%t.\n", boardID, strategy, len(lanes), canEdit)
				return nil
			}

			// Human mode
			fmt.Printf("Board %s swimlanes (strategy=%s, canEdit=%t):\n", boardID, strategy, canEdit)
			for _, lane := range lanes {
				laneID := "-"
				if lane.ID != nil {
					laneID = strconv.Itoa(*lane.ID)
				}
				def := ""
				if lane.IsDefault {
					def = " (default)"
				}
				query := strings.TrimSpace(lane.Query)
				if query == "" {
					query = "-"
				}
				fmt.Printf("  [%s] %s%s | %s\n", laneID, lane.Name, def, query)
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	return cmd
}

// ── export ──────────────────────────────────────────────────────────

func newSwimlanesExportCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "export <board>",
		Short:         "Export swimlane config to JSON file",
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
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			editModel, err := ghGetEditmodel(client, boardID)
			if err != nil {
				return err
			}
			cfg, err := ExtractSwimlanesConfig(editModel)
			if err != nil {
				return err
			}

			lanes := cfg.Swimlanes
			lanesJSON := make([]map[string]any, len(lanes))
			for i, lane := range lanes {
				lanesJSON[i] = lane.ToJSON()
			}
			exportData := map[string]any{
				"swimlaneStrategy": cfg.Strategy,
				"swimlanes":        lanesJSON,
			}

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
					"summary":  map[string]any{"count": len(lanes)},
				}
				emitEnvelope(true, "board-swimlanes export", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}

			if mode == "summary" {
				fmt.Printf("Exported %d swimlane(s) for board %s to %s.\n", len(lanes), boardID, outPath)
				return nil
			}

			fmt.Printf("Saved swimlane config to: %s\n", outPath)
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().StringP("output", "o", "swimlanes.json", "Output file path")
	return cmd
}

// ── apply ───────────────────────────────────────────────────────────

func newSwimlanesApplyCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "apply <board>",
		Short:         "Apply swimlane config from JSON file",
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
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			desired, err := LoadDesiredSwimlanesFile(filePath)
			if err != nil {
				return err
			}

			editModel, err := ghGetEditmodel(client, boardID)
			if err != nil {
				return err
			}
			currentCfg, err := ExtractSwimlanesConfig(editModel)
			if err != nil {
				return err
			}
			if !currentCfg.CanEdit {
				return &cerrors.CojiraError{
					Code:     cerrors.HTTP403,
					Message:  "Board swimlanes are not editable with this token/user.",
					Hint:     cerrors.HintPermission(),
					ExitCode: 1,
				}
			}

			plan, err := BuildApplyPlan(currentCfg, desired, deleteMissing)
			if err != nil {
				return err
			}
			ops := make([]map[string]any, len(plan.Ops))
			copy(ops, plan.Ops)

			// Compute current IDs for move operations.
			currentIDsAll := make([]int, 0)
			for _, lane := range currentCfg.Swimlanes {
				if lane.ID != nil {
					currentIDsAll = append(currentIDsAll, *lane.ID)
				}
			}
			if deleteMissing {
				deleteSet := make(map[int]bool)
				for _, id := range plan.Deletes {
					deleteSet[id] = true
				}
				filtered := make([]int, 0)
				for _, id := range currentIDsAll {
					if !deleteSet[id] {
						filtered = append(filtered, id)
					}
				}
				currentIDsAll = filtered
			}

			creates := plan.Creates
			var moveOps []MoveOp
			if len(creates) == 0 {
				byName, dupes := uniqueByName(currentCfg.Swimlanes)

				desiredIDs := make([]int, 0)
				for _, lane := range desired.Swimlanes {
					if lane.ID != nil {
						desiredIDs = append(desiredIDs, *lane.ID)
						continue
					}
					if !dupes[lane.Name] {
						if cur, ok := byName[lane.Name]; ok && cur.ID != nil {
							desiredIDs = append(desiredIDs, *cur.ID)
						}
					}
				}
				finalIDs := append(desiredIDs, plan.Reorder.Extras...)
				moveOps = ComputeMoveOps(currentIDsAll, finalIDs)
				for _, mop := range moveOps {
					opMap := map[string]any{"action": "move", "id": mop.ID}
					if mop.Position != "" {
						opMap["position"] = mop.Position
					}
					if mop.AfterID != nil {
						opMap["afterId"] = *mop.AfterID
					}
					ops = append(ops, opMap)
				}
			} else {
				ops = append(ops, map[string]any{"action": "reorder"})
			}

			summary := map[string]any{
				"create":  plan.Summary.Create,
				"update":  plan.Summary.Update,
				"delete":  plan.Summary.Delete,
				"move":    len(moveOps),
				"dry_run": isDryRun(cmd),
			}
			if len(creates) > 0 && len(moveOps) == 0 {
				summary["move"] = len(desired.Swimlanes)
			}

			strategyResult := map[string]any{
				"from":    plan.Strategy.From,
				"to":      plan.Strategy.To,
				"changed": plan.Strategy.Changed,
			}

			dryRun := isDryRun(cmd)

			if dryRun {
				if mode == "json" {
					result := map[string]any{
						"dry_run":  true,
						"strategy": strategyResult,
						"ops":      ops,
						"summary":  summary,
					}
					emitEnvelope(true, "board-swimlanes apply", map[string]any{"board": boardID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Would apply swimlane config to board %s.\n", boardID)
					return nil
				}
				printOpsHuman(ops)
				if !isQuiet(cmd) {
					r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would apply swimlanes to board %s", boardID)}
					fmt.Println(r.Format())
				}
				return nil
			}

			// Execute the plan.
			if err := executeSwimlanesApply(client, cmd, boardID, desired, currentCfg, plan, currentIDsAll); err != nil {
				// Best-effort verification: if the board now matches desired, treat as success.
				if verifyErr := verifySwimlanesApply(client, boardID, desired, deleteMissing, currentIDsAll, mode, cmd, strategyResult, ops, summary); verifyErr == nil {
					return nil
				}
				return err
			}

			if mode == "json" {
				result := map[string]any{
					"dry_run":  false,
					"strategy": strategyResult,
					"ops":      ops,
					"summary":  summary,
				}
				emitEnvelope(true, "board-swimlanes apply", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}
			if mode == "summary" {
				fmt.Printf("Applied swimlane config to board %s.\n", boardID)
				return nil
			}
			if !isQuiet(cmd) {
				r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Applied swimlanes to board %s", boardID)}
				fmt.Println(r.Format())
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().String("file", "", "Desired swimlane config JSON file")
	_ = cmd.MarkFlagRequired("file")
	cmd.Flags().Bool("delete-missing", false, "Delete existing swimlanes not in file")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	cmd.Flags().Bool("diff", false, "Show diff of changes")
	cmd.Flags().Bool("preview", false, "Show preview of changes")
	cmd.Flags().Float64("sleep", 0.0, "Delay between API calls in seconds")
	return cmd
}

func executeSwimlanesApply(client *jira.Client, cmd *cobra.Command, boardID string, desired, currentCfg *SwimlanesConfig, plan *ApplyPlan, currentIDsAll []int) error {
	createdIDs := make(map[string]int)
	desiredLanes := desired.Swimlanes
	var desiredDefault *Swimlane
	for i := range desiredLanes {
		if desiredLanes[i].IsDefault {
			desiredDefault = &desiredLanes[i]
			break
		}
	}
	deleteMissing, _ := cmd.Flags().GetBool("delete-missing")

	// Strategy change.
	if plan.Strategy.Changed {
		u := greenhopperURL(client.BaseURL(), "swimlaneStrategy")
		boardIDInt, _ := strconv.Atoi(boardID)
		if err := ghRequestNoBody(client, "PUT", u, map[string]any{
			"id":                 boardIDInt,
			"swimlaneStrategyId": plan.Strategy.To,
		}); err != nil {
			return err
		}
		sleepIfNeeded(cmd)
	}

	// Create lanes (always as non-default; set default via update later).
	for _, lane := range plan.Creates {
		payload := lane.ToJSON()
		delete(payload, "id")
		payload["isDefault"] = false
		u := ghSwimlaneURL(client, boardID, nil)
		data, err := ghRequestJSON(client, "POST", u, payload)
		if err != nil {
			return err
		}
		if data == nil || data["id"] == nil {
			return &cerrors.CojiraError{
				Code:     cerrors.CreateFailed,
				Message:  "GreenHopper create swimlane did not return an id.",
				ExitCode: 1,
			}
		}
		createdLane, err := SwimlaneFromAPI(data)
		if err != nil {
			return err
		}
		if createdLane.ID == nil {
			return &cerrors.CojiraError{
				Code:     cerrors.CreateFailed,
				Message:  "GreenHopper create swimlane returned invalid id.",
				ExitCode: 1,
			}
		}
		createdIDs[lane.Name] = *createdLane.ID
		sleepIfNeeded(cmd)
	}

	// Update existing lanes.
	for _, upd := range plan.Updates {
		if upd.ID == nil {
			continue
		}
		payload := upd.Desired.ToJSON()
		payload["id"] = *upd.ID
		u := ghSwimlaneURL(client, boardID, upd.ID)
		if err := ghRequestNoBody(client, "PUT", u, payload); err != nil {
			return err
		}
		sleepIfNeeded(cmd)
	}

	// If the desired default was created, set it default explicitly.
	if desiredDefault != nil && desiredDefault.IsDefault {
		if defaultID, ok := createdIDs[desiredDefault.Name]; ok {
			payload := desiredDefault.ToJSON()
			payload["id"] = defaultID
			u := ghSwimlaneURL(client, boardID, &defaultID)
			if err := ghRequestNoBody(client, "PUT", u, payload); err != nil {
				return err
			}
			sleepIfNeeded(cmd)
		}
	}

	// Deletes.
	for _, laneID := range plan.Deletes {
		id := laneID
		u := ghSwimlaneURL(client, boardID, &id)
		resp, err := client.RequestURL("DELETE", u, nil, nil)
		if err != nil {
			return err
		}
		_ = resp.Body.Close()
		sleepIfNeeded(cmd)
	}

	// Reorder lanes to file order, then append extras (stable).
	byName, dupes := uniqueByName(currentCfg.Swimlanes)
	desiredIDsFinal := make([]int, 0)
	for _, lane := range desiredLanes {
		if lane.ID != nil {
			desiredIDsFinal = append(desiredIDsFinal, *lane.ID)
			continue
		}
		if cid, ok := createdIDs[lane.Name]; ok {
			desiredIDsFinal = append(desiredIDsFinal, cid)
			continue
		}
		if !dupes[lane.Name] {
			if cur, ok := byName[lane.Name]; ok && cur.ID != nil {
				desiredIDsFinal = append(desiredIDsFinal, *cur.ID)
			}
		}
	}

	// Remove deleted IDs from currentIDsAll if delete-missing.
	if deleteMissing {
		deleteSet := make(map[int]bool)
		for _, id := range plan.Deletes {
			deleteSet[id] = true
		}
		filtered := make([]int, 0)
		for _, id := range currentIDsAll {
			if !deleteSet[id] {
				filtered = append(filtered, id)
			}
		}
		currentIDsAll = filtered
	}

	finalIDs := append(desiredIDsFinal, plan.Reorder.Extras...)
	moveOpsApply := ComputeMoveOps(currentIDsAll, finalIDs)
	for _, mop := range moveOpsApply {
		moveURL := greenhopperURL(client.BaseURL(), fmt.Sprintf("swimlanes/%s/%d/move", boardID, mop.ID))
		if mop.Position == "First" {
			if err := ghRequestNoBody(client, "POST", moveURL, map[string]any{"position": "First"}); err != nil {
				return err
			}
		} else if mop.AfterID != nil {
			afterURI := ghSwimlaneURL(client, boardID, mop.AfterID)
			if err := ghRequestNoBody(client, "POST", moveURL, map[string]any{"after": afterURI}); err != nil {
				return err
			}
		}
		sleepIfNeeded(cmd)
	}

	return nil
}

func verifySwimlanesApply(client *jira.Client, boardID string, desired *SwimlanesConfig, deleteMissing bool, currentIDsAll []int, mode string, cmd *cobra.Command, strategyResult map[string]any, ops []map[string]any, summary map[string]any) error {
	editModel, err := ghGetEditmodel(client, boardID)
	if err != nil {
		return err
	}
	verifyCfg, err := ExtractSwimlanesConfig(editModel)
	if err != nil {
		return err
	}
	verifyPlan, err := BuildApplyPlan(verifyCfg, desired, deleteMissing)
	if err != nil {
		return err
	}

	if verifyPlan.Strategy.Changed || len(verifyPlan.Creates) > 0 || len(verifyPlan.Updates) > 0 || len(verifyPlan.Deletes) > 0 {
		return fmt.Errorf("verification failed")
	}

	// Check moves.
	if len(verifyPlan.Creates) == 0 {
		verifyCurrentIDs := make([]int, 0)
		for _, lane := range verifyCfg.Swimlanes {
			if lane.ID != nil {
				verifyCurrentIDs = append(verifyCurrentIDs, *lane.ID)
			}
		}
		if deleteMissing {
			deleteSet := make(map[int]bool)
			for _, id := range verifyPlan.Deletes {
				deleteSet[id] = true
			}
			filtered := make([]int, 0)
			for _, id := range verifyCurrentIDs {
				if !deleteSet[id] {
					filtered = append(filtered, id)
				}
			}
			verifyCurrentIDs = filtered
		}

		byName, dupes := uniqueByName(verifyCfg.Swimlanes)
		desiredIDs := make([]int, 0)
		for _, lane := range desired.Swimlanes {
			if lane.ID != nil {
				desiredIDs = append(desiredIDs, *lane.ID)
				continue
			}
			if !dupes[lane.Name] {
				if cur, ok := byName[lane.Name]; ok && cur.ID != nil {
					desiredIDs = append(desiredIDs, *cur.ID)
				}
			}
		}
		finalIDs := append(desiredIDs, verifyPlan.Reorder.Extras...)
		verifyMoveOps := ComputeMoveOps(verifyCurrentIDs, finalIDs)
		if len(verifyMoveOps) > 0 {
			return fmt.Errorf("verification failed: moves needed")
		}
	}

	// Board matches desired state - emit success.
	warn := "Applied swimlanes, but got a transient error while updating. The board now matches the desired configuration."
	if mode == "json" {
		result := map[string]any{
			"dry_run":              false,
			"strategy":             strategyResult,
			"ops":                  ops,
			"summary":              summary,
			"verified_after_error": true,
		}
		emitEnvelope(true, "board-swimlanes apply", map[string]any{"board": boardID}, result, []any{warn}, nil)
		return nil
	}
	if mode == "summary" {
		fmt.Printf("Applied swimlane config to board %s (verified).\n", boardID)
		return nil
	}
	if !isQuiet(cmd) {
		r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Applied swimlane config to board %s (verified)", boardID)}
		fmt.Println(r.Format())
	}
	return nil
}

// ── set-strategy ────────────────────────────────────────────────────

func newSwimlanesSetStrategyCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "set-strategy <board>",
		Short:         "Set swimlane strategy for a board",
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
			strategy, _ := cmd.Flags().GetString("strategy")
			strategy = strings.TrimSpace(strategy)
			if strategy == "" {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "--strategy is required.",
					ExitCode: 2,
				}
			}
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				if mode == "json" {
					result := map[string]any{"dry_run": true, "strategy": strategy}
					emitEnvelope(true, "board-swimlanes set-strategy", map[string]any{"board": boardID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Would set swimlane strategy for board %s to %s.\n", boardID, strategy)
					return nil
				}
				r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would set strategy=%s for board %s", strategy, boardID)}
				fmt.Println(r.Format())
				return nil
			}

			u := greenhopperURL(client.BaseURL(), "swimlaneStrategy")
			boardIDInt, _ := strconv.Atoi(boardID)
			if err := ghRequestNoBody(client, "PUT", u, map[string]any{
				"id":                 boardIDInt,
				"swimlaneStrategyId": strategy,
			}); err != nil {
				return err
			}

			if mode == "json" {
				result := map[string]any{"dry_run": false, "strategy": strategy}
				emitEnvelope(true, "board-swimlanes set-strategy", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}
			if mode == "summary" {
				fmt.Printf("Set swimlane strategy for board %s to %s.\n", boardID, strategy)
				return nil
			}
			if !isQuiet(cmd) {
				r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Set strategy=%s for board %s", strategy, boardID)}
				fmt.Println(r.Format())
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().String("strategy", "", "Swimlane strategy (e.g., custom, assignees, none)")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	return cmd
}

// ── add ─────────────────────────────────────────────────────────────

func newSwimlanesAddCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "add <board>",
		Short:         "Add a new swimlane to a board",
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
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}
			name, _ := cmd.Flags().GetString("name")
			name = strings.TrimSpace(name)
			if name == "" {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "--name is required.",
					ExitCode: 2,
				}
			}
			query, _ := cmd.Flags().GetString("query")
			description, _ := cmd.Flags().GetString("description")
			isDefault, _ := cmd.Flags().GetBool("default")

			payload := map[string]any{
				"name":        name,
				"query":       query,
				"description": description,
				"isDefault":   isDefault,
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				if mode == "json" {
					result := map[string]any{"dry_run": true, "lane": payload}
					emitEnvelope(true, "board-swimlanes add", map[string]any{"board": boardID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Would add swimlane %q to board %s.\n", name, boardID)
					return nil
				}
				r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would add swimlane %q to board %s", name, boardID)}
				fmt.Println(r.Format())
				return nil
			}

			u := ghSwimlaneURL(client, boardID, nil)
			data, err := ghRequestJSON(client, "POST", u, payload)
			if err != nil {
				return err
			}
			var laneID any
			if data != nil {
				laneID = data["id"]
			}

			if mode == "json" {
				result := map[string]any{"id": laneID, "lane": data}
				emitEnvelope(true, "board-swimlanes add", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}
			if mode == "summary" {
				fmt.Printf("Added swimlane %q to board %s.\n", name, boardID)
				return nil
			}
			if !isQuiet(cmd) {
				r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Added swimlane %q to board %s", name, boardID)}
				fmt.Println(r.Format())
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().String("name", "", "Swimlane name")
	cmd.Flags().String("query", "", "JQL query for swimlane")
	cmd.Flags().String("description", "", "Swimlane description")
	cmd.Flags().Bool("default", false, "Set as default swimlane")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	return cmd
}

// ── update ──────────────────────────────────────────────────────────

func newSwimlanesUpdateCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "update <board> <swimlane-id>",
		Short:         "Update an existing swimlane",
		Args:          cobra.ExactArgs(2),
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
			laneID, err := parseSwimlaneID(args[1])
			if err != nil {
				return err
			}
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			u := ghSwimlaneURL(client, boardID, &laneID)
			currentData, err := ghRequestJSON(client, "GET", u, nil)
			if err != nil {
				return err
			}
			curLane, err := SwimlaneFromAPI(currentData)
			if err != nil {
				return err
			}

			newName := curLane.Name
			if cmd.Flags().Changed("name") {
				n, _ := cmd.Flags().GetString("name")
				newName = strings.TrimSpace(n)
			}
			newQuery := curLane.Query
			if cmd.Flags().Changed("query") {
				newQuery, _ = cmd.Flags().GetString("query")
			}
			newDesc := curLane.Description
			if cmd.Flags().Changed("description") {
				newDesc, _ = cmd.Flags().GetString("description")
			}
			newDefault := curLane.IsDefault
			if cmd.Flags().Changed("default") {
				d, _ := cmd.Flags().GetString("default")
				newDefault = d == "true"
			}

			newLane := Swimlane{
				ID:          &laneID,
				Name:        newName,
				Query:       newQuery,
				Description: newDesc,
				IsDefault:   newDefault,
			}

			changedFields := diffLane(curLane, newLane)

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				if mode == "json" {
					result := map[string]any{"dry_run": true, "diff": changedFields}
					emitEnvelope(true, "board-swimlanes update", map[string]any{"board": boardID, "swimlane_id": laneID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Would update swimlane %d on board %s.\n", laneID, boardID)
					return nil
				}
				if !isQuiet(cmd) {
					fields := make(map[string]any)
					for k, v := range changedFields {
						fields[k] = v
					}
					printOpsHuman([]map[string]any{{"action": "update", "id": laneID, "fields": fields}})
					r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would update swimlane %d on board %s", laneID, boardID)}
					fmt.Println(r.Format())
				}
				return nil
			}

			payload := newLane.ToJSON()
			payload["id"] = laneID
			if err := ghRequestNoBody(client, "PUT", u, payload); err != nil {
				return err
			}

			if mode == "json" {
				result := map[string]any{"diff": changedFields}
				emitEnvelope(true, "board-swimlanes update", map[string]any{"board": boardID, "swimlane_id": laneID}, result, nil, nil)
				return nil
			}
			if mode == "summary" {
				fmt.Printf("Updated swimlane %d on board %s.\n", laneID, boardID)
				return nil
			}
			if !isQuiet(cmd) {
				r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Updated swimlane %d on board %s", laneID, boardID)}
				fmt.Println(r.Format())
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().String("name", "", "New swimlane name")
	cmd.Flags().String("query", "", "New JQL query")
	cmd.Flags().String("description", "", "New description")
	cmd.Flags().String("default", "", "Set isDefault (true/false)")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	return cmd
}

// ── delete ──────────────────────────────────────────────────────────

func newSwimlanesDeleteCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "delete <board> <swimlane-id>",
		Short:         "Delete a swimlane from a board",
		Args:          cobra.ExactArgs(2),
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
			laneID, err := parseSwimlaneID(args[1])
			if err != nil {
				return err
			}
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			// Check if this is the default swimlane.
			u := ghSwimlaneURL(client, boardID, &laneID)
			currentData, err := ghRequestJSON(client, "GET", u, nil)
			if err != nil {
				return err
			}
			curLane, err := SwimlaneFromAPI(currentData)
			if err == nil && curLane.IsDefault {
				return &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  "Refusing to delete the default swimlane.",
					ExitCode: 2,
				}
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				if mode == "json" {
					result := map[string]any{"dry_run": true}
					emitEnvelope(true, "board-swimlanes delete", map[string]any{"board": boardID, "swimlane_id": laneID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Would delete swimlane %d from board %s.\n", laneID, boardID)
					return nil
				}
				if !isQuiet(cmd) {
					r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would delete swimlane %d from board %s", laneID, boardID)}
					fmt.Println(r.Format())
				}
				return nil
			}

			resp, err := client.RequestURL("DELETE", u, nil, nil)
			if err != nil {
				return err
			}
			_ = resp.Body.Close()

			if mode == "json" {
				result := map[string]any{"deleted": true}
				emitEnvelope(true, "board-swimlanes delete", map[string]any{"board": boardID, "swimlane_id": laneID}, result, nil, nil)
				return nil
			}
			if mode == "summary" {
				fmt.Printf("Deleted swimlane %d from board %s.\n", laneID, boardID)
				return nil
			}
			if !isQuiet(cmd) {
				r := output.Receipt{OK: true, DryRun: false, Message: fmt.Sprintf("Deleted swimlane %d from board %s", laneID, boardID)}
				fmt.Println(r.Format())
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	return cmd
}

// ── move ────────────────────────────────────────────────────────────

func newSwimlanesMoveCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "move <board> <swimlane-id>",
		Short:         "Move a swimlane to a new position",
		Args:          cobra.ExactArgs(2),
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
			laneID, err := parseSwimlaneID(args[1])
			if err != nil {
				return err
			}
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			first, _ := cmd.Flags().GetBool("first")
			after, _ := cmd.Flags().GetString("after")

			var payload map[string]any
			var msg string
			if first {
				payload = map[string]any{"position": "First"}
				msg = fmt.Sprintf("Move swimlane %d to first on board %s", laneID, boardID)
			} else {
				afterID, err := parseSwimlaneID(after)
				if err != nil {
					return err
				}
				afterURI := ghSwimlaneURL(client, boardID, &afterID)
				payload = map[string]any{"after": afterURI}
				msg = fmt.Sprintf("Move swimlane %d after %d on board %s", laneID, afterID, boardID)
			}

			dryRun, _ := cmd.Flags().GetBool("dry-run")
			if dryRun {
				if mode == "json" {
					result := map[string]any{"dry_run": true, "payload": payload}
					emitEnvelope(true, "board-swimlanes move", map[string]any{"board": boardID, "swimlane_id": laneID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Would %s.\n", strings.ToLower(msg))
					return nil
				}
				if !isQuiet(cmd) {
					r := output.Receipt{OK: true, DryRun: true, Message: fmt.Sprintf("Would %s", msg)}
					fmt.Println(r.Format())
				}
				return nil
			}

			moveURL := greenhopperURL(client.BaseURL(), fmt.Sprintf("swimlanes/%s/%d/move", boardID, laneID))
			if err := ghRequestNoBody(client, "POST", moveURL, payload); err != nil {
				return err
			}

			if mode == "json" {
				result := map[string]any{"payload": payload}
				emitEnvelope(true, "board-swimlanes move", map[string]any{"board": boardID, "swimlane_id": laneID}, result, nil, nil)
				return nil
			}
			if mode == "summary" {
				fmt.Printf("%s.\n", msg)
				return nil
			}
			if !isQuiet(cmd) {
				r := output.Receipt{OK: true, DryRun: false, Message: msg}
				fmt.Println(r.Format())
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().Bool("first", false, "Move swimlane to the first position")
	cmd.Flags().String("after", "", "Move swimlane after this ID")
	cmd.Flags().Bool("dry-run", false, "Preview changes without applying")
	return cmd
}

// ── validate ────────────────────────────────────────────────────────

func newSwimlanesValidateCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "validate <board>",
		Short:         "Validate swimlane JQL queries by running them against Jira",
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
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			var lanes []Swimlane
			var source string
			filePath, _ := cmd.Flags().GetString("file")
			if filePath != "" {
				desired, err := LoadDesiredSwimlanesFile(filePath)
				if err != nil {
					return err
				}
				lanes = desired.Swimlanes
				source = "file"
			} else {
				editModel, err := ghGetEditmodel(client, boardID)
				if err != nil {
					return err
				}
				cfg, err := ExtractSwimlanesConfig(editModel)
				if err != nil {
					return err
				}
				lanes = cfg.Swimlanes
				source = "board"
			}

			type validationResult struct {
				ID      *int   `json:"id"`
				Name    string `json:"name"`
				OK      bool   `json:"ok"`
				Skipped bool   `json:"skipped,omitempty"`
				Reason  string `json:"reason,omitempty"`
				Error   string `json:"error,omitempty"`
			}

			var results []validationResult
			var failures []validationResult
			for _, lane := range lanes {
				q := strings.TrimSpace(lane.Query)
				if q == "" {
					results = append(results, validationResult{ID: lane.ID, Name: lane.Name, OK: true, Skipped: true, Reason: "empty query"})
					continue
				}
				_, searchErr := client.Search(q, 0, 0, "id", "")
				row := validationResult{ID: lane.ID, Name: lane.Name, OK: searchErr == nil}
				if searchErr != nil {
					row.Error = searchErr.Error()
					failures = append(failures, row)
				}
				results = append(results, row)
				sleepIfNeeded(cmd)
			}

			okAll := true
			for _, r := range results {
				if !r.OK {
					okAll = false
					break
				}
			}

			okCount := 0
			failCount := 0
			skipCount := 0
			for _, r := range results {
				if r.Skipped {
					skipCount++
				}
				if r.OK {
					okCount++
				} else {
					failCount++
				}
			}
			summaryMap := map[string]any{
				"total":   len(results),
				"ok":      okCount,
				"failed":  failCount,
				"skipped": skipCount,
			}

			if mode == "json" {
				// Build result rows as maps for JSON output.
				var rows []map[string]any
				for _, r := range results {
					row := map[string]any{
						"id":      r.ID,
						"name":    r.Name,
						"ok":      r.OK,
						"skipped": r.Skipped,
					}
					if r.Reason != "" {
						row["reason"] = r.Reason
					}
					if r.Error != "" {
						row["error"] = r.Error
					}
					rows = append(rows, row)
				}

				var errObjs []any
				for _, f := range failures {
					obj, _ := output.ErrorObj(cerrors.OpFailed, fmt.Sprintf("%s: %s", f.Name, f.Error), "", "", nil)
					if obj != nil {
						errObjs = append(errObjs, obj)
					}
				}

				exitCode := 0
				if !okAll {
					exitCode = 1
				}
				env := output.BuildEnvelope(okAll, "jira", "board-swimlanes validate",
					map[string]any{"board": boardID, "source": source},
					map[string]any{"lanes": rows, "summary": summaryMap},
					nil, errObjs, "", "", "", &exitCode)
				_ = output.PrintJSON(env)
				if !okAll {
					return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Validation failed", ExitCode: 1}
				}
				return nil
			}

			if mode == "summary" {
				fmt.Printf("Validated swimlane queries for board %s: %d ok, %d failed.\n", boardID, okCount, failCount)
				if !okAll {
					return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Validation failed", ExitCode: 1}
				}
				return nil
			}

			fmt.Printf("Board %s swimlane query validation (%s):\n", boardID, source)
			for _, r := range results {
				status := "OK"
				if !r.OK {
					status = "INVALID"
				}
				if r.Skipped {
					status = "SKIP"
				}
				fmt.Printf("  - %s: %s\n", r.Name, status)
			}
			if !okAll {
				return &cerrors.CojiraError{Code: cerrors.OpFailed, Message: "Validation failed", ExitCode: 1}
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().String("file", "", "Validate lanes from a JSON file instead of from the board")
	cmd.Flags().Float64("sleep", 0.0, "Delay between JQL evaluations in seconds")
	return cmd
}

// ── simulate ────────────────────────────────────────────────────────

func newSwimlanesSimulateCmd(clientFn func(cmd *cobra.Command) (*jira.Client, error)) *cobra.Command {
	cmd := &cobra.Command{
		Use:           "simulate <board>",
		Short:         "Simulate swimlane routing for all board issues",
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
			client, err := clientFn(cmd)
			if err != nil {
				return err
			}

			editModel, err := ghGetEditmodel(client, boardID)
			if err != nil {
				return err
			}
			cfg, err := ExtractSwimlanesConfig(editModel)
			if err != nil {
				return err
			}
			lanes := cfg.Swimlanes
			if len(lanes) == 0 {
				if mode == "json" {
					result := map[string]any{"lanes": []any{}, "issues": []any{}, "summary": map[string]any{"total_issues": 0}}
					emitEnvelope(true, "board-swimlanes simulate", map[string]any{"board": boardID}, result, nil, nil)
					return nil
				}
				if mode == "summary" {
					fmt.Printf("Board %s has 0 swimlanes to simulate.\n", boardID)
					return nil
				}
				fmt.Println("No swimlanes found on this board.")
				return nil
			}

			// Fetch all issues on the board.
			pageSize, _ := cmd.Flags().GetInt("page-size")
			if pageSize <= 0 {
				pageSize = 50
			}
			maxIssues, _ := cmd.Flags().GetInt("max-issues")
			chunkSize, _ := cmd.Flags().GetInt("chunk-size")
			if chunkSize <= 0 {
				chunkSize = 50
			}

			var boardIssues []map[string]any
			var total int
			truncated := false
			startAt := 0
			for {
				page, err := client.GetBoardIssues(boardID, "", pageSize, startAt, "summary,status,assignee")
				if err != nil {
					return err
				}
				pageIssues, _ := page["issues"].([]any)
				for _, item := range pageIssues {
					if m, ok := item.(map[string]any); ok {
						boardIssues = append(boardIssues, m)
					}
				}
				if maxIssues > 0 && len(boardIssues) >= maxIssues {
					boardIssues = boardIssues[:maxIssues]
					truncated = true
					break
				}
				if total == 0 {
					if t, ok := page["total"].(float64); ok {
						total = int(t)
					} else {
						total = len(boardIssues)
					}
				}
				startAt += len(pageIssues)
				if startAt >= total || len(pageIssues) == 0 {
					break
				}
			}

			issueKeys := make([]string, 0)
			for _, issue := range boardIssues {
				if key, ok := issue["key"].(string); ok && key != "" {
					issueKeys = append(issueKeys, key)
				}
			}
			issueKeysSet := make(map[string]bool)
			for _, key := range issueKeys {
				issueKeysSet[key] = true
			}

			var defaultLane *Swimlane
			var nonDefaultLanes []Swimlane
			for i := range lanes {
				if lanes[i].IsDefault {
					defaultLane = &lanes[i]
				} else {
					nonDefaultLanes = append(nonDefaultLanes, lanes[i])
				}
			}

			laneMatches := make(map[string]map[string]bool)
			matchesByIssue := make(map[string][]string)
			for _, key := range issueKeys {
				matchesByIssue[key] = []string{}
			}
			var laneErrors []map[string]any

			for _, lane := range nonDefaultLanes {
				laneJQL := stripJQLOrderBy(lane.Query)
				matched := make(map[string]bool)

				func() {
					defer func() {
						laneMatches[lane.Name] = matched
						for key := range matched {
							if issueKeysSet[key] {
								matchesByIssue[key] = append(matchesByIssue[key], lane.Name)
							}
						}
					}()

					chunks := chunkStrings(issueKeys, chunkSize)
					for _, chunk := range chunks {
						keyClause := ""
						if len(chunk) > 0 {
							keyClause = "key in (" + strings.Join(chunk, ",") + ")"
						}
						var jql string
						if laneJQL != "" && keyClause != "" {
							jql = "(" + laneJQL + ") AND (" + keyClause + ")"
						} else if keyClause != "" {
							jql = keyClause
						} else {
							jql = laneJQL
						}
						if jql == "" {
							continue
						}

						searchPageSize := len(chunk)
						if searchPageSize > 1000 {
							searchPageSize = 1000
						}
						if searchPageSize < 1 {
							searchPageSize = 1
						}
						start := 0
						var totalMatches int
						for {
							data, err := client.Search(jql, searchPageSize, start, "id", "")
							if err != nil {
								laneErrors = append(laneErrors, map[string]any{"id": lane.ID, "name": lane.Name, "error": err.Error()})
								return
							}
							issuesPage, _ := data["issues"].([]any)
							for _, item := range issuesPage {
								if m, ok := item.(map[string]any); ok {
									if key, ok := m["key"].(string); ok && key != "" {
										matched[key] = true
									}
								}
							}
							start += len(issuesPage)
							if totalMatches == 0 {
								if t, ok := data["total"].(float64); ok {
									totalMatches = int(t)
								} else {
									totalMatches = start
								}
							}
							if start >= totalMatches || len(issuesPage) == 0 {
								break
							}
						}
						sleepIfNeeded(cmd)
					}
				}()
			}

			// Assignment: first matching lane in board order wins; otherwise default.
			assignedLane := make(map[string]string)
			remaining := make(map[string]bool)
			for _, key := range issueKeys {
				remaining[key] = true
			}
			for _, lane := range nonDefaultLanes {
				matched := laneMatches[lane.Name]
				for _, key := range issueKeys {
					if remaining[key] && matched[key] {
						assignedLane[key] = lane.Name
						delete(remaining, key)
					}
				}
			}
			defaultName := "(default)"
			if defaultLane != nil {
				defaultName = defaultLane.Name
			}
			for _, key := range issueKeys {
				if remaining[key] {
					assignedLane[key] = defaultName
				}
			}

			ambiguous := make(map[string][]string)
			for k, v := range matchesByIssue {
				if len(v) > 1 {
					ambiguous[k] = v
				}
			}
			noMatch := make([]string, 0)
			for k, v := range matchesByIssue {
				if len(v) == 0 {
					noMatch = append(noMatch, k)
				}
			}

			lanesSummary := make([]map[string]any, 0)
			for _, lane := range lanes {
				count := 0
				for _, a := range assignedLane {
					if a == lane.Name {
						count++
					}
				}
				lanesSummary = append(lanesSummary, map[string]any{
					"id": lane.ID, "name": lane.Name, "isDefault": lane.IsDefault, "assigned": count,
				})
			}

			issuesOut := make([]map[string]any, 0)
			for _, issue := range boardIssues {
				key, _ := issue["key"].(string)
				if key == "" {
					continue
				}
				_, isAmbiguous := ambiguous[key]
				issuesOut = append(issuesOut, map[string]any{
					"key":              key,
					"assignedSwimlane": assignedLane[key],
					"matches":          matchesByIssue[key],
					"ambiguous":        isAmbiguous,
				})
			}

			defaultAssigned := 0
			for _, a := range assignedLane {
				if a == defaultName {
					defaultAssigned++
				}
			}

			summaryMap := map[string]any{
				"total_issues":     len(issueKeys),
				"board_total":      total,
				"truncated":        truncated,
				"maxIssues":        maxIssues,
				"ambiguous":        len(ambiguous),
				"no_match":         len(noMatch),
				"default_assigned": defaultAssigned,
				"lane_errors":      len(laneErrors),
			}

			if mode == "json" {
				ambiguousOut := make([]map[string]any, 0)
				for k, v := range ambiguous {
					ambiguousOut = append(ambiguousOut, map[string]any{"key": k, "matches": v})
				}
				result := map[string]any{
					"lanes":      lanesSummary,
					"issues":     issuesOut,
					"ambiguous":  ambiguousOut,
					"noMatch":    noMatch,
					"laneErrors": laneErrors,
					"summary":    summaryMap,
				}
				emitEnvelope(true, "board-swimlanes simulate", map[string]any{"board": boardID}, result, nil, nil)
				return nil
			}

			if mode == "summary" {
				if truncated {
					fmt.Printf("Simulated swimlane routing for board %s: %d of %d issues, %d ambiguous, %d unmatched.\n",
						boardID, len(issueKeys), total, len(ambiguous), len(noMatch))
				} else {
					fmt.Printf("Simulated swimlane routing for board %s: %d issues, %d ambiguous, %d unmatched.\n",
						boardID, len(issueKeys), len(ambiguous), len(noMatch))
				}
				return nil
			}

			fmt.Printf("Board %s swimlane routing simulation:\n", boardID)
			for _, lane := range lanesSummary {
				defaultMark := ""
				if lane["isDefault"] == true {
					defaultMark = " (default)"
				}
				fmt.Printf("  - %s%s: %d issue(s)\n", lane["name"], defaultMark, lane["assigned"])
			}
			if truncated {
				fmt.Printf("\nNote: simulated %d of %d issues (safety cap).\n", len(issueKeys), total)
			}
			if len(ambiguous) > 0 {
				fmt.Printf("\nAmbiguous (match multiple swimlanes): %d issue(s)\n", len(ambiguous))
			}
			if len(noMatch) > 0 {
				fmt.Printf("Unmatched (will fall to default): %d issue(s)\n", len(noMatch))
			}
			if len(laneErrors) > 0 {
				fmt.Printf("Warnings: %d swimlane(s) could not be evaluated due to API errors.\n", len(laneErrors))
			}
			return nil
		},
	}
	cli.AddOutputFlags(cmd, true)
	cmd.Flags().Int("page-size", 50, "Page size for fetching board issues")
	cmd.Flags().Int("max-issues", 2000, "Safety cap for simulation (0 for unlimited)")
	cmd.Flags().Int("chunk-size", 50, "Chunk size for JQL key-in queries")
	cmd.Flags().Float64("sleep", 0.0, "Delay between JQL evaluations in seconds")
	return cmd
}

// stripJQLOrderBy strips a trailing ORDER BY clause from JQL (outside quoted strings).
func stripJQLOrderBy(jql string) string {
	raw := strings.TrimSpace(jql)
	if raw == "" {
		return ""
	}
	// Best-effort: find ORDER BY outside of quoted strings.
	sanitized := stripJQLStrings(raw)
	re := regexp.MustCompile(`(?i)\border\s+by\b`)
	loc := re.FindStringIndex(sanitized)
	if loc == nil {
		return raw
	}
	return strings.TrimSpace(raw[:loc[0]])
}

func stripJQLStrings(jql string) string {
	var out []byte
	var quote byte
	for i := 0; i < len(jql); i++ {
		ch := jql[i]
		if quote != 0 {
			if ch == quote {
				quote = 0
			}
			out = append(out, ' ')
			continue
		}
		if ch == '\'' || ch == '"' {
			quote = ch
			out = append(out, ' ')
			continue
		}
		out = append(out, ch)
	}
	return string(out)
}

func chunkStrings(values []string, size int) [][]string {
	if size <= 0 {
		return [][]string{values}
	}
	var chunks [][]string
	for i := 0; i < len(values); i += size {
		end := i + size
		if end > len(values) {
			end = len(values)
		}
		chunks = append(chunks, values[i:end])
	}
	return chunks
}
