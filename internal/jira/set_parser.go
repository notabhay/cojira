package jira

import (
	"fmt"
	"strings"

	cerrors "github.com/notabhay/cojira/internal/errors"
)

const (
	// OpJSONSet replaces a field using a JSON or object-style value.
	OpJSONSet = ":="
	// OpListAppend appends a value to a list-like field.
	OpListAppend = "+="
	// OpListRemove removes a value from a list-like field.
	OpListRemove = "-="
	// OpSet assigns a plain scalar value to a field.
	OpSet = "="
)

// ParseSetExpr parses a --set expression like "field=value", "field:=json",
// "labels+=x", or "labels-=x". Returns (field, operator, value).
func ParseSetExpr(expr string) (string, string, string, error) {
	raw := strings.TrimSpace(expr)
	if raw == "" {
		return "", "", "", &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  "Empty --set expression.",
			ExitCode: 1,
		}
	}

	for i := 0; i < len(raw); i++ {
		op := ""
		switch {
		case i+1 < len(raw) && raw[i] == ':' && raw[i+1] == '=':
			op = OpJSONSet
		case i+1 < len(raw) && raw[i] == '+' && raw[i+1] == '=':
			op = OpListAppend
		case i+1 < len(raw) && raw[i] == '-' && raw[i+1] == '=':
			op = OpListRemove
		case raw[i] == '=':
			op = OpSet
		}
		if op == "" {
			continue
		}
		field := strings.TrimSpace(raw[:i])
		value := strings.TrimSpace(raw[i+len(op):])
		field = strings.TrimPrefix(field, "fields.")
		field = strings.TrimSpace(field)
		if field == "" {
			return "", "", "", &cerrors.CojiraError{
				Code:     cerrors.OpFailed,
				Message:  fmt.Sprintf("Invalid --set expression (missing field): %q", expr),
				ExitCode: 1,
			}
		}
		return field, op, value, nil
	}

	return "", "", "", &cerrors.CojiraError{
		Code:     cerrors.OpFailed,
		Message:  fmt.Sprintf("Invalid --set expression: %q. Expected field=value, field:=<json>, labels+=x, labels-=x.", expr),
		ExitCode: 1,
	}
}

// MergeListByName applies an append (+=) or remove (-=) operation on a list
// of objects that have a "name" key.
func MergeListByName(current []map[string]any, op string, name string) ([]map[string]any, error) {
	out := make([]map[string]any, len(current))
	copy(out, current)

	switch op {
	case OpListAppend:
		// Check if name already exists.
		for _, item := range out {
			if fmt.Sprintf("%v", item["name"]) == name {
				return out, nil
			}
		}
		out = append(out, map[string]any{"name": name})
		return out, nil

	case OpListRemove:
		var result []map[string]any
		for _, item := range out {
			if fmt.Sprintf("%v", item["name"]) != name {
				result = append(result, item)
			}
		}
		if result == nil {
			result = []map[string]any{}
		}
		return result, nil

	default:
		return nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported list op: %s", op),
			ExitCode: 1,
		}
	}
}

// MergeListOfStrings applies an append (+=) or remove (-=) operation on a
// string slice.
func MergeListOfStrings(current []string, op string, value string) ([]string, error) {
	out := make([]string, len(current))
	copy(out, current)

	switch op {
	case OpListAppend:
		for _, v := range out {
			if v == value {
				return out, nil
			}
		}
		return append(out, value), nil

	case OpListRemove:
		var result []string
		for _, v := range out {
			if v != value {
				result = append(result, v)
			}
		}
		if result == nil {
			result = []string{}
		}
		return result, nil

	default:
		return nil, &cerrors.CojiraError{
			Code:     cerrors.OpFailed,
			Message:  fmt.Sprintf("Unsupported list op: %s", op),
			ExitCode: 1,
		}
	}
}
