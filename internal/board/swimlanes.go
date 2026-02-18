package board

import (
	"fmt"
	"strings"

	cerrors "github.com/cojira/cojira/internal/errors"
)

// Swimlane represents a board swimlane configuration.
type Swimlane struct {
	ID          *int   `json:"id,omitempty"`
	Name        string `json:"name"`
	Query       string `json:"query"`
	Description string `json:"description"`
	IsDefault   bool   `json:"isDefault"`
}

// SwimlanesConfig holds the extracted swimlane configuration from GreenHopper.
type SwimlanesConfig struct {
	Strategy  string
	Swimlanes []Swimlane
	CanEdit   bool
}

// SwimlaneFromAPI creates a Swimlane from GreenHopper API response data.
func SwimlaneFromAPI(data map[string]any) (Swimlane, error) {
	var laneID *int
	if rawID := data["id"]; rawID != nil {
		n, err := coerceInt(rawID, "swimlane.id")
		if err != nil {
			return Swimlane{}, err
		}
		laneID = &n
	}
	return Swimlane{
		ID:          laneID,
		Name:        toString(data["name"]),
		Query:       toString(data["query"]),
		Description: toString(data["description"]),
		IsDefault:   toBool(data["isDefault"]),
	}, nil
}

// SwimlaneFromDesired creates a Swimlane from a user-provided desired config entry.
func SwimlaneFromDesired(data map[string]any, index int) (Swimlane, error) {
	if data == nil {
		return Swimlane{}, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  fmt.Sprintf("swimlanes[%d] must be an object.", index),
			ExitCode: 1,
		}
	}

	var laneID *int
	if rawID := data["id"]; rawID != nil {
		n, err := coerceInt(rawID, fmt.Sprintf("swimlanes[%d].id", index))
		if err != nil {
			return Swimlane{}, err
		}
		laneID = &n
	}

	rawName := data["name"]
	if rawName == nil {
		return Swimlane{}, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  fmt.Sprintf("Expected string for swimlanes[%d].name.", index),
			ExitCode: 1,
		}
	}
	name, err := coerceStr(rawName, fmt.Sprintf("swimlanes[%d].name", index))
	if err != nil {
		return Swimlane{}, err
	}
	name = trimString(name)
	if name == "" {
		return Swimlane{}, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  fmt.Sprintf("swimlanes[%d].name cannot be empty.", index),
			ExitCode: 1,
		}
	}

	queryRaw := data["query"]
	if queryRaw == nil {
		queryRaw = ""
	}
	query, err := coerceStr(queryRaw, fmt.Sprintf("swimlanes[%d].query", index))
	if err != nil {
		return Swimlane{}, err
	}

	descRaw := data["description"]
	if descRaw == nil {
		descRaw = ""
	}
	desc, err := coerceStr(descRaw, fmt.Sprintf("swimlanes[%d].description", index))
	if err != nil {
		return Swimlane{}, err
	}

	isDefault := false
	if rawDefault, exists := data["isDefault"]; exists {
		isDefault, err = coerceBool(rawDefault, fmt.Sprintf("swimlanes[%d].isDefault", index))
		if err != nil {
			return Swimlane{}, err
		}
	}

	return Swimlane{
		ID:          laneID,
		Name:        name,
		Query:       query,
		Description: desc,
		IsDefault:   isDefault,
	}, nil
}

// ToJSON converts a Swimlane to a JSON-serializable map.
func (s Swimlane) ToJSON() map[string]any {
	out := map[string]any{
		"name":        s.Name,
		"query":       s.Query,
		"description": s.Description,
		"isDefault":   s.IsDefault,
	}
	if s.ID != nil {
		out["id"] = *s.ID
	}
	return out
}

func trimString(s string) string {
	return strings.TrimSpace(s)
}

