package board

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	cerrors "github.com/cojira/cojira/internal/errors"
)

func readJSONFile(path string) (map[string]any, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, &cerrors.CojiraError{
				Code:     cerrors.FileNotFound,
				Message:  fmt.Sprintf("File not found: %s", path),
				ExitCode: 1,
			}
		}
		return nil, &cerrors.CojiraError{
			Code:     cerrors.FileNotFound,
			Message:  fmt.Sprintf("Cannot read file: %s: %v", path, err),
			ExitCode: 1,
		}
	}
	if strings.TrimSpace(string(data)) == "" {
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

func coerceInt(value any, field string) (int, error) {
	switch v := value.(type) {
	case int:
		return v, nil
	case float64:
		return int(v), nil
	case string:
		s := strings.TrimSpace(v)
		if s != "" {
			var n int
			if _, err := fmt.Sscanf(s, "%d", &n); err == nil {
				return n, nil
			}
		}
	}
	return 0, &cerrors.CojiraError{
		Code:     cerrors.InvalidJSON,
		Message:  fmt.Sprintf("Expected integer for %s.", field),
		ExitCode: 1,
	}
}

func coerceBool(value any, field string) (bool, error) {
	if v, ok := value.(bool); ok {
		return v, nil
	}
	return false, &cerrors.CojiraError{
		Code:     cerrors.InvalidJSON,
		Message:  fmt.Sprintf("Expected boolean for %s.", field),
		ExitCode: 1,
	}
}

func coerceStr(value any, field string) (string, error) {
	if v, ok := value.(string); ok {
		return v, nil
	}
	return "", &cerrors.CojiraError{
		Code:     cerrors.InvalidJSON,
		Message:  fmt.Sprintf("Expected string for %s.", field),
		ExitCode: 1,
	}
}

func coerceIntOptional(value any, field string) (*int, error) {
	if value == nil {
		return nil, nil
	}
	n, err := coerceInt(value, field)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func toBool(value any) bool {
	if v, ok := value.(bool); ok {
		return v
	}
	return false
}

func toString(value any) string {
	if value == nil {
		return ""
	}
	if v, ok := value.(string); ok {
		return v
	}
	return fmt.Sprintf("%v", value)
}
