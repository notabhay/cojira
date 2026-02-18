package jira

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
)

// readJSONFile reads and parses a JSON file, returning a map.
func readJSONFile(path string) (map[string]any, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FileNotFound,
			Message:  fmt.Sprintf("File not found: %s", path),
			ExitCode: 1,
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FileNotFound,
			Message:  fmt.Sprintf("Cannot read file: %s", path),
			ExitCode: 1,
		}
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  "Refusing to load empty JSON file.",
			ExitCode: 1,
		}
	}
	var result map[string]any
	if err := json.Unmarshal(data, &result); err != nil {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  fmt.Sprintf("Invalid JSON in %s: %v", path, err),
			ExitCode: 1,
		}
	}
	return result, nil
}

// readTextFile reads a text file and returns its content.
func readTextFile(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", &cerrors.CojiraError{
			Code:     cerrors.FileNotFound,
			Message:  fmt.Sprintf("File not found: %s", path),
			ExitCode: 1,
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", &cerrors.CojiraError{
			Code:     cerrors.FileNotFound,
			Message:  fmt.Sprintf("Cannot read file: %s", path),
			ExitCode: 1,
		}
	}
	return string(data), nil
}

// formatValue formats an arbitrary value for human display, truncating if needed.
func formatValue(value any, maxLen int) string {
	if maxLen <= 0 {
		maxLen = 240
	}
	var text string
	switch v := value.(type) {
	case nil:
		text = "null"
	case map[string]any, []any:
		b, _ := json.Marshal(v)
		text = string(b)
	default:
		text = fmt.Sprintf("%v", v)
	}
	if len(text) > maxLen {
		return text[:maxLen-3] + "..."
	}
	return text
}

// diffFields compares current and new field maps, returning changes.
type fieldDiff struct {
	Field    string `json:"field"`
	OldValue any    `json:"from"`
	NewValue any    `json:"to"`
}

func diffFields(currentFields, newFields map[string]any) []fieldDiff {
	var diffs []fieldDiff
	for field, newValue := range newFields {
		oldValue := currentFields[field]
		oldJSON, _ := json.Marshal(oldValue)
		newJSON, _ := json.Marshal(newValue)
		if string(oldJSON) != string(newJSON) {
			diffs = append(diffs, fieldDiff{
				Field:    field,
				OldValue: oldValue,
				NewValue: newValue,
			})
		}
	}
	return diffs
}

// previewPayloadDiff computes and optionally prints field diffs between an issue and a payload.
func previewPayloadDiff(issueID string, issue, payload map[string]any, quiet bool) []fieldDiff {
	fields, _ := payload["fields"].(map[string]any)
	if len(fields) == 0 {
		if !quiet {
			fmt.Printf("%s: no field updates in payload\n", issueID)
		}
		return nil
	}
	currentFields, _ := issue["fields"].(map[string]any)
	if currentFields == nil {
		currentFields = map[string]any{}
	}
	diffs := diffFields(currentFields, fields)
	if len(diffs) == 0 {
		if !quiet {
			fmt.Printf("%s: no changes\n", issueID)
		}
		return nil
	}
	if !quiet {
		fmt.Printf("%s:\n", issueID)
		for _, d := range diffs {
			fmt.Printf("  %s: %s -> %s\n", d.Field, formatValue(d.OldValue, 240), formatValue(d.NewValue, 240))
		}
	}
	return diffs
}

// formatFieldList formats a list of field names for display.
func formatFieldList(fields []string, maxFields int) string {
	if len(fields) == 0 {
		return ""
	}
	if maxFields <= 0 {
		maxFields = 6
	}
	shown := fields
	if len(fields) > maxFields {
		shown = fields[:maxFields]
	}
	result := strings.Join(shown, ", ")
	if len(fields) > maxFields {
		result += fmt.Sprintf(" (+%d more)", len(fields)-maxFields)
	}
	return result
}

// collectIssueKeys pages through a JQL search to collect all issue keys.
func collectIssueKeys(client *Client, jql string, pageSize int) ([]string, error) {
	var keys []string
	startAt := 0
	for {
		data, err := client.Search(jql, pageSize, startAt, "summary", "")
		if err != nil {
			return nil, err
		}
		issues, _ := data["issues"].([]any)
		total := intFromAny(data["total"], 0)
		for _, i := range issues {
			if m, ok := i.(map[string]any); ok {
				if key, ok := m["key"].(string); ok && key != "" {
					keys = append(keys, key)
				}
			}
		}
		startAt += len(issues)
		if startAt >= total || len(issues) == 0 {
			break
		}
	}
	return keys, nil
}

// intFromAny extracts an int from any, returning defaultVal on failure.
func intFromAny(v any, defaultVal int) int {
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	case json.Number:
		i, err := n.Int64()
		if err == nil {
			return int(i)
		}
	}
	return defaultVal
}