// ExtractSwimlanesConfig extracts swimlane config from a GreenHopper editmodel payload.
func ExtractSwimlanesConfig(editModel map[string]any) (*SwimlanesConfig, error) {
	cfgRaw, ok := editModel["swimlanesConfig"]
	if !ok {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "GreenHopper edit model did not include swimlanesConfig.",
			ExitCode: 1,
		}
	}
	cfg, ok := cfgRaw.(map[string]any)
	if !ok {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "GreenHopper edit model did not include swimlanesConfig.",
			ExitCode: 1,
		}
	}

	strategy, ok := cfg["swimlaneStrategy"].(string)
	if !ok {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "GreenHopper swimlane strategy missing or invalid.",
			ExitCode: 1,
		}
	}

	swimlanesRaw, ok := cfg["swimlanes"].([]any)
	if !ok {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "GreenHopper swimlanes list missing or invalid.",
			ExitCode: 1,
		}
	}

	var lanes []Swimlane
	for _, item := range swimlanesRaw {
		if m, ok := item.(map[string]any); ok {
			lane, err := SwimlaneFromAPI(m)
			if err != nil {
				return nil, err
			}
			lanes = append(lanes, lane)
		}
	}

	canEdit := toBool(cfg["canEdit"])

	return &SwimlanesConfig{
		Strategy:  strategy,
		Swimlanes: lanes,
		CanEdit:   canEdit,
	}, nil
}

// LoadDesiredSwimlanesFile loads and validates a desired swimlanes config from a JSON file.
func LoadDesiredSwimlanesFile(path string) (*SwimlanesConfig, error) {
	data, err := readJSONFile(path)
	if err != nil {
		return nil, err
	}

	var strategy *string
	if raw := data["swimlaneStrategy"]; raw != nil {
		s, ok := raw.(string)
		if !ok {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  "swimlaneStrategy must be a string when provided.",
				ExitCode: 1,
			}
		}
		strategy = &s
	}

	swimlanesRaw, ok := data["swimlanes"].([]any)
	if !ok {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  "swimlanes must be an array.",
			ExitCode: 1,
		}
	}

	var lanes []Swimlane
	for i, item := range swimlanesRaw {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  fmt.Sprintf("swimlanes[%d] must be an object.", i),
				ExitCode: 1,
			}
		}
		lane, err := SwimlaneFromDesired(m, i)
		if err != nil {
			return nil, err
		}
		lanes = append(lanes, lane)
	}

	strat := ""
	if strategy != nil {
		strat = *strategy
	}

	return &SwimlanesConfig{
		Strategy:  strat,
		Swimlanes: lanes,
	}, nil
}

// ValidateDesiredSwimlanes validates swimlane constraints:
// exactly one default, unique names, unique IDs.
func ValidateDesiredSwimlanes(lanes []Swimlane) error {
	defaultCount := 0
	for _, lane := range lanes {
		if lane.IsDefault {
			defaultCount++
		}
	}
	if defaultCount != 1 {
		return &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  "Exactly one swimlane must have isDefault=true.",
			ExitCode: 1,
		}
	}

	names := make(map[string]bool)
	for _, lane := range lanes {
		if names[lane.Name] {
			return &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  "Duplicate swimlane names in desired config.",
				ExitCode: 1,
			}
		}
		names[lane.Name] = true
	}

	ids := make(map[int]bool)
	for _, lane := range lanes {
		if lane.ID != nil {
			if ids[*lane.ID] {
				return &cerrors.CojiraError{
					Code:     cerrors.InvalidJSON,
					Message:  "Duplicate swimlane ids in desired config.",
					ExitCode: 1,
				}
			}
			ids[*lane.ID] = true
		}
	}

	return nil
}

// ApplyPlan is the result of BuildApplyPlan.
type ApplyPlan struct {
	Strategy StrategyOp
	Creates  []Swimlane
	Updates  []UpdateOp
	Deletes  []int
	Reorder  ReorderPlan
	Ops      []map[string]any
	Summary  PlanSummary
}

