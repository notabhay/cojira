package board

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	cerrors "github.com/notabhay/cojira/internal/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractValidConfig(t *testing.T) {
	payload := map[string]any{
		"rapidViewId": 100.0,
		"canEdit":     true,
		"availableFields": []any{
			map[string]any{"id": nil, "fieldId": "priority", "name": "Priority", "category": "System", "isValid": true},
		},
		"currentFields": []any{
			map[string]any{"id": 1.0, "fieldId": "status", "name": "Status", "category": "System", "isValid": true},
		},
	}
	cfg, err := ExtractDetailViewFieldConfig(payload)
	require.NoError(t, err)
	assert.True(t, cfg.CanEdit)
	assert.Len(t, cfg.AvailableFields, 1)
	assert.Len(t, cfg.CurrentFields, 1)
	assert.Equal(t, "status", cfg.CurrentFields[0].FieldID)
}

func TestExtractMissingAvailableFields(t *testing.T) {
	_, err := ExtractDetailViewFieldConfig(map[string]any{"currentFields": []any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "availableFields")
}

func TestExtractMissingCurrentFields(t *testing.T) {
	_, err := ExtractDetailViewFieldConfig(map[string]any{"availableFields": []any{}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "currentFields")
}

func TestExtractNotADict(t *testing.T) {
	_, err := ExtractDetailViewFieldConfig(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not an object")
}

func TestDetailViewFieldMissingFieldID(t *testing.T) {
	_, err := DetailViewFieldFromAPI(map[string]any{"id": 1.0, "name": "X"}, 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing fieldId")
}

func TestDetailViewFieldMissingIDNotAllowed(t *testing.T) {
	_, err := DetailViewFieldFromAPI(map[string]any{"fieldId": "x", "name": "X"}, 0, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "missing id")
}

func TestDetailViewFieldMissingIDAllowed(t *testing.T) {
	f, err := DetailViewFieldFromAPI(map[string]any{"fieldId": "x", "name": "X"}, 0, true)
	require.NoError(t, err)
	assert.Nil(t, f.ID)
	assert.Equal(t, "x", f.FieldID)
}

func TestLoadDesiredFieldsFromFieldIDs(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fields.json")
	data, _ := json.Marshal(map[string]any{"fieldIds": []string{"priority", "status"}})
	require.NoError(t, os.WriteFile(path, data, 0644))

	result, err := LoadDesiredDetailViewFieldsFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"priority", "status"}, result)
}

func TestLoadDesiredFieldsFromObjects(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fields.json")
	data, _ := json.Marshal(map[string]any{
		"fields": []map[string]any{{"fieldId": "priority"}, {"fieldId": "status"}},
	})
	require.NoError(t, os.WriteFile(path, data, 0644))

	result, err := LoadDesiredDetailViewFieldsFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"priority", "status"}, result)
}

func TestLoadDesiredFieldsFromStrings(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fields.json")
	data, _ := json.Marshal(map[string]any{"fields": []string{"priority", "status"}})
	require.NoError(t, os.WriteFile(path, data, 0644))

	result, err := LoadDesiredDetailViewFieldsFile(path)
	require.NoError(t, err)
	assert.Equal(t, []string{"priority", "status"}, result)
}

func TestLoadDesiredFieldsRejectsDuplicates(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fields.json")
	data, _ := json.Marshal(map[string]any{"fieldIds": []string{"priority", "priority"}})
	require.NoError(t, os.WriteFile(path, data, 0644))

	_, err := LoadDesiredDetailViewFieldsFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Duplicate")
}

func TestLoadDesiredFieldsRejectsEmptyFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fields.json")
	require.NoError(t, os.WriteFile(path, []byte(""), 0644))

	_, err := LoadDesiredDetailViewFieldsFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "empty")
}

func TestLoadDesiredFieldsFileNotFound(t *testing.T) {
	_, err := LoadDesiredDetailViewFieldsFile("/nonexistent/path.json")
	require.Error(t, err)
	var ce *cerrors.CojiraError
	require.ErrorAs(t, err, &ce)
	assert.Contains(t, err.Error(), "not found")
}

func TestLoadDesiredFieldsInvalidJSON(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "fields.json")
	require.NoError(t, os.WriteFile(path, []byte("{bad json"), 0644))

	_, err := LoadDesiredDetailViewFieldsFile(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Invalid JSON")
}

func TestToExportJSON(t *testing.T) {
	f := DetailViewField{ID: intPtr(1), FieldID: "priority", Name: "Priority", Category: "System", IsEstimationField: false, IsValid: true}
	out := f.ToExportJSON()
	assert.Equal(t, "priority", out["fieldId"])
	assert.Equal(t, "Priority", out["name"])
	assert.Equal(t, "System", out["category"])
	_, hasID := out["id"]
	assert.False(t, hasID)
}

func TestToExportJSONMinimal(t *testing.T) {
	f := DetailViewField{ID: intPtr(1), FieldID: "custom_1234", Name: "", Category: ""}
	out := f.ToExportJSON()
	assert.Equal(t, map[string]any{"fieldId": "custom_1234"}, out)
}