// printFailures prints a list of failures.
func printFailures(failures []failureEntry) {
	if len(failures) == 0 {
		return
	}
	fmt.Println("\nFailures:")
	for _, f := range failures {
		fmt.Printf("  %s: %s\n", f.key, f.err)
	}
}

type failureEntry struct {
	key string
	err string
}

// safeString extracts a string from a nested map path.
func safeString(m map[string]any, keys ...string) string {
	var cur any = m
	for _, key := range keys {
		mm, ok := cur.(map[string]any)
		if !ok {
			return ""
		}
		cur = mm[key]
	}
	s, _ := cur.(string)
	return s
}

// safeStringSlice extracts a string slice from a map.
func safeStringSlice(m map[string]any, key string) []string {
	arr, ok := m[key].([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, v := range arr {
		if s, ok := v.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// extractNames extracts "name" fields from a list of objects.
func extractNames(m map[string]any, key string) []string {
	arr, ok := m[key].([]any)
	if !ok {
		return nil
	}
	var result []string
	for _, v := range arr {
		if obj, ok := v.(map[string]any); ok {
			if name, ok := obj["name"].(string); ok {
				result = append(result, name)
			}
		}
	}
	return result
}

var normTitleRe = regexp.MustCompile(`^\d+-`)

// normalizeTitleFromFilename strips leading digits and hyphens from a filename stem.
func normalizeTitleFromFilename(path string) string {
	base := filepath.Base(path)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	title := normTitleRe.ReplaceAllString(stem, "")
	return strings.TrimSpace(title)
}

// summaryMapping is a key->summary pair for bulk summary updates.
type summaryMapping struct {
	Key     string
	Summary string
}

// loadSummaryMap loads a CSV or JSON file containing issue key -> summary mappings.
func loadSummaryMap(path string) ([]summaryMapping, error) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FileNotFound,
			Message:  fmt.Sprintf("File not found: %s", path),
			ExitCode: 1,
		}
	}

	ext := strings.ToLower(filepath.Ext(path))

	if ext == ".json" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		// Try as object first.
		var obj map[string]any
		if err := json.Unmarshal(data, &obj); err == nil {
			var result []summaryMapping
			for k, v := range obj {
				result = append(result, summaryMapping{Key: k, Summary: fmt.Sprintf("%v", v)})
			}
			return result, nil
		}
		// Try as array of objects.
		var arr []map[string]any
		if err := json.Unmarshal(data, &arr); err == nil {
			var result []summaryMapping
			for _, row := range arr {
				key := firstString(row, "key", "issue", "issue_key")
				summary := firstString(row, "summary", "title")
				if key != "" && summary != "" {
					result = append(result, summaryMapping{Key: key, Summary: summary})
				}
			}
			return result, nil
		}
		return nil, &cerrors.CojiraError{
			Code:     cerrors.InvalidJSON,
			Message:  "JSON summary map must be an object or list of objects.",
			ExitCode: 1,
		}
	}

	if ext == ".csv" {
		f, err := os.Open(path)
		if err != nil {
			return nil, err
		}
		defer func() { _ = f.Close() }()
		reader := csv.NewReader(f)
		// Read header.
		header, err := reader.Read()
		if err != nil {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "CSV must include columns: key/issue and summary/title.",
				ExitCode: 1,
			}
		}
		keyIdx := -1
		summaryIdx := -1
		for i, h := range header {
			h = strings.TrimSpace(strings.ToLower(h))
			if h == "key" || h == "issue" || h == "issue_key" {
				if keyIdx == -1 {
					keyIdx = i
				}
			}
			if h == "summary" || h == "title" {
				if summaryIdx == -1 {
					summaryIdx = i
				}
			}
		}
		if keyIdx == -1 || summaryIdx == -1 {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "CSV must include columns: key/issue and summary/title.",
				ExitCode: 1,
			}
		}
		var result []summaryMapping
		for {
			row, err := reader.Read()
			if err == io.EOF {
				break
			}
			if err != nil {
				continue
			}
			if keyIdx < len(row) && summaryIdx < len(row) {
				k := strings.TrimSpace(row[keyIdx])
				s := strings.TrimSpace(row[summaryIdx])
				if k != "" && s != "" {
					result = append(result, summaryMapping{Key: k, Summary: s})
				}
			}
		}
		if len(result) == 0 {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  "CSV must include columns: key/issue and summary/title.",
				ExitCode: 1,
			}
		}
		return result, nil
	}

	return nil, &cerrors.CojiraError{
		Code:     cerrors.OpFailed,
		Message:  "Summary map file must be .json or .csv",
		ExitCode: 1,
	}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := fmt.Sprintf("%v", v)
			if strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
	}
	return ""
}

// writeFile writes content to a file path.
func writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
