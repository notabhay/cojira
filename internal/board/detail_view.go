package board

import (
	"fmt"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
)

// DetailViewField represents a board detail view field configuration.
type DetailViewField struct {
	ID                *int   `json:"id,omitempty"`
	FieldID           string `json:"fieldId"`
	Name              string `json:"name,omitempty"`
	Category          string `json:"category,omitempty"`
	IsEstimationField bool   `json:"isEstimationField,omitempty"`
	IsValid           bool   `json:"isValid,omitempty"`
}

// DetailViewFieldConfig holds the extracted detail view field configuration.
type DetailViewFieldConfig struct {
	RapidViewID     any               `json:"rapidViewId,omitempty"`
	CanEdit         bool              `json:"canEdit"`
	AvailableFields []DetailViewField `json:"availableFields"`
	CurrentFields   []DetailViewField `json:"currentFields"`
}

// DetailViewFieldFromAPI creates a DetailViewField from GreenHopper API data.
func DetailViewFieldFromAPI(data map[string]any, index int, allowMissingID bool) (DetailViewField, error) {
	if data == nil {
		return DetailViewField{}, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  fmt.Sprintf("detail view field [%d] is not an object.", index),
			ExitCode: 1,
		}
	}

	rawID := data["id"]
	fieldID := trimString2(toString(data["fieldId"]))
	name := trimString2(toString(data["name"]))
	category := trimString2(toString(data["category"]))

	if rawID == nil && !allowMissingID {
		return DetailViewField{}, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  fmt.Sprintf("detail view field [%d] is missing id.", index),
			ExitCode: 1,
		}
	}

	var coercedID *int
	if rawID != nil {
		id, err := coerceIntOptional(rawID, fmt.Sprintf("detailViewField[%d].id", index))
		if err != nil {
			return DetailViewField{}, err
		}
		coercedID = id
	}

	if fieldID == "" {
		return DetailViewField{}, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  fmt.Sprintf("detail view field [%d] is missing fieldId.", index),
			ExitCode: 1,
		}
	}

	isEstimation := toBool(data["isEstimationField"])
	isValid := true
	if v, exists := data["isValid"]; exists {
		isValid = toBool(v)
	}

	return DetailViewField{
		ID:                coercedID,
		FieldID:           fieldID,
		Name:              name,
		Category:          category,
		IsEstimationField: isEstimation,
		IsValid:           isValid,
	}, nil
}

// ToExportJSON converts a DetailViewField to a minimal export map.
func (f DetailViewField) ToExportJSON() map[string]any {
	out := map[string]any{
		"fieldId": f.FieldID,
	}
	if f.Name != "" {
		out["name"] = f.Name
	}
	if f.Category != "" {
		out["category"] = f.Category
	}
	return out
}

// ExtractDetailViewFieldConfig parses the GreenHopper detail view field config payload.
func ExtractDetailViewFieldConfig(payload map[string]any) (*DetailViewFieldConfig, error) {
	if payload == nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "GreenHopper detail view field config response is not an object.",
			ExitCode: 1,
		}
	}

	availableRaw, ok := payload["availableFields"].([]any)
	if !ok {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "GreenHopper availableFields missing or invalid.",
			ExitCode: 1,
		}
	}
	currentRaw, ok := payload["currentFields"].([]any)
	if !ok {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FetchFailed,
			Message:  "GreenHopper currentFields missing or invalid.",
			ExitCode: 1,
		}
	}

	var availableFields []DetailViewField
	for i, item := range availableRaw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		field, err := DetailViewFieldFromAPI(m, i, true)
		if err != nil {
			return nil, err
		}
		availableFields = append(availableFields, field)
	}

	var currentFields []DetailViewField
	for i, item := range currentRaw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		field, err := DetailViewFieldFromAPI(m, i, false)
		if err != nil {
			return nil, err
		}
		currentFields = append(currentFields, field)
	}

	return &DetailViewFieldConfig{
		RapidViewID:     payload["rapidViewId"],
		CanEdit:         toBool(payload["canEdit"]),
		AvailableFields: availableFields,
		CurrentFields:   currentFields,
	}, nil
}

// LoadDesiredDetailViewFieldsFile loads desired field IDs from a JSON config file.
// Supports both {"fieldIds": [...]} and {"fields": [...]} formats.
func LoadDesiredDetailViewFieldsFile(path string) ([]string, error) {
	data, err := readJSONFile(path)
	if err != nil {
		return nil, err
	}

	var fieldIDs []string

	if raw, ok := data["fieldIds"]; ok {
		arr, ok := raw.([]any)
		if !ok {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  "fieldIds must be an array.",
				ExitCode: 1,
			}
		}
		for i, item := range arr {
			s, err := coerceStr(item, fmt.Sprintf("fieldIds[%d]", i))
			if err != nil {
				return nil, err
			}
			fieldIDs = append(fieldIDs, trimString2(s))
		}
	} else {
		rawFields, ok := data["fields"]
		if !ok {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  "fields must be an array (or use fieldIds).",
				ExitCode: 1,
			}
		}
		arr, ok := rawFields.([]any)
		if !ok {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  "fields must be an array (or use fieldIds).",
				ExitCode: 1,
			}
		}
		for i, item := range arr {
			switch v := item.(type) {
			case string:
				fieldIDs = append(fieldIDs, trimString2(v))
			case map[string]any:
				s, err := coerceStr(v["fieldId"], fmt.Sprintf("fields[%d].fieldId", i))
				if err != nil {
					return nil, err
				}
				fieldIDs = append(fieldIDs, trimString2(s))
			default:
				return nil, &cerrors.CojiraError{
					Code:     cerrors.InvalidJSON,
					Message:  fmt.Sprintf("fields[%d] must be a string or object with fieldId.", i),
					ExitCode: 1,
				}
			}
		}
	}

	// Filter empty strings.
	var filtered []string
	for _, id := range fieldIDs {
		if id != "" {
			filtered = append(filtered, id)
		}
	}
	fieldIDs = filtered

	// Check for duplicates.
	seen := make(map[string]bool)
	for _, id := range fieldIDs {
		if seen[id] {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.InvalidJSON,
				Message:  "Duplicate fieldIds in desired config.",
				ExitCode: 1,
			}
		}
		seen[id] = true
	}

	return fieldIDs, nil
}

func trimString2(s string) string {
	return strings.TrimSpace(s)
}