// StrategyOp describes a strategy change.
type StrategyOp struct {
	From    string
	To      string
	Changed bool
}

// UpdateOp describes an update to an existing swimlane.
type UpdateOp struct {
	Action  string
	ID      *int
	Fields  map[string]map[string]any
	Desired Swimlane
	Current Swimlane
}

// ReorderPlan describes the desired ordering.
type ReorderPlan struct {
	Desired []map[string]any
	Extras  []int
}

// PlanSummary holds counts for the plan.
type PlanSummary struct {
	Create int
	Update int
	Delete int
	Move   int
	DryRun bool
}

// BuildApplyPlan computes the diff between current and desired swimlane configs.
func BuildApplyPlan(current, desired *SwimlanesConfig, deleteMissing bool) (*ApplyPlan, error) {
	currentStrategy := current.Strategy
	desiredStrategy := desired.Strategy
	if desiredStrategy == "" {
		desiredStrategy = currentStrategy
	}

	currentLanes := current.Swimlanes
	desiredLanes := desired.Swimlanes

	if desiredStrategy != "custom" {
		if len(desiredLanes) > 0 {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "Refusing to apply swimlanes when swimlaneStrategy != 'custom'. Use set-strategy first or export/apply a custom strategy config.",
				ExitCode: 2,
			}
		}
	} else {
		if err := ValidateDesiredSwimlanes(desiredLanes); err != nil {
			return nil, err
		}
	}

	byID := make(map[int]Swimlane)
	for _, lane := range currentLanes {
		if lane.ID != nil {
			byID[*lane.ID] = lane
		}
	}

	byName, dupes := uniqueByName(currentLanes)
	type matchPair struct {
		desired Swimlane
		current *Swimlane
	}
	var matched []matchPair
	usedCurrentIDs := make(map[int]bool)

	for _, lane := range desiredLanes {
		if lane.ID != nil {
			cur, ok := byID[*lane.ID]
			if !ok {
				return nil, &cerrors.CojiraError{
					Code:     cerrors.OpFailed,
					Message:  fmt.Sprintf("Desired swimlane id %d not found on this board.", *lane.ID),
					ExitCode: 2,
				}
			}
			matched = append(matched, matchPair{desired: lane, current: &cur})
			id := *lane.ID
			if cur.ID != nil {
				id = *cur.ID
			}
			usedCurrentIDs[id] = true
			continue
		}

		if dupes[lane.Name] {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Swimlane name match is ambiguous on this board: %q. Use ids (export first) to apply safely.", lane.Name),
				ExitCode: 2,
			}
		}

		if cur, ok := byName[lane.Name]; ok {
			matched = append(matched, matchPair{desired: lane, current: &cur})
			if cur.ID != nil {
				usedCurrentIDs[*cur.ID] = true
			}
		} else {
			matched = append(matched, matchPair{desired: lane, current: nil})
		}
	}

	var creates []Swimlane
	var updates []UpdateOp
	for _, m := range matched {
		if m.current == nil {
			creates = append(creates, m.desired)
			continue
		}
		changed := diffLane(*m.current, m.desired)
		if len(changed) > 0 {
			updates = append(updates, UpdateOp{
				Action:  "update",
				ID:      m.current.ID,
				Fields:  changed,
				Desired: m.desired,
				Current: *m.current,
			})
		}
	}

	var deletes []int
	if deleteMissing {
		for _, lane := range currentLanes {
			if lane.ID == nil {
				continue
			}
			if !usedCurrentIDs[*lane.ID] {
				deletes = append(deletes, *lane.ID)
			}
		}
	}

	strategyOp := StrategyOp{
		From:    currentStrategy,
		To:      desiredStrategy,
		Changed: currentStrategy != desiredStrategy,
	}

	var desiredOrder []map[string]any
	for _, m := range matched {
		if m.current != nil && m.current.ID != nil {
			desiredOrder = append(desiredOrder, map[string]any{"kind": "id", "id": *m.current.ID})
		} else if m.desired.ID != nil {
			desiredOrder = append(desiredOrder, map[string]any{"kind": "id", "id": *m.desired.ID})
		} else {
			desiredOrder = append(desiredOrder, map[string]any{"kind": "name", "name": m.desired.Name})
		}
	}

	var extras []int
	if !deleteMissing {
		for _, lane := range currentLanes {
			if lane.ID == nil {
				continue
			}
			if !usedCurrentIDs[*lane.ID] {
				extras = append(extras, *lane.ID)
			}
		}
	}

	summary := PlanSummary{
		Create: len(creates),
		Update: len(updates),
		Delete: len(deletes),
		Move:   0,
		DryRun: false,
	}

	var ops []map[string]any
	if strategyOp.Changed {
		ops = append(ops, map[string]any{
			"action": "set-strategy",
			"from":   strategyOp.From,
			"to":     strategyOp.To,
		})
	}
	for _, lane := range creates {
		ops = append(ops, map[string]any{
			"action": "create",
			"lane":   lane.ToJSON(),
		})
	}
	for _, upd := range updates {
		fields := make(map[string]any)
		for k, v := range upd.Fields {
			fields[k] = v
		}
		op := map[string]any{
			"action": "update",
			"id":     upd.ID,
			"fields": fields,
		}
		if upd.ID != nil {
			op["id"] = *upd.ID
		}
		ops = append(ops, op)
	}
	for _, laneID := range deletes {
		ops = append(ops, map[string]any{
			"action": "delete",
			"id":     laneID,
		})
	}

	return &ApplyPlan{
		Strategy: strategyOp,
		Creates:  creates,
		Updates:  updates,
		Deletes:  deletes,
		Reorder: ReorderPlan{
			Desired: desiredOrder,
			Extras:  extras,
		},
		Ops:     ops,
		Summary: summary,
	}, nil
}

