package confluence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	cerrors "github.com/cojira/cojira/internal/errors"
)

// ConfluenceIdentifierFormats describes the supported page identifier formats.
const ConfluenceIdentifierFormats = `12345, SPACE:"Title", https://confluence...pageId=12345, tiny-link-code`

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

// writeFile writes content to a file path.
func writeFile(path, content string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

// defaultSpace reads the default space from project config.
func defaultSpace(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	confSection, ok := cfg["confluence"].(map[string]any)
	if !ok {
		return ""
	}
	v, ok := confSection["default_space"]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprintf("%v", v))
}

// defaultPageID reads the default page ID from project config.
func defaultPageID(cfg map[string]any) string {
	if cfg == nil {
		return ""
	}
	confSection, ok := cfg["confluence"].(map[string]any)
	if !ok {
		return ""
	}
	for _, key := range []string{"root_page_id", "default_page_id"} {
		if v, ok := confSection[key]; ok && v != nil {
			s := strings.TrimSpace(fmt.Sprintf("%v", v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}