func uniqueByName(lanes []Swimlane) (map[string]Swimlane, map[string]bool) {
	byName := make(map[string]Swimlane)
	dupes := make(map[string]bool)
	for _, lane := range lanes {
		if _, exists := byName[lane.Name]; exists {
			dupes[lane.Name] = true
			continue
		}
		byName[lane.Name] = lane
	}
	for name := range dupes {
		delete(byName, name)
	}
	return byName, dupes
}

func diffLane(current, desired Swimlane) map[string]map[string]any {
	fields := make(map[string]map[string]any)
	if current.Name != desired.Name {
		fields["name"] = map[string]any{"from": current.Name, "to": desired.Name}
	}
	if current.Query != desired.Query {
		fields["query"] = map[string]any{"from": current.Query, "to": desired.Query}
	}
	if current.Description != desired.Description {
		fields["description"] = map[string]any{"from": current.Description, "to": desired.Description}
	}
	if current.IsDefault != desired.IsDefault {
		fields["isDefault"] = map[string]any{"from": current.IsDefault, "to": desired.IsDefault}
	}
	return fields
}

// MoveOp describes a reorder operation.
type MoveOp struct {
	Action   string `json:"action"`
	ID       int    `json:"id"`
	Position string `json:"position,omitempty"`
	AfterID  *int   `json:"afterId,omitempty"`
}

// ComputeMoveOps computes the sequence of move operations needed to reorder
// swimlanes from currentIDs to finalIDs.
func ComputeMoveOps(currentIDs, finalIDs []int) []MoveOp {
	if len(finalIDs) == 0 {
		return nil
	}
	if intSliceEqual(currentIDs, finalIDs) {
		return nil
	}
	ops := []MoveOp{{Action: "move", ID: finalIDs[0], Position: "First"}}
	for i := 1; i < len(finalIDs); i++ {
		prev := finalIDs[i-1]
		ops = append(ops, MoveOp{Action: "move", ID: finalIDs[i], AfterID: &prev})
	}
	return ops
}

func intSliceEqual(a, b []int) bool {
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
